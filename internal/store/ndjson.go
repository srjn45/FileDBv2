// Package store handles low-level NDJSON encoding and decoding for FileDB
// segment entries. Each entry is a single JSON object terminated by a newline.
package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// Op represents the type of operation recorded in a segment entry.
type Op string

const (
	OpInsert Op = "insert"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
)

// Entry is one line in a segment file. It captures the operation, the record
// id, a timestamp, and — for insert/update — the full record data.
type Entry struct {
	ID   uint64         `json:"id"`
	Op   Op             `json:"op"`
	Ts   time.Time      `json:"ts"`
	Data map[string]any `json:"data,omitempty"` // nil for OpDelete
}

// Encode serialises e as a JSON object followed by a newline.
// The returned slice is ready to be appended directly to a segment file.
func Encode(e Entry) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("store: encode entry id=%d: %w", e.ID, err)
	}
	return append(b, '\n'), nil
}

// Decode parses a single JSON line (the trailing newline is ignored).
func Decode(line []byte) (Entry, error) {
	var e Entry
	if err := json.Unmarshal(line, &e); err != nil {
		return Entry{}, fmt.Errorf("store: decode entry: %w", err)
	}
	return e, nil
}

// NewInsert returns an Entry for an insert operation.
func NewInsert(id uint64, data map[string]any) Entry {
	return Entry{ID: id, Op: OpInsert, Ts: time.Now().UTC(), Data: data}
}

// NewUpdate returns an Entry for an update operation.
func NewUpdate(id uint64, data map[string]any) Entry {
	return Entry{ID: id, Op: OpUpdate, Ts: time.Now().UTC(), Data: data}
}

// NewDelete returns an Entry for a delete tombstone (no data).
func NewDelete(id uint64) Entry {
	return Entry{ID: id, Op: OpDelete, Ts: time.Now().UTC()}
}
