package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/srjn45/filedbv2/internal/query"
	"github.com/srjn45/filedbv2/internal/store"
)

// CollectionConfig holds tunable parameters for a single collection.
type CollectionConfig struct {
	SegmentMaxSize  int64         // default: DefaultSegmentMaxSize
	CompactInterval time.Duration // default: 5m
	CompactDirtyPct float64       // default: 0.30 (30%)
}

func defaultConfig() CollectionConfig {
	return CollectionConfig{
		SegmentMaxSize:  DefaultSegmentMaxSize,
		CompactInterval: 5 * time.Minute,
		CompactDirtyPct: 0.30,
	}
}

// WatchEvent is emitted to Watch subscribers on every write.
type WatchEvent struct {
	Op   store.Op
	ID   uint64
	Data map[string]any
	Ts   time.Time
}

// Collection is a named set of records stored across one or more segment files.
// All exported methods are safe for concurrent use.
type Collection struct {
	name   string
	dir    string
	cfg    CollectionConfig
	mu     sync.RWMutex
	sealed []*Segment
	active *Segment
	index  *Index
	idSeq  atomic.Uint64 // monotonically increasing id counter

	// Watch subscribers.
	watchMu      sync.Mutex
	watchers     map[uint64]chan WatchEvent
	watcherIDSeq atomic.Uint64

	// Compactor control.
	compactC  chan struct{} // signal: run compaction now
	closeOnce sync.Once
	closed    chan struct{}
}

// OpenCollection opens or creates the collection rooted at dir.
// It loads the persisted index (rebuilding from segments if stale),
// and starts the background compactor goroutine.
func OpenCollection(name, dataDir string, cfg CollectionConfig) (*Collection, error) {
	dir := filepath.Join(dataDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("collection: mkdir %q: %w", dir, err)
	}

	c := &Collection{
		name:     name,
		dir:      dir,
		cfg:      cfg,
		index:    newIndex(),
		watchers: make(map[uint64]chan WatchEvent),
		compactC: make(chan struct{}, 1),
		closed:   make(chan struct{}),
	}

	if err := c.load(); err != nil {
		return nil, err
	}

	go c.compactLoop()
	return c, nil
}

// load reads existing segments from disk, restores the index, and opens
// or creates the active (write) segment.
func (c *Collection) load() error {
	// Discover sealed segment files.
	pattern := filepath.Join(c.dir, "seg_*.ndjson")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("collection: glob segments: %w", err)
	}
	sort.Strings(paths)

	// Identify the active (latest) segment — the one we'll append to.
	// All others are sealed.
	var activePath string
	if len(paths) == 0 {
		activePath = c.segmentPath(1)
	} else {
		activePath = paths[len(paths)-1]
		for _, p := range paths[:len(paths)-1] {
			info, err := os.Stat(p)
			if err != nil {
				return fmt.Errorf("collection: stat %q: %w", p, err)
			}
			c.sealed = append(c.sealed, openSealedSegment(p, info.Size()))
		}
	}

	active, err := openActiveSegment(activePath)
	if err != nil {
		return fmt.Errorf("collection: open active segment: %w", err)
	}
	c.active = active

	// Build the full segment list for index rebuild.
	all := make([]*Segment, 0, len(c.sealed)+1)
	all = append(all, c.sealed...)
	all = append(all, c.active)

	// Try loading the persisted index.
	indexPath := filepath.Join(c.dir, "index.json")
	err = c.index.Load(indexPath)
	if err != nil && !errors.Is(err, ErrIndexStale) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("collection: load index: %w", err)
	}
	if err != nil {
		// Stale or missing — rebuild.
		if rbErr := c.index.Rebuild(all); rbErr != nil {
			return fmt.Errorf("collection: rebuild index: %w", rbErr)
		}
		_ = c.index.Persist(indexPath)
	}

	// Set the id sequence to the highest known id.
	c.index.mu.RLock()
	var maxID uint64
	for id := range c.index.entries {
		if id > maxID {
			maxID = id
		}
	}
	c.index.mu.RUnlock()

	// Also scan all entries to find the true max id (includes deleted).
	for _, seg := range all {
		entries, err := seg.ScanAll()
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.ID > maxID {
				maxID = e.ID
			}
		}
	}
	c.idSeq.Store(maxID)

	return nil
}

// Insert adds a new record and returns its assigned id.
func (c *Collection) Insert(data map[string]any) (uint64, time.Time, error) {
	id := c.idSeq.Add(1)
	ts := time.Now().UTC()
	e := store.NewInsert(id, data)
	e.Ts = ts

	c.mu.Lock()
	offset, err := c.active.Append(e)
	if err != nil {
		c.mu.Unlock()
		return 0, time.Time{}, fmt.Errorf("collection: insert: %w", err)
	}
	c.index.Set(id, IndexEntry{SegmentPath: c.active.Path(), Offset: offset})
	needRotate := c.active.Size() >= c.cfg.SegmentMaxSize
	c.mu.Unlock()

	if needRotate {
		if err := c.rotateSegment(); err != nil {
			return id, ts, fmt.Errorf("collection: rotate after insert: %w", err)
		}
	}

	c.emit(WatchEvent{Op: store.OpInsert, ID: id, Data: data, Ts: ts})
	return id, ts, nil
}

