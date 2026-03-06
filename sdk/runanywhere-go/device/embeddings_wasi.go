//go:build wasip1 && wasm

package device

import (
	"context"
	"encoding/json"
	"fmt"
)

// NewEmbeddings creates an Embeddings handle by calling the host's emb.create operation.
func NewEmbeddings(ctx context.Context, modelPath string, opts *EmbedOptions) (*Embeddings, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasiBackendInstance.IsInitialized() {
		return nil, ErrNotInitialized
	}
	if !wasiBackendInstance.Capabilities().Has(CapabilityEmbeddings) {
		return nil, ErrUnsupported
	}

	req, err := json.Marshal(map[string]interface{}{
		"modelPath": modelPath,
		"opts":      opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal emb.create request: " + err.Error()}
	}

	resp, err := callHost("emb.create", req)
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
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to parse emb.create response: " + err.Error()}
	}

	emb := &wasiEmbeddings{handle: result.Handle}
	wasiBackendInstance.registerEmbeddings(result.Handle, emb)
	return emb, nil
}

// Embed returns the embedding for a single text.
func (e *wasiEmbeddings) Embed(ctx context.Context, text string, opts *EmbedOptions) ([]float32, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.handle == 0 {
		return nil, ErrNotInitialized
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle": e.handle,
		"text":   text,
		"opts":   opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal emb.embed request: " + err.Error()}
	}

	resp, err := callHost("emb.embed", req)
	if err != nil {
		return nil, err
	}
	if hostErr := parseHostError(resp); hostErr != nil {
		return nil, hostErr
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to parse emb.embed response: " + err.Error()}
	}

	return result.Embedding, nil
}

// EmbedBatch returns embeddings for multiple texts.
func (e *wasiEmbeddings) EmbedBatch(ctx context.Context, texts []string, opts *EmbedOptions) (*EmbedBatchResult, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.handle == 0 {
		return nil, ErrNotInitialized
	}

	req, err := json.Marshal(map[string]interface{}{
		"handle": e.handle,
		"texts":  texts,
		"opts":   opts,
	})
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal emb.embed_batch request: " + err.Error()}
	}

	resp, err := callHost("emb.embed_batch", req)
	if err != nil {
		return nil, err
	}
	if hostErr := parseHostError(resp); hostErr != nil {
		return nil, hostErr
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
		Dimension  int         `json:"dimension"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, &RACError{Code: ErrCodeWASMError, Message: "failed to parse emb.embed_batch response: " + err.Error()}
	}

	dim := result.Dimension
	if dim == 0 && len(result.Embeddings) > 0 {
		dim = len(result.Embeddings[0])
	}

	return &EmbedBatchResult{
		Embeddings: result.Embeddings,
		Dimension:  dim,
	}, nil
}

// Close releases the Embeddings handle.
func (e *wasiEmbeddings) Close() error {
	e.mu.Lock()
	handle := e.handle
	if handle == 0 {
		e.mu.Unlock()
		return nil
	}
	e.handle = 0
	e.mu.Unlock()

	req, err := json.Marshal(map[string]interface{}{
		"handle": handle,
	})
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal emb.close request: " + err.Error()}
	}

	if _, err := callHost("emb.close", req); err != nil {
		LogBridge.Warn("emb.close error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("emb.close", ErrCodeWASMError)
	}

	wasiBackendInstance.unregisterEmbeddings(handle)
	return nil
}
