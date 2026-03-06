//go:build !cgo && !js && !wasip1

package device

import "context"

// Embeddings is an on-device embeddings handle. Stub implementation.
type Embeddings struct{}

// NewEmbeddings creates an Embeddings handle. Stub returns (nil, ErrUnsupported).
func NewEmbeddings(ctx context.Context, modelPath string, opts *EmbedOptions) (*Embeddings, error) {
	return nil, ErrUnsupported
}

// Embed returns the embedding for a single text. Stub returns ErrUnsupported.
func (e *Embeddings) Embed(ctx context.Context, text string, opts *EmbedOptions) ([]float32, error) {
	return nil, ErrUnsupported
}

// EmbedBatch returns embeddings for multiple texts. Stub returns ErrUnsupported.
func (e *Embeddings) EmbedBatch(ctx context.Context, texts []string, opts *EmbedOptions) (*EmbedBatchResult, error) {
	return nil, ErrUnsupported
}

// Close releases the handle. Stub is a no-op (idempotent).
func (e *Embeddings) Close() error {
	return nil
}
