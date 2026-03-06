//go:build js && wasm

package device

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
)

// =============================================================================
// WASM TTS Implementation
// =============================================================================

// wasmTTS is the TTS handle for browser WASM.
type wasmTTS struct {
	mu      sync.RWMutex
	handle  int64
	backend *wasmBrowserBackend
}

// NewTTS creates a new TTS handle for the given voice/model path.
func NewTTS(ctx context.Context, voicePath string, opts *TTSOptions) (*TTS, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasmBackend.IsInitialized() {
		return nil, ErrNotInitialized
	}

	// Check capability
	caps := wasmBackend.Capabilities()
	if !caps.Has(CapabilityTTS) {
		return nil, ErrUnsupported
	}

	optsJSON := "{}"
	if opts != nil {
		data, err := json.Marshal(opts)
		if err != nil {
			return nil, &RACError{
				Code:    ErrCodeInvalidParam,
				Message: "failed to marshal options: " + err.Error(),
			}
		}
		optsJSON = string(data)
	}

	args := map[string]interface{}{
		"voicePath": voicePath,
		"options":   optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal createTTS args: " + err.Error()}
	}

	result, err := wasmBackend.callBridgeSync("createTTS", string(argsJSON))
	if err != nil {
		return nil, err
	}

	var createResult struct {
		Success bool   `json:"success"`
		Handle  int64  `json:"handle"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &createResult); err != nil {
		return nil, &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse createTTS result: " + err.Error(),
		}
	}

	if !createResult.Success {
		return nil, &RACError{
			Code:    ErrCodeModelLoadFailed,
			Message: createResult.Error,
		}
	}

	tts := &wasmTTS{
		handle:  createResult.Handle,
		backend: wasmBackend,
	}

	// Register the handle using the bridge's ID
	wasmBackend.registerTTS(tts.handle, tts)

	return tts, nil
}

// Synthesize synthesizes speech from text.
func (t *wasmTTS) Synthesize(ctx context.Context, text string, opts *TTSOptions) ([]byte, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	t.mu.RLock()
	handle := t.handle
	t.mu.RUnlock()
	if handle == 0 {
		return nil, ErrNotInitialized
	}

	args := map[string]interface{}{
		"handle": handle,
		"text":   text,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal ttsSynthesize args: " + err.Error()}
	}

	result, err := t.backend.callBridgeSync("ttsSynthesize", string(argsJSON))
	if err != nil {
		return nil, err
	}

	var synthResult struct {
		Success   bool   `json:"success"`
		AudioData string `json:"audioData"` // base64 encoded
		Error     string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &synthResult); err != nil {
		return nil, &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse synthesize result: " + err.Error(),
		}
	}

	if !synthResult.Success {
		return nil, &RACError{
			Code:    ErrCodeGenerationFailed,
			Message: synthResult.Error,
		}
	}

	// Decode base64 audio data returned by the bridge
	audioBytes, err := base64.StdEncoding.DecodeString(synthResult.AudioData)
	if err != nil {
		return nil, &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to decode base64 audio: " + err.Error(),
		}
	}
	return audioBytes, nil
}

// SynthesizeStream returns a stream iterator for streaming synthesis.
func (t *wasmTTS) SynthesizeStream(ctx context.Context, text string, opts *TTSOptions) (TTSStreamIterator, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	return nil, ErrUnsupported
}

// Close releases the handle.
func (t *wasmTTS) Close() error {
	t.mu.Lock()
	handle := t.handle
	if handle == 0 {
		t.mu.Unlock()
		return nil
	}
	t.handle = 0
	t.mu.Unlock()

	args := map[string]interface{}{
		"handle": handle,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal closeTTS args: " + err.Error()}
	}
	if _, err := t.backend.callBridgeSync("closeTTS", string(argsJSON)); err != nil {
		LogBridge.Warn("closeTTS error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("closeTTS", ErrCodeWASMError)
	}

	t.backend.unregisterTTS(handle)

	return nil
}
