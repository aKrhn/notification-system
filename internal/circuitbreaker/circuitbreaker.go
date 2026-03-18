package circuitbreaker

import (
	"sync"
	"time"
)

type State int

const (
	Closed   State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type CircuitBreaker struct {
	mu          sync.Mutex
	state       State
	failures    int
	successes   int
	total       int
	lastFailure time.Time
	threshold   float64
	minSamples  int
	timeout     time.Duration
	windowStart time.Time
	window      time.Duration
}

func New() *CircuitBreaker {
	return &CircuitBreaker{
		state:       Closed,
		threshold:   0.5,
		minSamples:  10,
		timeout:     30 * time.Second,
		window:      60 * time.Second,
		windowStart: time.Now(),
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.resetWindowIfExpired()

	switch cb.state {
	case Closed:
		return true
	case Open:
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.state = HalfOpen
			return true
		}
		return false
	case HalfOpen:
		return false
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successes++
	cb.total++

	if cb.state == HalfOpen {
		cb.state = Closed
		cb.reset()
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.total++
	cb.lastFailure = time.Now()

	if cb.state == HalfOpen {
		cb.state = Open
		return
	}

	if cb.state == Closed && cb.total >= cb.minSamples {
		failureRate := float64(cb.failures) / float64(cb.total)
		if failureRate > cb.threshold {
			cb.state = Open
		}
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) resetWindowIfExpired() {
	if time.Since(cb.windowStart) > cb.window {
		cb.reset()
	}
}

func (cb *CircuitBreaker) reset() {
	cb.failures = 0
	cb.successes = 0
	cb.total = 0
	cb.windowStart = time.Now()
}
