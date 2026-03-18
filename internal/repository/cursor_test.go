package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorEncodeDecodeRoundtrip(t *testing.T) {
	original := &Cursor{
		CreatedAt: time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
		ID:        uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
	}

	encoded := original.Encode()
	if encoded == "" {
		t.Fatal("encoded cursor is empty")
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", decoded.ID, original.ID)
	}
}

func TestDecodeCursorInvalidBase64(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecodeCursorMalformedJSON(t *testing.T) {
	// Valid base64 but invalid JSON
	_, err := DecodeCursor("bm90LWpzb24=") // "not-json" in base64
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestDecodeCursorEmpty(t *testing.T) {
	_, err := DecodeCursor("")
	if err == nil {
		t.Error("expected error for empty cursor")
	}
}
