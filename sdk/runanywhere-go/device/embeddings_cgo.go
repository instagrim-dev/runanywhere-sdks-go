//go:build cgo

package device

/*
#cgo CFLAGS: -I${SRCDIR}/../../runanywhere-commons/include
#cgo LDFLAGS: -lrac_commons -lrac_backend_llamacpp -lrac_backend_onnx

#include <stdlib.h>
#include "rac/features/embeddings/rac_embeddings_service.h"
#include "rac/features/embeddings/rac_embeddings_types.h"
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"
)

// Embeddings is an on-device embeddings handle (CGO implementation).
type Embeddings struct {
	handle    C.rac_handle_t
	closeOnce sync.Once
}

// NewEmbeddings creates an Embeddings handle for the given model path. Call device.Init() first.
// For ONNX-based embeddings, RegisterONNX must be true. opts is reserved for future use.
func NewEmbeddings(ctx context.Context, modelPath string, opts *EmbedOptions) (*Embeddings, error) {
	if !isInitialized() {
		return nil, ErrNotInitialized
	}
	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))
	var outHandle C.rac_handle_t
	res := C.rac_embeddings_create(cPath, &outHandle)
	if res != C.RAC_SUCCESS {
		return nil, fmt.Errorf("%w", newCGOError("rac_embeddings_create", int(res)))
	}
	// Some backends require initialize with path after create; try it and ignore if unsupported.
	if C.rac_embeddings_initialize(outHandle, cPath) != C.RAC_SUCCESS {
		// Proceed with handle; backend may have loaded from create(model_path)
	}
	incrementHandleCount()
	return &Embeddings{handle: outHandle}, nil
}

// Embed returns the embedding vector for a single text.
func (e *Embeddings) Embed(ctx context.Context, text string, opts *EmbedOptions) ([]float32, error) {
	if e.handle == nil {
		return nil, fmt.Errorf("Embeddings handle closed")
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	var cOpts *C.rac_embeddings_options_t
	if opts != nil {
		cOpts = &C.rac_embeddings_options_t{
			normalize: C.int32_t(opts.Normalize),
			pooling:   C.int32_t(opts.Pooling),
			n_threads: C.int32_t(opts.NThreads),
		}
	}
	var result C.rac_embeddings_result_t
	res := C.rac_embeddings_embed(e.handle, cText, cOpts, &result)
	if res != C.RAC_SUCCESS {
		return nil, fmt.Errorf("%w", newCGOError("rac_embeddings_embed", int(res)))
	}
	defer C.rac_embeddings_result_free(&result)
	if result.num_embeddings == 0 || result.embeddings == nil {
		return nil, nil
	}
	vec := (*C.rac_embedding_vector_t)(unsafe.Pointer(result.embeddings))
	if vec.data == nil || vec.dimension == 0 {
		return nil, nil
	}
	dim := int(vec.dimension)
	cSlice := unsafe.Slice(vec.data, dim)
	out := make([]float32, dim)
	for i := 0; i < dim; i++ {
		out[i] = float32(cSlice[i])
	}
	return out, nil
}

// EmbedBatch returns embeddings for multiple texts. Embeddings[i] corresponds to texts[i]; each row has length Dimension.
func (e *Embeddings) EmbedBatch(ctx context.Context, texts []string, opts *EmbedOptions) (*EmbedBatchResult, error) {
	if e.handle == nil {
		return nil, fmt.Errorf("Embeddings handle closed")
	}
	if len(texts) == 0 {
		return &EmbedBatchResult{Embeddings: nil, Dimension: 0}, nil
	}
	cStrings := make([]*C.char, len(texts))
	for i, s := range texts {
		cStrings[i] = C.CString(s)
	}
	defer func() {
		for _, c := range cStrings {
			C.free(unsafe.Pointer(c))
		}
	}()
	var cOpts *C.rac_embeddings_options_t
	if opts != nil {
		cOpts = &C.rac_embeddings_options_t{
			normalize:  C.int32_t(opts.Normalize),
			pooling:    C.int32_t(opts.Pooling),
			n_threads: C.int32_t(opts.NThreads),
		}
	}
	var result C.rac_embeddings_result_t
	res := C.rac_embeddings_embed_batch(e.handle, &cStrings[0], C.size_t(len(texts)), cOpts, &result)
	if res != C.RAC_SUCCESS {
		return nil, fmt.Errorf("%w", newCGOError("rac_embeddings_embed_batch", int(res)))
	}
	defer C.rac_embeddings_result_free(&result)
	dim := int(result.dimension)
	out := &EmbedBatchResult{
		Embeddings: make([][]float32, result.num_embeddings),
		Dimension:  dim,
	}
	// result.embeddings is rac_embedding_vector_t* (array of structs)
	vecs := unsafe.Slice(result.embeddings, int(result.num_embeddings))
	for i := range vecs {
		vec := &vecs[i]
		if vec.data == nil {
			out.Embeddings[i] = nil
			continue
		}
		cData := unsafe.Slice(vec.data, dim)
		row := make([]float32, dim)
		for j := 0; j < dim; j++ {
			row[j] = float32(cData[j])
		}
		out.Embeddings[i] = row
	}
	return out, nil
}

// Close releases the handle. Idempotent and safe for concurrent use.
func (e *Embeddings) Close() error {
	e.closeOnce.Do(func() {
		if e.handle == nil {
			return
		}
		C.rac_embeddings_destroy(e.handle)
		e.handle = nil
		decrementHandleCount()
	})
	return nil
}
