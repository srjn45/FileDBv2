package store

import (
	"testing"
	"time"
)

func TestEncodeDecodeParity(t *testing.T) {
	original := Entry{
		ID:   42,
		Op:   OpInsert,
		Ts:   time.Now().UTC().Truncate(time.Millisecond),
		Data: map[string]any{"userName": "srajan", "score": float64(99)},
	}

	b, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Encoded bytes should end with newline.
	if b[len(b)-1] != '\n' {
		t.Error("Encode: missing trailing newline")
	}

	decoded, err := Decode(b[:len(b)-1])
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: got %d want %d", decoded.ID, original.ID)
	}
	if decoded.Op != original.Op {
		t.Errorf("Op mismatch: got %q want %q", decoded.Op, original.Op)
	}
	if decoded.Data["userName"] != original.Data["userName"] {
		t.Errorf("Data mismatch: got %v want %v", decoded.Data, original.Data)
	}
}

func TestDeleteEntryHasNoData(t *testing.T) {
	e := NewDelete(7)
	b, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(b[:len(b)-1])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Data != nil {
		t.Errorf("expected nil Data for delete entry, got %v", decoded.Data)
	}
	if decoded.Op != OpDelete {
		t.Errorf("expected OpDelete, got %q", decoded.Op)
	}
}
