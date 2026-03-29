package engine

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// txOpKind identifies the operation staged inside a transaction.
type txOpKind int8

const (
	txOpInsert txOpKind = iota
	txOpUpdate
	txOpDelete
)

// txOp is one pending write staged inside a transaction.
type txOp struct {
	kind txOpKind
	id   uint64
	data map[string]any
	ts   time.Time
}

// Tx is an open, uncommitted transaction for a single collection.
type Tx struct {
	ID         string
	Collection string
	mu         sync.Mutex
	ops        []txOp
}

// StageInsert appends an insert op to the transaction buffer.
func (t *Tx) StageInsert(id uint64, data map[string]any) {
	t.mu.Lock()
	t.ops = append(t.ops, txOp{kind: txOpInsert, id: id, data: data, ts: time.Now().UTC()})
	t.mu.Unlock()
}

// StageUpdate appends an update op to the transaction buffer.
func (t *Tx) StageUpdate(id uint64, data map[string]any) {
	t.mu.Lock()
	t.ops = append(t.ops, txOp{kind: txOpUpdate, id: id, data: data, ts: time.Now().UTC()})
	t.mu.Unlock()
}

// StageDelete appends a delete op to the transaction buffer.
func (t *Tx) StageDelete(id uint64) {
	t.mu.Lock()
	t.ops = append(t.ops, txOp{kind: txOpDelete, id: id, ts: time.Now().UTC()})
	t.mu.Unlock()
}

// Snapshot returns a copy of the staged ops (safe to call from any package).
func (t *Tx) Snapshot() []txOp {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := make([]txOp, len(t.ops))
	copy(cp, t.ops)
	return cp
}

// TxManager owns all open transactions. It is safe for concurrent use.
type TxManager struct {
	mu  sync.RWMutex
	txs map[string]*Tx
}

// NewTxManager returns a ready TxManager.
func NewTxManager() *TxManager {
	return &TxManager{txs: make(map[string]*Tx)}
}

// Begin creates a new transaction for the given collection and returns its ID.
func (m *TxManager) Begin(collection string) string {
	id := newTxID()
	m.mu.Lock()
	m.txs[id] = &Tx{ID: id, Collection: collection}
	m.mu.Unlock()
	return id
}

// Get returns the transaction with the given ID, or (nil, false) if not found.
func (m *TxManager) Get(txID string) (*Tx, bool) {
	m.mu.RLock()
	tx, ok := m.txs[txID]
	m.mu.RUnlock()
	return tx, ok
}

// Remove deletes a transaction from the manager (used on commit or rollback).
func (m *TxManager) Remove(txID string) {
	m.mu.Lock()
	delete(m.txs, txID)
	m.mu.Unlock()
}

// newTxID generates a random UUID-shaped identifier using crypto/rand.
func newTxID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

