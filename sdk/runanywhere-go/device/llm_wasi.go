//go:build wasip1 && wasm

package device

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// NewLLM creates an LLM handle by calling the host's llm.create operation.
func NewLLM(ctx context.Context, modelPath string, opts *LLMOptions) (*LLM, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasiBackendInstance.IsInitialized() {
		return nil, ErrNotInitialized
	}
	if !wasiBackendInstance.Capabilities().Has(CapabilityLLM) {
		return nil, ErrUnsupported
	}

	req, err := json.Marshal(map[string]interface{}{
		"modelPath": modelPath,
		"opts":      opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal llm.create request: " + err.Error()}
	}

	resp, err := callHost("llm.create", req)
	if err != nil {
		return nil, err
	}
	if hostErr := parseHostError(resp); hostErr != nil {
		return nil, hostErr
	}

	var result struct {
		Handle int64 `json:"handle"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to parse llm.create response: " + err.Error()}
	}

	llm := &wasiLLM{handle: result.Handle}
	wasiBackendInstance.registerLLM(result.Handle, llm)
	return llm, nil
}

// Generate runs non-streaming generation via the host.
func (l *wasiLLM) Generate(ctx context.Context, prompt string, opts *LLMOptions) (string, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.handle == 0 {
		return "", ErrNotInitialized
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle": l.handle,
		"prompt": prompt,
		"opts":   opts,
	})
	if err != nil {
		return "", &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal llm.generate request: " + err.Error()}
	}

	resp, err := callHost("llm.generate", req)
	if err != nil {
		return "", err
	}
	if hostErr := parseHostError(resp); hostErr != nil {
		return "", hostErr
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", &RACError{Code: ErrCodeWASMError, Message: "failed to parse llm.generate response: " + err.Error()}
	}

	return result.Text, nil
}

// GenerateStream returns a stream iterator for token-by-token generation.
func (l *wasiLLM) GenerateStream(ctx context.Context, prompt string, opts *LLMOptions) (LLMStreamIterator, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	l.mu.Lock()
	handle := l.handle
	l.mu.Unlock()
	if handle == 0 {
		return nil, ErrNotInitialized
	}
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle": handle,
		"prompt": prompt,
		"opts":   opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal llm.generate_stream request: " + err.Error()}
	}

	streamHandle, err := callHostStreamStart("llm.generate_stream", req)
	if err != nil {
		return nil, err
	}

	return &wasiLLMStreamIterator{
		ctx:          ctx,
		streamHandle: streamHandle,
	}, nil
}

// Close releases the LLM handle.
func (l *wasiLLM) Close() error {
	l.mu.Lock()
	handle := l.handle
	if handle == 0 {
		l.mu.Unlock()
		return nil
	}
	l.handle = 0
	l.mu.Unlock()

	req, err := json.Marshal(map[string]interface{}{
		"handle": handle,
	})
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal llm.close request: " + err.Error()}
	}

	if _, err := callHost("llm.close", req); err != nil {
		LogBridge.Warn("llm.close error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("llm.close", ErrCodeWASMError)
	}

	wasiBackendInstance.unregisterLLM(handle)
	return nil
}

// wasiLLMStreamIterator reads LLM stream chunks from the host.
type wasiLLMStreamIterator struct {
	mu           sync.Mutex
	ctx          context.Context
	streamHandle int64
	cancelOnce   sync.Once
	done         bool
}

func (it *wasiLLMStreamIterator) Next() (token string, isFinal bool, err error) {
	it.mu.Lock()
	if it.done {
		it.mu.Unlock()
		return "", true, io.EOF
	}
	ctx := it.ctx
	it.mu.Unlock()

	select {
	case <-ctx.Done():
		_ = it.cancel()
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return "", true, ctx.Err()
	default:
	}

	chunk, done, err := callHostStreamNext(it.streamHandle)
	if err != nil {
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return "", true, err
	}
	if done {
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return "", true, io.EOF
	}

	// Parse chunk JSON
	var frame struct {
		Text string `json:"text"`
		Done bool   `json:"done"`
	}
	if err := json.Unmarshal(chunk, &frame); err != nil {
		return string(chunk), false, nil // fallback: treat as raw text
	}

	if frame.Done {
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return frame.Text, true, io.EOF
	}

	return frame.Text, false, nil
}

func (it *wasiLLMStreamIterator) Close() error {
	it.mu.Lock()
	if it.done {
		it.mu.Unlock()
		return nil
	}
	it.done = true
	it.mu.Unlock()
	return it.cancel()
}

func (it *wasiLLMStreamIterator) cancel() error {
	var err error
	it.cancelOnce.Do(func() {
		err = callHostStreamCancel(it.streamHandle)
	})
	return err
}