// Update overwrites the data for an existing record.
func (c *Collection) Update(id uint64, data map[string]any) (time.Time, error) {
	ts := time.Now().UTC()
	e := store.NewUpdate(id, data)
	e.Ts = ts

	c.mu.Lock()
	if _, ok := c.index.Get(id); !ok {
		c.mu.Unlock()
		return time.Time{}, fmt.Errorf("collection: update: id %d not found", id)
	}
	offset, err := c.active.Append(e)
	if err != nil {
		c.mu.Unlock()
		return time.Time{}, fmt.Errorf("collection: update: %w", err)
	}
	c.index.Set(id, IndexEntry{SegmentPath: c.active.Path(), Offset: offset})
	needRotate := c.active.Size() >= c.cfg.SegmentMaxSize
	c.mu.Unlock()

	if needRotate {
		_ = c.rotateSegment()
	}

	c.emit(WatchEvent{Op: store.OpUpdate, ID: id, Data: data, Ts: ts})
	return ts, nil
}

// Delete removes a record by id.
func (c *Collection) Delete(id uint64) error {
	e := store.NewDelete(id)

	c.mu.Lock()
	if _, ok := c.index.Get(id); !ok {
		c.mu.Unlock()
		return fmt.Errorf("collection: delete: id %d not found", id)
	}
	if _, err := c.active.Append(e); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("collection: delete: %w", err)
	}
	c.index.Delete(id)
	c.mu.Unlock()

	c.emit(WatchEvent{Op: store.OpDelete, ID: id, Ts: e.Ts})
	return nil
}

// FindByID returns the data for the given id.
func (c *Collection) FindByID(id uint64) (map[string]any, time.Time, error) {
	c.mu.RLock()
	loc, ok := c.index.Get(id)
	c.mu.RUnlock()

	if !ok {
		return nil, time.Time{}, fmt.Errorf("collection: findById: id %d not found", id)
	}

	seg := c.segmentByPath(loc.SegmentPath)
	if seg == nil {
		return nil, time.Time{}, fmt.Errorf("collection: findById: segment not found for id %d", id)
	}

	e, err := seg.ReadAt(loc.Offset)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("collection: findById: %w", err)
	}
	return e.Data, e.Ts, nil
}

// Scan iterates all live records and returns those matching f.
// Results are returned in an undefined order (segment → entry order).
func (c *Collection) Scan(f query.Filter) ([]ScanResult, error) {
	if f == nil {
		f = query.MatchAll
	}

	c.mu.RLock()
	allSegs := make([]*Segment, 0, len(c.sealed)+1)
	allSegs = append(allSegs, c.sealed...)
	allSegs = append(allSegs, c.active)
	c.mu.RUnlock()

	// latest[id] = most recent entry seen (last write wins)
	latest := make(map[uint64]store.Entry)
	for _, seg := range allSegs {
		entries, err := seg.ScanAll()
		if err != nil {
			return nil, fmt.Errorf("collection: scan: %w", err)
		}
		for _, e := range entries {
			latest[e.ID] = e
		}
	}

	var results []ScanResult
	for _, e := range latest {
		if e.Op == store.OpDelete {
			continue
		}
		if f.Match(e.Data) {
			results = append(results, ScanResult{ID: e.ID, Data: e.Data, Ts: e.Ts})
		}
	}
	return results, nil
}

// ScanResult holds a single matched record from a Scan.
type ScanResult struct {
	ID   uint64
	Data map[string]any
	Ts   time.Time
}

// Stats returns diagnostic information about the collection.
func (c *Collection) Stats() CollectionStats {
	c.mu.RLock()
	segCount := len(c.sealed) + 1
	var totalSize int64
	for _, s := range c.sealed {
		totalSize += s.Size()
	}
	totalSize += c.active.Size()
	c.mu.RUnlock()

	return CollectionStats{
		Name:         c.name,
		RecordCount:  uint64(c.index.Len()),
		SegmentCount: uint64(segCount),
		SizeBytes:    uint64(totalSize),
	}
}

// CollectionStats holds diagnostic data for a collection.
type CollectionStats struct {
	Name         string
	RecordCount  uint64
	SegmentCount uint64
	DirtyEntries uint64
	SizeBytes    uint64
}

// Subscribe registers a watcher channel and returns its id and a cancel func.
func (c *Collection) Subscribe() (uint64, <-chan WatchEvent, func()) {
	id := c.watcherIDSeq.Add(1)
	ch := make(chan WatchEvent, 64)

	c.watchMu.Lock()
	c.watchers[id] = ch
	c.watchMu.Unlock()

	cancel := func() {
		c.watchMu.Lock()
		delete(c.watchers, id)
		c.watchMu.Unlock()
		close(ch)
	}
	return id, ch, cancel
}

func (c *Collection) emit(ev WatchEvent) {
	c.watchMu.Lock()
	for _, ch := range c.watchers {
		select {
		case ch <- ev:
		default: // drop if subscriber is slow
		}
	}
	c.watchMu.Unlock()
}

// rotateSegment seals the current active segment and opens a new one.
func (c *Collection) rotateSegment() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.active.Seal(); err != nil {
		return err
	}
	c.sealed = append(c.sealed, c.active)

	newPath := c.segmentPath(uint64(len(c.sealed) + 1))
	active, err := openActiveSegment(newPath)
	if err != nil {
		return err
	}
	c.active = active

	// Signal the compactor.
	select {
	case c.compactC <- struct{}{}:
	default:
	}
	return nil
}

// segmentByPath finds a segment in the collection by its file path.
func (c *Collection) segmentByPath(path string) *Segment {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, s := range c.sealed {
		if s.Path() == path {
			return s
		}
	}
	if c.active.Path() == path {
		return c.active
	}
	return nil
}

func (c *Collection) segmentPath(n uint64) string {
	return filepath.Join(c.dir, fmt.Sprintf("seg_%06d.ndjson", n))
}

// Name returns the collection name.
func (c *Collection) Name() string { return c.name }

// Close shuts down the collection and its background goroutine.
func (c *Collection) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() { close(c.closed) })
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.active.Close(); err != nil {
		return err
	}
	return c.index.Persist(filepath.Join(c.dir, "index.json"))
}
