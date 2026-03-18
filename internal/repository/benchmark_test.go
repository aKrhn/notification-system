package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func BenchmarkCursorEncode(b *testing.B) {
	c := &Cursor{
		CreatedAt: time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
		ID:        uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Encode()
	}
}

func BenchmarkCursorDecode(b *testing.B) {
	c := &Cursor{
		CreatedAt: time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
		ID:        uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
	}
	encoded := c.Encode()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeCursor(encoded)
	}
}

func BenchmarkCursorRoundtrip(b *testing.B) {
	c := &Cursor{
		CreatedAt: time.Now(),
		ID:        uuid.New(),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded := c.Encode()
		DecodeCursor(encoded)
	}
}
