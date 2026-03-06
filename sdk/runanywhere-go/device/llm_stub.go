//go:build !cgo && !js && !wasip1

package device

import "context"

// LLM is an on-device LLM handle. Stub implementation; all methods return ErrUnsupported.
type LLM struct{}

// NewLLM creates an LLM handle for the given model path. Stub returns (nil, ErrUnsupported).
func NewLLM(ctx context.Context, modelPath string, opts *LLMOptions) (*LLM, error) {
	return nil, ErrUnsupported
}

// Generate runs non-streaming generation. Stub returns ErrUnsupported.
func (l *LLM) Generate(ctx context.Context, prompt string, opts *LLMOptions) (string, error) {
	return "", ErrUnsupported
}

// GenerateStream returns a stream iterator for token-by-token generation. Stub returns (nil, ErrUnsupported).
func (l *LLM) GenerateStream(ctx context.Context, prompt string, opts *LLMOptions) (LLMStreamIterator, error) {
	return nil, ErrUnsupported
}

// Close releases the handle. Stub is a no-op (idempotent).
func (l *LLM) Close() error {
	return nil
}
