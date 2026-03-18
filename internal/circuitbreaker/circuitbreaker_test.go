package circuitbreaker

import (
	"testing"
	"time"
)

func TestInitialState(t *testing.T) {
	cb := New()
	if cb.State() != Closed {
		t.Errorf("expected Closed, got %s", cb.State())
	}
}

func TestClosedAllowsRequests(t *testing.T) {
	cb := New()
	if !cb.Allow() {
		t.Error("expected Allow() = true when closed")
	}
}

func TestTripsAfterThreshold(t *testing.T) {
	cb := New()

	// Interleave failures and successes to reach 10 samples with >50% failure
	// 4 successes, then 6 failures = 60% failure rate, checked on last RecordFailure
	for i := 0; i < 4; i++ {
		cb.RecordSuccess()
	}
	for i := 0; i < 6; i++ {
		cb.RecordFailure()
	}

	if cb.State() != Open {
		t.Errorf("expected Open after exceeding threshold, got %s", cb.State())
	}
}

func TestOpenDeniesRequests(t *testing.T) {
	cb := New()

	// Trip the breaker
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
	}

	if cb.Allow() {
		t.Error("expected Allow() = false when open")
	}
}

func TestBelowMinSamplesNoTrip(t *testing.T) {
	cb := New()

	// 9 failures, below minSamples=10
	for i := 0; i < 9; i++ {
		cb.RecordFailure()
	}

	if cb.State() != Closed {
		t.Errorf("expected Closed with <10 samples, got %s", cb.State())
	}
}

func TestHalfOpenAfterTimeout(t *testing.T) {
	cb := &CircuitBreaker{
		state:       Closed,
		threshold:   0.5,
		minSamples:  10,
		timeout:     50 * time.Millisecond, // short timeout for testing
		window:      60 * time.Second,
		windowStart: time.Now(),
	}

	// Trip breaker
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
	}
	if cb.State() != Open {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Should transition to HalfOpen and allow one request
	if !cb.Allow() {
		t.Error("expected Allow() = true after timeout (half-open)")
	}
	if cb.State() != HalfOpen {
		t.Errorf("expected HalfOpen, got %s", cb.State())
	}

	// Second request should be denied (only one allowed in half-open)
	if cb.Allow() {
		t.Error("expected Allow() = false for second request in half-open")
	}
}

func TestHalfOpenSuccessCloses(t *testing.T) {
	cb := &CircuitBreaker{
		state:       HalfOpen,
		threshold:   0.5,
		minSamples:  10,
		timeout:     30 * time.Second,
		window:      60 * time.Second,
		windowStart: time.Now(),
	}

	cb.RecordSuccess()

	if cb.State() != Closed {
		t.Errorf("expected Closed after success in half-open, got %s", cb.State())
	}
}

func TestHalfOpenFailureReopens(t *testing.T) {
	cb := &CircuitBreaker{
		state:       HalfOpen,
		threshold:   0.5,
		minSamples:  10,
		timeout:     30 * time.Second,
		window:      60 * time.Second,
		windowStart: time.Now(),
	}

	cb.RecordFailure()

	if cb.State() != Open {
		t.Errorf("expected Open after failure in half-open, got %s", cb.State())
	}
}

func TestWindowReset(t *testing.T) {
	cb := &CircuitBreaker{
		state:       Closed,
		failures:    5,
		successes:   5,
		total:       10,
		threshold:   0.5,
		minSamples:  10,
		timeout:     30 * time.Second,
		window:      50 * time.Millisecond, // short window for testing
		windowStart: time.Now().Add(-100 * time.Millisecond),
	}

	// Allow() should trigger window reset
	cb.Allow()

	cb.mu.Lock()
	if cb.total != 0 {
		t.Errorf("expected counters reset after window, total=%d", cb.total)
	}
	cb.mu.Unlock()
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Closed, "closed"},
		{Open, "open"},
		{HalfOpen, "half-open"},
		{State(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
