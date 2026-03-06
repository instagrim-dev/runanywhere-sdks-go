//go:build !cgo && !js && !wasip1

package device

import "context"

// Init initializes the device stack (platform adapter, rac_init, backend registration).
// This build is the stub: Init always returns ErrUnsupported.
func Init(ctx context.Context) error {
	return ErrUnsupported
}

// InitWithConfig is like Init but with explicit config. Stub returns ErrUnsupported.
func InitWithConfig(ctx context.Context, cfg *Config) error {
	return ErrUnsupported
}

// Shutdown shuts down the device stack. Stub returns ErrUnsupported.
func Shutdown() error {
	return ErrUnsupported
}

// isInitialized reports whether the device is initialized. Stub always returns false.
func isInitialized() bool {
	return false
}
