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

// NewTTS creates a TTS handle by calling the host's tts.create operation.
func NewTTS(ctx context.Context, voicePath string, opts *TTSOptions) (*TTS, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasiBackendInstance.IsInitialized() {
		return nil, ErrNotInitialized
	}
	if !wasiBackendInstance.Capabilities().Has(CapabilityTTS) {
		return nil, ErrUnsupported
	}

	req, err := json.Marshal(map[string]interface{}{
		"voicePath": voicePath,
		"opts":      opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal tts.create request: " + err.Error()}
	}

	resp, err := callHost("tts.create", req)
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
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to parse tts.create response: " + err.Error()}
	}

	tts := &wasiTTS{handle: result.Handle}
	wasiBackendInstance.registerTTS(result.Handle, tts)
	return tts, nil
}

// Synthesize runs non-streaming TTS via the host. Returns audio bytes.
func (t *wasiTTS) Synthesize(ctx context.Context, text string, opts *TTSOptions) ([]byte, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.handle == 0 {
		return nil, ErrNotInitialized
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle": t.handle,
		"text":   text,
		"opts":   opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal tts.synthesize request: " + err.Error()}
	}

	resp, err := callHost("tts.synthesize", req)
	if err != nil {
		return nil, err
	}
	if hostErr := parseHostError(resp); hostErr != nil {
		return nil, hostErr
	}

	var result struct {
		AudioData string `json:"audioData"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to parse tts.synthesize response: " + err.Error()}
	}

	audioBytes, err := base64.StdEncoding.DecodeString(result.AudioData)
	if err != nil {
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to decode audio data: " + err.Error()}
	}

	return audioBytes, nil
}

// SynthesizeStream returns a stream iterator for progressive synthesis.
func (t *wasiTTS) SynthesizeStream(ctx context.Context, text string, opts *TTSOptions) (TTSStreamIterator, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	t.mu.Lock()
	handle := t.handle
	t.mu.Unlock()
	if handle == 0 {
		return nil, ErrNotInitialized
	}
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle": handle,
		"text":   text,
		"opts":   opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal tts.synthesize_stream request: " + err.Error()}
	}

	streamHandle, err := callHostStreamStart("tts.synthesize_stream", req)
	if err != nil {
		return nil, err
	}

	return &wasiTTSStreamIterator{
		ctx:          ctx,
		streamHandle: streamHandle,
	}, nil
}

// Close releases the TTS handle.
func (t *wasiTTS) Close() error {
	t.mu.Lock()
	handle := t.handle
	if handle == 0 {
		t.mu.Unlock()
		return nil
	}
	t.handle = 0
	t.mu.Unlock()

	req, err := json.Marshal(map[string]interface{}{
		"handle": handle,
	})
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal tts.close request: " + err.Error()}
	}

	if _, err := callHost("tts.close", req); err != nil {
		LogBridge.Warn("tts.close error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("tts.close", ErrCodeWASMError)
	}

	wasiBackendInstance.unregisterTTS(handle)
	return nil
}

// wasiTTSStreamIterator reads TTS stream chunks from the host.
type wasiTTSStreamIterator struct {
	mu           sync.Mutex
	ctx          context.Context
	streamHandle int64
	cancelOnce   sync.Once
	done         bool
}

func (it *wasiTTSStreamIterator) Next() (chunk []byte, isFinal bool, err error) {
	it.mu.Lock()
	if it.done {
		it.mu.Unlock()
		return nil, true, io.EOF
	}
	ctx := it.ctx
	it.mu.Unlock()

	select {
	case <-ctx.Done():
		_ = it.cancel()
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return nil, true, ctx.Err()
	default:
	}

	data, done, err := callHostStreamNext(it.streamHandle)
	if err != nil {
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return nil, true, err
	}
	if done {
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return nil, true, io.EOF
	}

	// Parse chunk — may contain base64 audio or raw bytes
	var frame struct {
		AudioData string `json:"audioData"`
		Done      bool   `json:"done"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return data, false, nil // fallback: treat as raw audio
	}

	if frame.Done {
		it.mu.Lock()
		it.done = true
		it.mu.Unlock()
		return nil, true, io.EOF
	}

	audioBytes, err := base64.StdEncoding.DecodeString(frame.AudioData)
	if err != nil {
		return data, false, nil // fallback
	}

	return audioBytes, false, nil
}

func (it *wasiTTSStreamIterator) Close() error {
	it.mu.Lock()
	if it.done {
		it.mu.Unlock()
		return nil
	}
	it.done = true
	it.mu.Unlock()
	return it.cancel()
}

func (it *wasiTTSStreamIterator) cancel() error {
	var err error
	it.cancelOnce.Do(func() {
		err = callHostStreamCancel(it.streamHandle)
	})
	return err
}
