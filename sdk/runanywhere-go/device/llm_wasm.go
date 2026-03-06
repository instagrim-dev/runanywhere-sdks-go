//go:build js && wasm

package device

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// =============================================================================
// WASM LLM Implementation
// =============================================================================

// wasmLLM is the LLM handle for browser WASM.
// Concurrency: Generate uses an RW lock so multiple reads can proceed; actual
// execution remains serialized by the single-threaded JS bridge/event loop.
type wasmLLM struct {
	mu      sync.RWMutex
	handle  int64
	model   string
	backend *wasmBrowserBackend
}

// NewLLM creates a new LLM handle for the given model path.
func NewLLM(ctx context.Context, modelPath string, opts *LLMOptions) (*LLM, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasmBackend.IsInitialized() {
		return nil, ErrNotInitialized
	}

	// Check capability
	caps := wasmBackend.Capabilities()
	if !caps.Has(CapabilityLLM) {
		return nil, ErrUnsupported
	}

	// Call bridge to create LLM
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
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal createLLM args: " + err.Error()}
	}

	result, err := wasmBackend.callBridgeSync("createLLM", string(argsJSON))
	if err != nil {
		return nil, err
	}

	// Parse result to get handle
	var createResult struct {
		Success bool   `json:"success"`
		Handle  int64  `json:"handle"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &createResult); err != nil {
		return nil, &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse createLLM result: " + err.Error(),
		}
	}

	if !createResult.Success {
		return nil, &RACError{
			Code:    ErrCodeModelLoadFailed,
			Message: createResult.Error,
		}
	}

	llm := &wasmLLM{
		handle:  createResult.Handle,
		model:   modelPath,
		backend: wasmBackend,
	}

	// Register the handle using the bridge's ID
	wasmBackend.registerLLM(llm.handle, llm)

	return llm, nil
}

// Generate runs non-streaming generation.
func (l *wasmLLM) Generate(ctx context.Context, prompt string, opts *LLMOptions) (string, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return "", err
	}
	l.mu.RLock()
	handle := l.handle
	l.mu.RUnlock()

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
		"handle": handle,
		"prompt": prompt,
		"opts":   optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal llmGenerate args: " + err.Error()}
	}

	result, err := l.backend.callBridgeSync("llmGenerate", string(argsJSON))
	if err != nil {
		return "", err
	}

	// Parse result
	var genResult struct {
		Success bool   `json:"success"`
		Text    string `json:"text"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &genResult); err != nil {
		return "", &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse generate result: " + err.Error(),
		}
	}

	if !genResult.Success {
		return "", &RACError{
			Code:    ErrCodeGenerationFailed,
			Message: genResult.Error,
		}
	}

	return genResult.Text, nil
}

// GenerateStream returns a stream iterator for token-by-token generation.
func (l *wasmLLM) GenerateStream(ctx context.Context, prompt string, opts *LLMOptions) (LLMStreamIterator, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	l.mu.RLock()
	handle := l.handle
	l.mu.RUnlock()

	if handle == 0 {
		return nil, ErrNotInitialized
	}

	optsJSON := "{}"
	if opts != nil {
		data, err := json.Marshal(opts)
		if err != nil {
			return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal options: " + err.Error()}
		}
		optsJSON = string(data)
	}

	args := map[string]interface{}{
		"handle": handle,
		"prompt": prompt,
		"opts":   optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal llmGenerateStream args: " + err.Error()}
	}

	chunkCh, release := l.backend.callBridgeStream("llmGenerateStream", string(argsJSON))
	it := &wasmLLMStreamIterator{
		ctx:     ctx,
		chunkCh: chunkCh,
		release: release,
		closeCh: make(chan struct{}),
	}
	return it, nil
}

// Close releases the LLM handle.
func (l *wasmLLM) Close() error {
	l.mu.Lock()
	handle := l.handle
	if handle == 0 {
		l.mu.Unlock()
		return nil
	}
	l.handle = 0
	l.mu.Unlock()

	args := map[string]interface{}{
		"handle": handle,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal closeLLM args: " + err.Error()}
	}
	if _, err := l.backend.callBridgeSync("closeLLM", string(argsJSON)); err != nil {
		LogBridge.Warn("closeLLM error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("closeLLM", ErrCodeWASMError)
	}

	l.backend.unregisterLLM(handle)

	return nil
}

// =============================================================================
// Stream Iterator for WASM
// =============================================================================

// wasmLLMStreamIterator implements LLMStreamIterator for WASM.
type wasmLLMStreamIterator struct {
	mu      sync.Mutex
	ctx     context.Context
	chunkCh <-chan wasmStreamChunk
	closeCh chan struct{}
	release func()
	closed  bool
}

// Next returns the next token, or io.EOF when done.
func (it *wasmLLMStreamIterator) Next() (string, bool, error) {
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
func (it *wasmLLMStreamIterator) Close() error {
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
var _ LLMStreamIterator = (*wasmLLMStreamIterator)(nil)
