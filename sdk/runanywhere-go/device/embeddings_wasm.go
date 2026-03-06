//go:build js && wasm

package device

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// =============================================================================
// WASM Embeddings Implementation
// =============================================================================

// wasmEmbeddings is the Embeddings handle for browser WASM.
type wasmEmbeddings struct {
	mu      sync.RWMutex
	handle  int64
	backend *wasmBrowserBackend
}

// NewEmbeddings creates a new Embeddings handle for the given model path.
func NewEmbeddings(ctx context.Context, modelPath string, opts *EmbedOptions) (*Embeddings, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	if !wasmBackend.IsInitialized() {
		return nil, ErrNotInitialized
	}

	// Check capability
	caps := wasmBackend.Capabilities()
	if !caps.Has(CapabilityEmbeddings) {
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
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal createEmbeddings args: " + err.Error()}
	}

	result, err := wasmBackend.callBridgeSync("createEmbeddings", string(argsJSON))
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
			Message: "failed to parse createEmbeddings result: " + err.Error(),
		}
	}

	if !createResult.Success {
		return nil, &RACError{
			Code:    ErrCodeModelLoadFailed,
			Message: createResult.Error,
		}
	}

	emb := &wasmEmbeddings{
		handle:  createResult.Handle,
		backend: wasmBackend,
	}

	// Register the handle using the bridge's ID
	wasmBackend.registerEmbeddings(emb.handle, emb)

	return emb, nil
}

// Embed computes the embedding for a single text.
func (e *wasmEmbeddings) Embed(ctx context.Context, text string, opts *EmbedOptions) ([]float32, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	embedding, err := e.embedSingle(ctx, text, opts)
	if err != nil {
		return nil, err
	}
	if len(embedding) == 0 {
		return nil, &RACError{
			Code:    ErrCodeGenerationFailed,
			Message: "embed returned no results",
		}
	}
	return embedding, nil
}

// EmbedBatch computes embeddings for multiple texts.
func (e *wasmEmbeddings) EmbedBatch(ctx context.Context, texts []string, opts *EmbedOptions) (*EmbedBatchResult, error) {
	if err := checkContextNotDone(ctx); err != nil {
		return nil, err
	}
	e.mu.RLock()
	handle := e.handle
	e.mu.RUnlock()
	if handle == 0 {
		return nil, ErrNotInitialized
	}
	if len(texts) == 0 {
		return &EmbedBatchResult{Embeddings: nil, Dimension: 0}, nil
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

	// Preferred fast path: single bridge round-trip for the whole batch.
	args := map[string]interface{}{
		"handle": handle,
		"texts":  texts,
		"opts":   optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal embeddingsEmbedBatch args: " + err.Error()}
	}
	if result, err := e.backend.callBridgeSync("embeddingsEmbedBatch", string(argsJSON)); err == nil {
		var batchResult struct {
			Success    bool        `json:"success"`
			Embeddings [][]float32 `json:"embeddings"`
			Dimension  int         `json:"dimension"`
			Error      string      `json:"error"`
		}
		if err := json.Unmarshal([]byte(result), &batchResult); err == nil && batchResult.Success {
			dim := batchResult.Dimension
			if dim == 0 && len(batchResult.Embeddings) > 0 {
				dim = len(batchResult.Embeddings[0])
			}
			return &EmbedBatchResult{
				Embeddings: batchResult.Embeddings,
				Dimension:  dim,
			}, nil
		} else if err == nil && !batchResult.Success && batchResult.Error != "" {
			return nil, &RACError{
				Code:    ErrCodeGenerationFailed,
				Message: batchResult.Error,
			}
		}
	}

	// Backward-compatible fallback for older bridges without batch support.
	embeddings := make([][]float32, len(texts))
	dimension := 0
	for i, text := range texts {
		embedding, err := e.embedSingle(ctx, text, opts)
		if err != nil {
			return nil, err
		}
		if len(embedding) == 0 {
			return nil, &RACError{
				Code:    ErrCodeGenerationFailed,
				Message: "embed returned no results",
			}
		}
		if dimension == 0 {
			dimension = len(embedding)
		}
		embeddings[i] = embedding
	}

	return &EmbedBatchResult{
		Embeddings: embeddings,
		Dimension:  dimension,
	}, nil
}

func (e *wasmEmbeddings) embedSingle(ctx context.Context, text string, opts *EmbedOptions) ([]float32, error) {
	e.mu.RLock()
	handle := e.handle
	e.mu.RUnlock()
	if handle == 0 {
		return nil, ErrNotInitialized
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

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	args := map[string]interface{}{
		"handle": handle,
		"text":   text,
		"opts":   optsJSON,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal embeddingsEmbed args: " + err.Error()}
	}

	result, err := e.backend.callBridgeSync("embeddingsEmbed", string(argsJSON))
	if err != nil {
		return nil, err
	}

	var embedResult struct {
		Success   bool      `json:"success"`
		Embedding []float32 `json:"embedding"`
		Error     string    `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &embedResult); err != nil {
		return nil, &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse embed result: " + err.Error(),
		}
	}
	if !embedResult.Success {
		return nil, &RACError{
			Code:    ErrCodeGenerationFailed,
			Message: embedResult.Error,
		}
	}
	return embedResult.Embedding, nil
}

// Close releases the handle.
func (e *wasmEmbeddings) Close() error {
	e.mu.Lock()
	handle := e.handle
	if handle == 0 {
		e.mu.Unlock()
		return nil
	}
	e.handle = 0
	e.mu.Unlock()

	args := map[string]interface{}{
		"handle": handle,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return &RACError{Code: ErrCodeInvalidParam, Message: "failed to marshal closeEmbeddings args: " + err.Error()}
	}
	if _, err := e.backend.callBridgeSync("closeEmbeddings", string(argsJSON)); err != nil {
		LogBridge.Warn("closeEmbeddings error", map[string]string{"handle": fmt.Sprint(handle), "error": err.Error()})
		RecordBridgeError("closeEmbeddings", ErrCodeWASMError)
	}

	e.backend.unregisterEmbeddings(handle)

	return nil
}
