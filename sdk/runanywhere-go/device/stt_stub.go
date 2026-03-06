//go:build !cgo && !js && !wasip1

package device

import "context"

// STT is an on-device speech-to-text handle. Stub implementation.
type STT struct{}

// NewSTT creates an STT handle. Stub returns (nil, ErrUnsupported).
func NewSTT(ctx context.Context, modelPath string, opts *STTOptions) (*STT, error) {
	return nil, ErrUnsupported
}

// Transcribe runs non-streaming transcription. Stub returns ErrUnsupported.
func (s *STT) Transcribe(ctx context.Context, audioData []byte, opts *STTOptions) (string, error) {
	return "", ErrUnsupported
}

// TranscribeStream returns a stream iterator. Stub returns (nil, ErrUnsupported).
func (s *STT) TranscribeStream(ctx context.Context, audioData []byte, opts *STTOptions) (STTStreamIterator, error) {
	return nil, ErrUnsupported
}

// Close releases the handle. Stub is a no-op (idempotent).
func (s *STT) Close() error {
	return nil
}
