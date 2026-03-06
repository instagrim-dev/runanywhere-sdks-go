//go:build wasip1 && wasm

package device

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// NewSTT creates an STT handle by calling the host's stt.create operation.
func NewSTT(ctx context.Context, modelPath string, opts *STTOptions) (*STT, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasiBackendInstance.IsInitialized() {
		return nil, ErrNotInitialized
	}
	if !wasiBackendInstance.Capabilities().Has(CapabilitySTT) {
		return nil, ErrUnsupported
	}

	req, err := json.Marshal(map[string]interface{}{
		"modelPath": modelPath,
		"opts":      opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal stt.create request: " + err.Error()}
	}

	resp, err := callHost("stt.create", req)
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
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to parse stt.create response: " + err.Error()}
	}

	stt := &wasiSTT{handle: result.Handle}
	wasiBackendInstance.registerSTT(result.Handle, stt)
	return stt, nil
}

// Transcribe performs non-streaming transcription via the host.
func (s *wasiSTT) Transcribe(ctx context.Context, audioData []byte, opts *STTOptions) (string, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.handle == 0 {
		return "", ErrNotInitialized
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle":    s.handle,
		"audioData": base64.StdEncoding.EncodeToString(audioData),
		"opts":      opts,
	})
	if err != nil {
		return "", &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal stt.transcribe request: " + err.Error()}
	}

	resp, err := callHost("stt.transcribe", req)
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
		return "", &RACError{Code: ErrCodeWASMError, Message: "failed to parse stt.transcribe response: " + err.Error()}
	}

	return result.Text, nil
}

// TranscribeStream returns a stream iterator for progressive transcription.
func (s *wasiSTT) TranscribeStream(ctx context.Context, audioData []byte, opts *STTOptions) (STTStreamIterator, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	handle := s.handle
	s.mu.Unlock()
	if handle == 0 {
		return nil, ErrNotInitialized
	}
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle":    handle,
		"audioData": base64.StdEncoding.EncodeToString(audioData),
		"opts":      opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal stt.transcribe_stream request: " + err.Error()}
	}

	streamHandle, err := callHostStreamStart("stt.transcribe_stream", req)
	if err != nil {
		return nil, err
	}

	return &wasiSTTStreamIterator{
		ctx:          ctx,
		streamHandle: streamHandle,
	}, nil
}

// Close releases the STT handle.
func (s *wasiSTT) Close() error {
	s.mu.Lock()
	handle := s.handle
	if handle == 0 {
		s.mu.Unlock()
		return nil
	}
	s.handle = 0
	s.mu.Unlock()

	req, err := json.Marshal(map[string]interface{}{
		"handle": handle,
	})
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal stt.close request: " + err.Error()}
	}

	if _, err := callHost("stt.close", req); err != nil {
		LogBridge.Warn("stt.close error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("stt.close", ErrCodeWASMError)
	}

	wasiBackendInstance.unregisterSTT(handle)
	return nil
}

// wasiSTTStreamIterator reads STT stream chunks from the host.
type wasiSTTStreamIterator struct {
	mu           sync.Mutex
	ctx          context.Context
	streamHandle int64
	cancelOnce   sync.Once
	done         bool
}

func (it *wasiSTTStreamIterator) Next() (text string, isFinal bool, err error) {
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

	var frame struct {
		Text string `json:"text"`
		Done bool   `json:"done"`
	}
	if err := json.Unmarshal(chunk, &frame); err != nil {
		return string(chunk), false, nil
	}

	if frame.Done {
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return frame.Text, true, io.EOF
	}

	return frame.Text, false, nil
}

func (it *wasiSTTStreamIterator) Close() error {
	it.mu.Lock()
	if it.done {
		it.mu.Unlock()
		return nil
	}
	it.done = true
	it.mu.Unlock()
	return it.cancel()
}

func (it *wasiSTTStreamIterator) cancel() error {
	var err error
	it.cancelOnce.Do(func() {
		err = callHostStreamCancel(it.streamHandle)
	})
	return err
}
