package device

import (
	"errors"
	"sync"
	"time"
)

// =============================================================================
// Circuit Breaker State
// =============================================================================

// CircuitBreakerState represents the current state of a circuit breaker.
type CircuitBreakerState int

const (
	CircuitBreakerClosed   CircuitBreakerState = iota // Normal operation
	CircuitBreakerOpen                                // Failing — reject calls
	CircuitBreakerHalfOpen                            // Trial — allow limited calls
)

// String returns the human-readable name for the state.
func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitBreakerClosed:
		return "CLOSED"
	case CircuitBreakerOpen:
		return "OPEN"
	case CircuitBreakerHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// =============================================================================
// Circuit Breaker Config
// =============================================================================

// CircuitBreakerConfig holds tuning parameters for a circuit breaker.
type CircuitBreakerConfig struct {
	Name             string
	FailureThreshold int           // failures before opening; default 5
	RecoveryTimeout  time.Duration // time in OPEN before moving to HALF_OPEN; default 30s
	HalfOpenMaxCalls int           // max trial calls in HALF_OPEN; default 3
}

func (c CircuitBreakerConfig) withDefaults() CircuitBreakerConfig {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 5
	}
	if c.RecoveryTimeout <= 0 {
		c.RecoveryTimeout = 30 * time.Second
	}
	if c.HalfOpenMaxCalls <= 0 {
		c.HalfOpenMaxCalls = 3
	}
	return c
}

// =============================================================================
// Circuit Breaker Status
// =============================================================================

// CircuitBreakerStatus is a snapshot of the circuit breaker's health.
type CircuitBreakerStatus struct {
	State           CircuitBreakerState
	FailureCount    int
	LastFailureTime time.Time
	IsHealthy       bool
}

// =============================================================================
// Sentinel Error
// =============================================================================

// ErrCircuitBreakerOpen is returned when Execute is called on an open breaker.
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

// =============================================================================
// Circuit Breaker
// =============================================================================

// CircuitBreaker implements the circuit breaker pattern with three states:
// CLOSED → OPEN → HALF_OPEN → CLOSED.
type CircuitBreaker struct {
	mu              sync.Mutex
	config          CircuitBreakerConfig
	state           CircuitBreakerState
	failureCount    int
	halfOpenCalls   int
	lastFailureTime time.Time
}

// NewCircuitBreaker creates a circuit breaker with the given config (defaults applied).
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	cfg = cfg.withDefaults()
	return &CircuitBreaker{
		config: cfg,
		state:  CircuitBreakerClosed,
	}
}

// Execute runs fn if the breaker allows it.
// Returns ErrCircuitBreakerOpen if the breaker is open and the recovery timeout
// has not elapsed.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.beforeCall(); err != nil {
		return err
	}
	err := fn()
	cb.afterCall(err)
	return err
}

// Status returns a snapshot of the breaker's current state.
func (cb *CircuitBreaker) Status() CircuitBreakerStatus {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return CircuitBreakerStatus{
		State:           cb.state,
		FailureCount:    cb.failureCount,
		LastFailureTime: cb.lastFailureTime,
		IsHealthy:       cb.state == CircuitBreakerClosed,
	}
}

// Reset forces the breaker back to CLOSED with zero failure count.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitBreakerClosed
	cb.failureCount = 0
	cb.halfOpenCalls = 0
	cb.lastFailureTime = time.Time{}
}

// beforeCall checks the state machine and decides whether to allow the call.
func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitBreakerClosed:
		return nil

	case CircuitBreakerOpen:
		// Check if recovery timeout has elapsed → move to HALF_OPEN.
		if time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout {
			cb.state = CircuitBreakerHalfOpen
			cb.halfOpenCalls = 1 // count this call as the first trial
			return nil
		}
		return ErrCircuitBreakerOpen

	case CircuitBreakerHalfOpen:
		if cb.halfOpenCalls >= cb.config.HalfOpenMaxCalls {
			// Exceeded trial budget → trip back to OPEN.
			// Do not update lastFailureTime: no actual call failed, so the
			// recovery timer should not be reset unnecessarily.
			cb.state = CircuitBreakerOpen
			return ErrCircuitBreakerOpen
		}
		cb.halfOpenCalls++
		return nil
	}

	return nil
}

// afterCall updates the state machine based on whether the call succeeded.
func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err == nil {
		// Success path.
		switch cb.state {
		case CircuitBreakerHalfOpen, CircuitBreakerOpen:
			// Trial call succeeded (or raced with budget exceeded) → close the breaker.
			cb.state = CircuitBreakerClosed
			cb.failureCount = 0
			cb.halfOpenCalls = 0
		case CircuitBreakerClosed:
			// Reset failure count on success (consecutive failures model).
			cb.failureCount = 0
		}
		return
	}

	// Failure path.
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitBreakerClosed:
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.state = CircuitBreakerOpen
		}
	case CircuitBreakerHalfOpen:
		// Any failure in half-open trips back to open.
		cb.state = CircuitBreakerOpen
	}
}

// =============================================================================
// Generic Execute Helper (package-level function)
// =============================================================================

// ExecuteWithResult runs fn through the circuit breaker and returns its result.
// This is a package-level function because Go does not allow generic methods.
func ExecuteWithResult[T any](cb *CircuitBreaker, fn func() (T, error)) (T, error) {
	var result T
	err := cb.Execute(func() error {
		var innerErr error
		result, innerErr = fn()
		return innerErr
	})
	return result, err
}

// =============================================================================
// Circuit Breaker Registry (same pattern as metrics.go)
// =============================================================================

var (
	cbMu       sync.RWMutex
	cbRegistry = map[string]*CircuitBreaker{}
)

// GetCircuitBreaker returns the named circuit breaker, creating it if absent.
// If cfg is provided (first element used), it is used for creation only; an
// existing breaker with the same name is returned as-is.
func GetCircuitBreaker(name string, cfg ...CircuitBreakerConfig) *CircuitBreaker {
	cbMu.RLock()
	if cb, ok := cbRegistry[name]; ok {
		cbMu.RUnlock()
		return cb
	}
	cbMu.RUnlock()

	cbMu.Lock()
	defer cbMu.Unlock()

	// Double-check after acquiring write lock.
	if cb, ok := cbRegistry[name]; ok {
		return cb
	}

	var config CircuitBreakerConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}
	config.Name = name
	cb := NewCircuitBreaker(config)
	cbRegistry[name] = cb
	return cb
}

// CircuitBreakerStatuses returns a snapshot of all registered breakers.
func CircuitBreakerStatuses() map[string]CircuitBreakerStatus {
	cbMu.RLock()
	defer cbMu.RUnlock()
	out := make(map[string]CircuitBreakerStatus, len(cbRegistry))
	for name, cb := range cbRegistry {
		out[name] = cb.Status()
	}
	return out
}

// ResetAllCircuitBreakers resets all registered breakers to CLOSED.
func ResetAllCircuitBreakers() {
	cbMu.RLock()
	defer cbMu.RUnlock()
	for _, cb := range cbRegistry {
		cb.Reset()
	}
}
