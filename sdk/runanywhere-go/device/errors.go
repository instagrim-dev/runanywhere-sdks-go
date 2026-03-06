package device

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// =============================================================================
// Error Codes
// =============================================================================

// ErrorCode is a typed string for structured error codes.
type ErrorCode string

// Error codes for structured error information.
const (
	ErrCodeUnknown            ErrorCode = "unknown"
	ErrCodeNotInitialized     ErrorCode = "not_initialized"
	ErrCodeAlreadyInitialized ErrorCode = "already_initialized"
	ErrCodeUnsupported        ErrorCode = "unsupported"
	ErrCodeModelNotFound      ErrorCode = "model_not_found"
	ErrCodeModelLoadFailed    ErrorCode = "model_load_failed"
	ErrCodeOutOfMemory        ErrorCode = "out_of_memory"
	ErrCodeInvalidParam       ErrorCode = "invalid_param"
	ErrCodeGenerationFailed   ErrorCode = "generation_failed"
	ErrCodeBridgeDisconnected ErrorCode = "bridge_disconnected"
	ErrCodeTimeout            ErrorCode = "timeout"
	ErrCodeCancelled          ErrorCode = "cancelled"
	ErrCodeNetworkError       ErrorCode = "network_error"
	ErrCodeWASMError          ErrorCode = "wasm_error"
	ErrCodeWebGPUUnavailable  ErrorCode = "webgpu_unavailable"
)

// =============================================================================
// Sentinel Errors
// =============================================================================

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

// =============================================================================
// RACError - Structured Error
// =============================================================================

// RACError provides structured error information with error codes, messages, and additional context.
type RACError struct {
	// Code is the error code from the list above.
	Code ErrorCode

	// Message is a human-readable message describing the error.
	Message string

	// Details contains additional context about the error.
	Details map[string]interface{}

	// Op is the operation name (for CGO compatibility).
	Op string

	// CodeNum is the numeric C API result code (for CGO compatibility).
	CodeNum int
}

// Error returns a human-readable string representation of the error.
func (e *RACError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("[%s] %s", e.Code, e.Message)
	}
	if e.Op != "" {
		return e.Op + " failed: " + strconv.Itoa(e.CodeNum)
	}
	return "error: " + string(e.Code)
}

// Unwrap returns the underlying error for errors.Is compatibility.
func (e *RACError) Unwrap() error {
	switch e.Code {
	case ErrCodeUnsupported:
		return ErrUnsupported
	case ErrCodeNotInitialized:
		return ErrNotInitialized
	case ErrCodeTimeout:
		return context.DeadlineExceeded
	case ErrCodeCancelled:
		return ErrCancelled
	default:
		return nil
	}
}

// newCGOError creates a RACError from a C API result code. It maps numeric
// codes (see rac/core/rac_error.h) to string codes so Unwrap/errors.Is work.
func newCGOError(op string, codeNum int) *RACError {
	return &RACError{
		Code:    cgoCodeFromNum(codeNum),
		Op:      op,
		CodeNum: codeNum,
	}
}

// cgoCodeFromNum maps C API rac_result_t (rac_error.h) to package error codes.
func cgoCodeFromNum(codeNum int) ErrorCode {
	switch codeNum {
	case -100:
		return ErrCodeNotInitialized // RAC_ERROR_NOT_INITIALIZED
	case -101:
		return ErrCodeAlreadyInitialized // RAC_ERROR_ALREADY_INITIALIZED
	case -110:
		return ErrCodeModelNotFound // RAC_ERROR_MODEL_NOT_FOUND
	case -111:
		return ErrCodeModelLoadFailed // RAC_ERROR_MODEL_LOAD_FAILED
	case -130:
		return ErrCodeGenerationFailed // RAC_ERROR_GENERATION_FAILED
	case -155:
		return ErrCodeTimeout // RAC_ERROR_TIMEOUT
	case -221:
		return ErrCodeOutOfMemory // RAC_ERROR_OUT_OF_MEMORY / INSUFFICIENT_MEMORY
	case -236, -801, -802, -803:
		return ErrCodeUnsupported // RAC_ERROR_NOT_SUPPORTED, FEATURE_NOT_AVAILABLE, etc.
	case -380, -303:
		return ErrCodeCancelled // RAC_ERROR_CANCELLED, RAC_ERROR_STREAM_CANCELLED
	case -106, -259, -251:
		return ErrCodeInvalidParam // RAC_ERROR_INVALID_PARAMETER, INVALID_ARGUMENT, INVALID_INPUT
	case -151, -152, -153:
		return ErrCodeNetworkError // RAC_ERROR_NETWORK_*
	default:
		return ErrCodeUnknown
	}
}
