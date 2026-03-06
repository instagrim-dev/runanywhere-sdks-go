//go:build wasip1 && wasm

package device

import (
	"context"
	"encoding/json"
	"sync"
)

// =============================================================================
// WASI Backend
// =============================================================================

// wasiBackend implements the Backend interface for WASI environments.
// It communicates with the host runtime via go:wasmimport host functions.
type wasiBackend struct {
	mu           sync.RWMutex
	initialized  bool
	config       *Config
	capabilities CapabilitySet
	llmHandles   *handleRegistry[*wasiLLM]
	sttHandles   *handleRegistry[*wasiSTT]
	ttsHandles   *handleRegistry[*wasiTTS]
	embHandles   *handleRegistry[*wasiEmbeddings]
}

func newWASIBackend() *wasiBackend {
	return &wasiBackend{
		llmHandles: newHandleRegistry[*wasiLLM](),
		sttHandles: newHandleRegistry[*wasiSTT](),
		ttsHandles: newHandleRegistry[*wasiTTS](),
		embHandles: newHandleRegistry[*wasiEmbeddings](),
	}
}

// wasiBackendInstance is the global WASI backend instance.
var wasiBackendInstance = newWASIBackend()

// Init initializes the WASI backend by calling the host's init operation.
// ctx is reserved for future cancellation/deadline support across host calls.
func (b *wasiBackend) Init(ctx context.Context, cfg *Config) error {
	if err := checkContextNotDone(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized {
		return &RACError{
			Code:    ErrCodeAlreadyInitialized,
			Message: "WASI backend already initialized",
		}
	}

	b.config = cfg
	if b.config == nil {
		b.config = &Config{}
	}

	// Call host init
	reqJSON, err := json.Marshal(map[string]interface{}{
		"logLevel": b.config.LogLevel,
		"logTag":   b.config.LogTag,
	})
	if err != nil {
		return &RACError{
			Code:    ErrCodeInvalidParam,
			Message: "failed to marshal init request: " + err.Error(),
		}
	}

	resp, err := callHost("init", reqJSON)
	if err != nil {
		return err
	}
	if hostErr := parseHostError(resp); hostErr != nil {
		return hostErr
	}

	// Parse capabilities from init response
	var initResult struct {
		Success      bool     `json:"success"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(resp, &initResult); err != nil {
		return &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse host init response: " + err.Error(),
		}
	}
	if !initResult.Success {
		return &RACError{
			Code:    ErrCodeWASMError,
			Message: "host init returned success=false",
		}
	}

	b.capabilities = ParseCapabilityStrings(initResult.Capabilities)
	b.initialized = true
	Publish(NewLifecycleEvent("initialized"))
	return nil
}

// Shutdown shuts down the WASI backend.
// ctx is reserved for future cancellation/deadline support across host calls.
func (b *wasiBackend) Shutdown(ctx context.Context) error {
	if err := checkContextNotDone(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	if !b.initialized {
		b.mu.Unlock()
		return nil
	}
	b.initialized = false
	b.capabilities = CapabilitySetNone
	b.mu.Unlock()

	// Close all handles
	b.llmHandles.CloseAll(func(h *wasiLLM) error { return h.Close() })
	b.sttHandles.CloseAll(func(h *wasiSTT) error { return h.Close() })
	b.ttsHandles.CloseAll(func(h *wasiTTS) error { return h.Close() })
	b.embHandles.CloseAll(func(h *wasiEmbeddings) error { return h.Close() })

	// Call host shutdown
	if _, err := callHost("shutdown", nil); err != nil {
		LogBridge.Error("shutdown error", err)
	}
	Publish(NewLifecycleEvent("shutdown"))
	return nil
}

// Capabilities returns the available capabilities.
func (b *wasiBackend) Capabilities() CapabilitySet {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.capabilities
}

// Mode returns BackendModeWASI.
func (b *wasiBackend) Mode() BackendMode { return BackendModeWASI }

// IsInitialized returns whether the backend is initialized.
func (b *wasiBackend) IsInitialized() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.initialized
}

// =============================================================================
// Handle Management (delegates to handleRegistry)
// =============================================================================

func (b *wasiBackend) registerLLM(id int64, llm *wasiLLM) { b.llmHandles.Register(id, llm) }
func (b *wasiBackend) unregisterLLM(id int64)             { b.llmHandles.Unregister(id) }
func (b *wasiBackend) registerSTT(id int64, stt *wasiSTT) { b.sttHandles.Register(id, stt) }
func (b *wasiBackend) unregisterSTT(id int64)             { b.sttHandles.Unregister(id) }
func (b *wasiBackend) registerTTS(id int64, tts *wasiTTS) { b.ttsHandles.Register(id, tts) }
func (b *wasiBackend) unregisterTTS(id int64)             { b.ttsHandles.Unregister(id) }
func (b *wasiBackend) registerEmbeddings(id int64, emb *wasiEmbeddings) {
	b.embHandles.Register(id, emb)
}
func (b *wasiBackend) unregisterEmbeddings(id int64) { b.embHandles.Unregister(id) }

// =============================================================================
// Package-Level API (delegates to wasiBackendInstance)
// =============================================================================

// Init initializes the device stack for WASI environment.
func Init(ctx context.Context) error {
	return InitWithConfig(ctx, nil)
}

// InitWithConfig initializes with explicit configuration.
func InitWithConfig(ctx context.Context, cfg *Config) error {
	return wasiBackendInstance.Init(ctx, cfg)
}

// Shutdown shuts down the device stack.
func Shutdown() error {
	return wasiBackendInstance.Shutdown(context.Background())
}

// isInitialized reports whether the device is initialized.
func isInitialized() bool {
	return wasiBackendInstance.IsInitialized()
}
