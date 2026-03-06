//go:build wasip1 && wasm

package device

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

// =============================================================================
// Host Imports (go:wasmimport)
// =============================================================================
//
// The WASI host must provide a "runanywhere" module with these four functions.
// All data exchange uses JSON payloads in linear memory.

// hostCall performs a synchronous host call.
// op/opLen: operation name (e.g. "llm.generate")
// req/reqLen: JSON request payload
// resp/respCap: response buffer and its capacity
// Returns bytes written to resp, or negative error code.
//
//go:wasmimport runanywhere call
func hostCall(op unsafe.Pointer, opLen int32, req unsafe.Pointer, reqLen int32, resp unsafe.Pointer, respCap int32) int32

// hostStreamStart begins a streaming host call.
// Returns a stream handle (>0) or negative error code.
//
//go:wasmimport runanywhere call_stream_start
func hostStreamStart(op unsafe.Pointer, opLen int32, req unsafe.Pointer, reqLen int32) int64

// hostStreamNext reads the next chunk from a stream.
// chunk/chunkCap: buffer for the next chunk
// Returns bytes written (>0), 0 = stream done, negative = error.
//
//go:wasmimport runanywhere call_stream_next
func hostStreamNext(stream int64, chunk unsafe.Pointer, chunkCap int32) int32

// hostStreamCancel cancels and releases a stream on the host.
// Returns 0 on success, negative on error.
//
//go:wasmimport runanywhere call_stream_cancel
func hostStreamCancel(stream int64) int32

// =============================================================================
// Go-friendly wrappers
// =============================================================================

const (
	defaultResponseCap = 64 * 1024 // 64KB initial response buffer
	maxResponseCap     = 16 << 20  // 16MB max response buffer
	defaultChunkCap    = 4 * 1024  // 4KB stream chunk buffer
	errOverflow        = -2        // host signals response exceeds buffer
)

// callHost performs a synchronous host operation and returns the JSON response.
// It retries with a larger buffer if the response overflows.
func callHost(op string, request []byte) ([]byte, error) {
	if len(request) == 0 {
		request = []byte("{}")
	}

	opBytes := []byte(op)
	respCap := int32(defaultResponseCap)

	for respCap <= int32(maxResponseCap) {
		resp := make([]byte, respCap)

		n := hostCall(
			unsafe.Pointer(&opBytes[0]), int32(len(opBytes)),
			unsafe.Pointer(&request[0]), int32(len(request)),
			unsafe.Pointer(&resp[0]), respCap,
		)

		if n == int32(errOverflow) {
			respCap *= 2
			continue
		}
		if n < 0 {
			return nil, &RACError{
				Code:    ErrCodeWASMError,
				Message: fmt.Sprintf("host call %q failed with code %d", op, n),
			}
		}

		return resp[:n], nil
	}

	return nil, &RACError{
		Code:    ErrCodeWASMError,
		Message: fmt.Sprintf("host call %q response exceeds max buffer (%d bytes)", op, maxResponseCap),
	}
}

// callHostStreamStart begins a streaming host call and returns the stream handle.
func callHostStreamStart(op string, request []byte) (int64, error) {
	if len(request) == 0 {
		request = []byte("{}")
	}

	opBytes := []byte(op)

	handle := hostStreamStart(
		unsafe.Pointer(&opBytes[0]), int32(len(opBytes)),
		unsafe.Pointer(&request[0]), int32(len(request)),
	)

	if handle < 0 {
		return 0, &RACError{
			Code:    ErrCodeWASMError,
			Message: fmt.Sprintf("host stream start %q failed with code %d", op, handle),
		}
	}

	return handle, nil
}

// callHostStreamNext reads the next chunk from a stream.
// Returns (chunk, done, error). When done is true, the stream is finished.
func callHostStreamNext(stream int64) ([]byte, bool, error) {
	chunkCap := int32(defaultChunkCap)
	for chunkCap <= int32(maxResponseCap) {
		chunk := make([]byte, chunkCap)
		n := hostStreamNext(stream, unsafe.Pointer(&chunk[0]), chunkCap)

		if n == int32(errOverflow) {
			chunkCap *= 2
			continue
		}
		if n < 0 {
			return nil, false, &RACError{
				Code:    ErrCodeWASMError,
				Message: fmt.Sprintf("host stream next failed with code %d", n),
			}
		}
		if n == 0 {
			return nil, true, nil // stream done
		}

		return chunk[:n], false, nil
	}

	return nil, false, &RACError{
		Code:    ErrCodeWASMError,
		Message: fmt.Sprintf("host stream next response exceeds max buffer (%d bytes)", maxResponseCap),
	}
}

// callHostStreamCancel requests host-side stream cancellation/cleanup.
func callHostStreamCancel(stream int64) error {
	n := hostStreamCancel(stream)
	if n < 0 {
		return &RACError{
			Code:    ErrCodeWASMError,
			Message: fmt.Sprintf("host stream cancel failed with code %d", n),
		}
	}
	return nil
}

// =============================================================================
// JSON helpers
// =============================================================================

// hostErrorResponse is the JSON error format returned by the host.
type hostErrorResponse struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// parseHostError checks if the response is an error and converts it to a RACError.
// Returns nil if the response is not an error.
func parseHostError(data []byte) error {
	var resp hostErrorResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil // not an error response
	}
	if resp.Error == nil {
		return nil
	}
	return &RACError{
		Code:    mapHostErrorCode(resp.Error.Code),
		Message: resp.Error.Message,
	}
}

// mapHostErrorCode maps host error code strings to SDK error codes.
func mapHostErrorCode(code string) ErrorCode {
	switch code {
	case "unsupported":
		return ErrCodeUnsupported
	case "not_initialized":
		return ErrCodeNotInitialized
	case "model_not_found":
		return ErrCodeModelNotFound
	case "model_load_failed":
		return ErrCodeModelLoadFailed
	case "out_of_memory":
		return ErrCodeOutOfMemory
	case "invalid_param":
		return ErrCodeInvalidParam
	case "generation_failed":
		return ErrCodeGenerationFailed
	case "timeout":
		return ErrCodeTimeout
	case "cancelled":
		return ErrCodeCancelled
	default:
		return ErrCodeWASMError
	}
}
