//go:build js && wasm

package device

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// =============================================================================
// WASM STT Implementation
// =============================================================================

// wasmSTT is the STT handle for browser WASM.
type wasmSTT struct {
	mu      sync.RWMutex
	handle  int64
	backend *wasmBrowserBackend
}

// NewSTT creates a new STT handle for the given model path.
func NewSTT(ctx context.Context, modelPath string, opts *STTOptions) (*STT, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasmBackend.IsInitialized() {
		return nil, ErrNotInitialized
	}

	// Check capability
	caps := wasmBackend.Capabilities()
	if !caps.Has(CapabilitySTT) {
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
		"modelPath": modelPath,
		"options":   optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal createSTT args: " + err.Error()}
	}

	result, err := wasmBackend.callBridgeSync("createSTT", string(argsJSON))
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
			Message: "failed to parse createSTT result: " + err.Error(),
		}
	}

	if !createResult.Success {
		return nil, &RACError{
			Code:    ErrCodeModelLoadFailed,
			Message: createResult.Error,
		}
	}

	stt := &wasmSTT{
		handle:  createResult.Handle,
		backend: wasmBackend,
	}

	// Register the handle using the bridge's ID
	wasmBackend.registerSTT(stt.handle, stt)

	return stt, nil
}

// Transcribe transcribes audio data.
func (s *wasmSTT) Transcribe(ctx context.Context, audioData []byte, opts *STTOptions) (string, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return "", err
	}
	s.mu.RLock()
	handle := s.handle
	s.mu.RUnlock()
	if handle == 0 {
		return "", ErrNotInitialized
	}

	optsJSON := "{}"
	if opts != nil {
		data, err := json.Marshal(opts)
		if err != nil {
			return "", &RACError{
				Code:    ErrCodeInvalidParam,
				Message: "failed to marshal options: " + err.Error(),
			}
		}
		optsJSON = string(data)
	}
	args := map[string]interface{}{
		"handle":    handle,
		"audioData": base64.StdEncoding.EncodeToString(audioData),
		"opts":      optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal sttTranscribe args: " + err.Error()}
	}

	result, err := s.backend.callBridgeSync("sttTranscribe", string(argsJSON))
	if err != nil {
		return "", err
	}

	var transcribeResult struct {
		Success bool   `json:"success"`
		Text    string `json:"text"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &transcribeResult); err != nil {
		return "", &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse transcribe result: " + err.Error(),
		}
	}

	if !transcribeResult.Success {
		return "", &RACError{
			Code:    ErrCodeGenerationFailed,
			Message: transcribeResult.Error,
		}
	}

	return transcribeResult.Text, nil
}

// TranscribeStream returns a stream iterator for streaming transcription.
func (s *wasmSTT) TranscribeStream(ctx context.Context, audioData []byte, opts *STTOptions) (STTStreamIterator, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	handle := s.handle
	s.mu.RUnlock()
	if handle == 0 {
		return nil, ErrNotInitialized
	}
	if len(audioData) == 0 {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "audio data is empty"}
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
		"handle":    handle,
		"audioData": base64.StdEncoding.EncodeToString(audioData),
		"opts":      optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal sttTranscribeStream args: " + err.Error()}
	}

	chunkCh, release := s.backend.callBridgeStream("sttTranscribeStream", string(argsJSON))
	it := &wasmSTTStreamIterator{
		ctx:     ctx,
		chunkCh: chunkCh,
		release: release,
		closeCh: make(chan struct{}),
	}
	return it, nil
}

// Close releases the handle.
func (s *wasmSTT) Close() error {
	s.mu.Lock()
	handle := s.handle
	if handle == 0 {
		s.mu.Unlock()
		return nil
	}
	s.handle = 0
	s.mu.Unlock()

	args := map[string]interface{}{
		"handle": handle,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal closeSTT args: " + err.Error()}
	}
	if _, err := s.backend.callBridgeSync("closeSTT", string(argsJSON)); err != nil {
		LogBridge.Warn("closeSTT error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("closeSTT", ErrCodeWASMError)
	}

	s.backend.unregisterSTT(handle)

	return nil
}

// wasmSTTStreamIterator implements STTStreamIterator for WASM.
type wasmSTTStreamIterator struct {
	mu      sync.Mutex
	ctx     context.Context
	chunkCh <-chan wasmStreamChunk
	closeCh chan struct{}
	release func()
	closed  bool
}

// Next returns the next text segment, or io.EOF when done.
func (it *wasmSTTStreamIterator) Next() (string, bool, error) {
	it.mu.Lock()
	if it.closed {
		it.mu.Unlock()
		return "", true, io.EOF
	}
	ctx := it.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	chunkCh := it.chunkCh
	closeCh := it.closeCh
	it.mu.Unlock()

	select {
	case <-closeCh:
		return "", true, io.EOF
	case <-ctx.Done():
		it.mu.Lock()
		it.closed = true
		it.mu.Unlock()
		return "", true, ctx.Err()
	case chunk, ok := <-chunkCh:
		it.mu.Lock()
		defer it.mu.Unlock()
		if !ok {
			it.closed = true
			return "", true, io.EOF
		}
		if chunk.Err != nil {
			it.closed = true
			return "", true, chunk.Err
		}
		if chunk.Done {
			it.closed = true
			return "", true, io.EOF
		}
		return chunk.Content, false, nil
	}
}

// Close releases resources and the JS callback.
func (it *wasmSTTStreamIterator) Close() error {
	it.mu.Lock()
	if it.closed {
		it.mu.Unlock()
		return nil
	}
	it.closed = true
	if it.closeCh != nil {
		close(it.closeCh)
		it.closeCh = nil
	}
	release := it.release
	it.release = nil
	it.mu.Unlock()
	if release != nil {
		release()
	}
	return nil
}

// compile-time interface check
var _ STTStreamIterator = (*wasmSTTStreamIterator)(nil)
