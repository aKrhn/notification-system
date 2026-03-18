package worker

import (
	"testing"
	"time"
)

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		minWant time.Duration
		maxWant time.Duration
	}{
		{0, 1 * time.Second, 1500 * time.Millisecond},
		{1, 2 * time.Second, 2500 * time.Millisecond},
		{2, 4 * time.Second, 4500 * time.Millisecond},
		{3, 8 * time.Second, 8500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run("attempt_"+string(rune('0'+tt.attempt)), func(t *testing.T) {
			// Run multiple times to account for jitter
			for i := 0; i < 100; i++ {
				got := calculateBackoff(tt.attempt)
				if got < tt.minWant || got > tt.maxWant {
					t.Errorf("attempt %d: backoff %v not in range [%v, %v]", tt.attempt, got, tt.minWant, tt.maxWant)
					break
				}
			}
		})
	}
}

func TestCalculateBackoff_Cap(t *testing.T) {
	maxBackoff := 30*time.Second + 500*time.Millisecond // 30s + max jitter

	for i := 0; i < 100; i++ {
		got := calculateBackoff(10) // 2^10 = 1024s uncapped → should be capped at 30s
		if got > maxBackoff {
			t.Errorf("attempt 10: backoff %v exceeds cap %v", got, maxBackoff)
			break
		}
		if got < 30*time.Second {
			t.Errorf("attempt 10: backoff %v below 30s base", got)
			break
		}
	}
}
