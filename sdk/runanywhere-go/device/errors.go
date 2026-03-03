package device

import (
	"errors"
	"strconv"
)

// Sentinel errors for the device API. Use errors.Is to check.
var (
	// ErrUnsupported is returned when the device package is built without CGO
	// (stubs) or when the operation is not available (e.g. missing backend).
	ErrUnsupported = errors.New("on-device inference requires CGO and runanywhere-commons shared libraries")

	// ErrNotInitialized is returned when a New* constructor or handle method
	// is called before Init() has been called successfully.
	ErrNotInitialized = errors.New("device not initialized: call Init() first")

	// ErrHandlesStillOpen is returned when Shutdown() is called while one or
	// more handles (LLM, STT, TTS, Embeddings) are still open. Close all handles
	// before calling Shutdown().
	ErrHandlesStillOpen = errors.New("cannot shutdown: one or more device handles are still open")

	// ErrCancelled is returned when an operation is cancelled via context
	// (e.g. context.Canceled or context.DeadlineExceeded). Callers may also
	// check context.Canceled with errors.Is.
	ErrCancelled = errors.New("operation cancelled")
)

// RACError wraps a C API result code so callers can use errors.As to inspect the code.
type RACError struct {
	Op   string // operation name (e.g. "rac_llm_llamacpp_create")
	Code int    // RAC result code from C (see rac_error.h)
}

func (e *RACError) Error() string {
	return e.Op + " failed: " + strconv.Itoa(e.Code)
}
