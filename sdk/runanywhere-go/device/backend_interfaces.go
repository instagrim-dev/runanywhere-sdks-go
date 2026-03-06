package device

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
)

// =============================================================================
// Backend Interface
// =============================================================================

// Backend defines the interface for all device backends (native, WASM browser, WASI, stub).
// Each backend implementation provides access to LLM, STT, TTS, and Embeddings capabilities.
type Backend interface {
	// Init initializes the backend with the given configuration.
	// Must be called before any other operations.
	Init(ctx context.Context, config *Config) error

	// Shutdown releases all resources held by the backend.
	// After Shutdown, the backend cannot be used unless re-initialized.
	Shutdown(ctx context.Context) error

	// Capabilities returns the set of capabilities available after initialization.
	// This allows runtime detection based on loaded backends, WebGPU availability,
	// memory constraints, and host-provided capabilities.
	Capabilities() CapabilitySet

	// IsInitialized returns whether the backend has been successfully initialized.
	IsInitialized() bool

	// Mode returns the BackendMode that identifies this backend type.
	// Used by FallbackChain.TryInitWithMode to filter backends by mode.
	Mode() BackendMode
}

// =============================================================================
// ModelSource Interface
// =============================================================================

// ModelSource defines where a model is loaded from.
// Different backends load models from different sources:
// - Native: local filesystem path
// - Browser WASM: URL to Web SDK model or base64-encoded model
// - WASI: host-provided path or remote URL
//
// NOTE: This interface is intentionally part of the public surface even where
// specific backend flows do not call Resolve() yet. It is used by option
// structs and should not be removed as dead code without a replacement.
type ModelSource interface {
	// Resolve returns a reader for the model data.
	Resolve(ctx context.Context) (io.ReadCloser, error)
}

// LocalModel represents a model loaded from the local filesystem.
type LocalModel string

// Resolve returns a reader for the local model file.
func (l LocalModel) Resolve(ctx context.Context) (io.ReadCloser, error) {
	return os.Open(string(l))
}

// RemoteModel represents a model loaded from a remote URL.
type RemoteModel string

// RemoteModelHTTPClient is used by RemoteModel.Resolve. Override this to apply
// custom timeout/transport behavior.
var RemoteModelHTTPClient = http.DefaultClient

// Resolve returns a reader for the remote model. The caller's context controls
// timeouts and cancellation; no per-client timeout is applied internally.
func (r RemoteModel) Resolve(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, string(r), nil)
	if err != nil {
		return nil, err
	}
	client := RemoteModelHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &RACError{
			Code:    ErrCodeModelNotFound,
			Message: "failed to fetch model: " + resp.Status,
		}
	}
	return resp.Body, nil
}

// Base64Model represents a model provided as base64-encoded data.
// It decodes the entire model into memory; suitable only for small models or testing.
type Base64Model string

// Resolve returns a reader for the base64-decoded model.
func (b Base64Model) Resolve(ctx context.Context) (io.ReadCloser, error) {
	data, err := base64.StdEncoding.DecodeString(string(b))
	if err != nil {
		return nil, &RACError{
			Code:    ErrCodeInvalidParam,
			Message: "invalid base64 model data: " + err.Error(),
		}
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// =============================================================================
// Backend Mode
// =============================================================================

// BackendMode specifies which backend to use.
type BackendMode int

const (
	// BackendModeAuto detects the appropriate backend based on build tags and runtime.
	BackendModeAuto BackendMode = iota

	// BackendModeNative uses the native CGO backend.
	BackendModeNative

	// BackendModeWASM uses the WASM backend (browser or WASI based on runtime).
	BackendModeWASM

	// BackendModeWASMBrowser forces browser WASM backend.
	BackendModeWASMBrowser

	// BackendModeWASI forces WASI backend.
	BackendModeWASI

	// BackendModeStub uses the stub backend (always returns ErrUnsupported).
	BackendModeStub
)

// String returns the string representation of BackendMode.
func (m BackendMode) String() string {
	switch m {
	case BackendModeAuto:
		return "auto"
	case BackendModeNative:
		return "native"
	case BackendModeWASM:
		return "wasm"
	case BackendModeWASMBrowser:
		return "wasm-browser"
	case BackendModeWASI:
		return "wasi"
	case BackendModeStub:
		return "stub"
	default:
		return "unknown"
	}
}
