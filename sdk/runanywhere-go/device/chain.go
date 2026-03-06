package device

import (
	"context"
	"fmt"
	"sync/atomic"
)

// =============================================================================
// Fallback Chain
// =============================================================================

// FallbackChain defines a chain of backends to try in order.
// It attempts to initialize each backend until one succeeds.
type FallbackChain struct {
	backends []Backend
}

// NewFallbackChain creates a new FallbackChain with the given backends.
// The backends are tried in the order they are provided.
func NewFallbackChain(backends ...Backend) *FallbackChain {
	return &FallbackChain{
		backends: backends,
	}
}

// TryInit attempts to initialize each backend in order until one succeeds.
// Returns the successful backend and nil error, or the last error if all fail.
func (f *FallbackChain) TryInit(ctx context.Context, cfg *Config) (Backend, error) {
	var lastErr error

	for _, be := range f.backends {
		if err := be.Init(ctx, cfg); err != nil {
			lastErr = err
			// Continue to next backend
			continue
		}

		// Successfully initialized
		return be, nil
	}

	if lastErr == nil {
		lastErr = &RACError{
			Code:    ErrCodeUnknown,
			Message: "no backends available",
		}
	}

	return nil, fmt.Errorf("all backends failed: %w", lastErr)
}

// TryInitWithMode attempts to initialize backends based on the specified mode.
// If mode is BackendModeAuto, tries all backends in order.
// Otherwise, only backends whose Mode() matches the requested mode are tried.
func (f *FallbackChain) TryInitWithMode(ctx context.Context, cfg *Config, mode BackendMode) (Backend, error) {
	if mode == BackendModeAuto {
		return f.TryInit(ctx, cfg)
	}

	var lastErr error
	for _, be := range f.backends {
		if !modeMatches(be.Mode(), mode) {
			continue
		}
		if err := be.Init(ctx, cfg); err != nil {
			lastErr = err
			continue
		}
		return be, nil
	}

	if lastErr == nil {
		lastErr = &RACError{
			Code:    ErrCodeUnsupported,
			Message: mode.String() + " backend not available",
		}
	}
	return nil, fmt.Errorf("all %s backends failed: %w", mode.String(), lastErr)
}

// modeMatches reports whether a backend's mode satisfies the requested mode.
// BackendModeWASM matches both BackendModeWASMBrowser and BackendModeWASI.
func modeMatches(backendMode, requestedMode BackendMode) bool {
	if requestedMode == BackendModeWASM {
		return backendMode == BackendModeWASMBrowser || backendMode == BackendModeWASI
	}
	return backendMode == requestedMode
}

// Add adds a backend to the end of the chain.
func (f *FallbackChain) Add(backend Backend) *FallbackChain {
	f.backends = append(f.backends, backend)
	return f
}

// Len returns the number of backends in the chain.
func (f *FallbackChain) Len() int {
	return len(f.backends)
}

// Get returns the backend at the given index.
func (f *FallbackChain) Get(i int) Backend {
	if i < 0 || i >= len(f.backends) {
		return nil
	}
	return f.backends[i]
}

// =============================================================================
// Stub Backend
// =============================================================================

// stubBackend implements Backend for testing and fallback purposes.
// All capabilities return CapabilitySetNone.
type stubBackend struct {
	initialized atomic.Bool
}

func (b *stubBackend) Init(ctx context.Context, cfg *Config) error {
	b.initialized.Store(true)
	return nil
}

func (b *stubBackend) Shutdown(ctx context.Context) error {
	b.initialized.Store(false)
	return nil
}

func (b *stubBackend) Capabilities() CapabilitySet {
	return CapabilitySetNone
}

func (b *stubBackend) IsInitialized() bool {
	return b.initialized.Load()
}

func (b *stubBackend) Mode() BackendMode {
	return BackendModeStub
}
