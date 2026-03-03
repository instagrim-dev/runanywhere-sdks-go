//go:build !cgo

package device

import "context"

// TTS is an on-device text-to-speech handle. Stub implementation.
type TTS struct{}

// NewTTS creates a TTS handle. Stub returns (nil, ErrUnsupported).
func NewTTS(ctx context.Context, voicePath string, opts *TTSOptions) (*TTS, error) {
	return nil, ErrUnsupported
}

// Synthesize runs non-streaming synthesis. Stub returns ErrUnsupported.
func (t *TTS) Synthesize(ctx context.Context, text string, opts *TTSOptions) ([]byte, error) {
	return nil, ErrUnsupported
}

// SynthesizeStream returns a stream iterator. Stub returns (nil, ErrUnsupported).
func (t *TTS) SynthesizeStream(ctx context.Context, text string, opts *TTSOptions) (TTSStreamIterator, error) {
	return nil, ErrUnsupported
}

// Close releases the handle. Stub is a no-op (idempotent).
func (t *TTS) Close() error {
	return nil
}
