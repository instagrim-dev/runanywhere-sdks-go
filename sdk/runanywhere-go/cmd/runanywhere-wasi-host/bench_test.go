package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

// =============================================================================
// Lifecycle Benchmarks
// =============================================================================

// BenchmarkHandleLifecycleLLM measures a full create → generate → close round-trip for LLM.
func BenchmarkHandleLifecycleLLM(b *testing.B) {
	b.ReportAllocs()
	h := newHostHandler("")

	createReq := []byte(`{"modelPath":"/models/test.gguf"}`)

	for b.Loop() {
		resp := h.handleCall("llm.create", createReq)
		var cr struct {
			Handle int64 `json:"handle"`
		}
		json.Unmarshal(resp, &cr)

		genReq, _ := json.Marshal(map[string]any{"handle": cr.Handle, "prompt": "Hello world"})
		h.handleCall("llm.generate", genReq)

		closeReq, _ := json.Marshal(map[string]any{"handle": cr.Handle})
		h.handleCall("llm.close", closeReq)
	}
}

// BenchmarkHandleLifecycleEmb measures a full create → embed → close round-trip for Embeddings.
func BenchmarkHandleLifecycleEmb(b *testing.B) {
	b.ReportAllocs()
	h := newHostHandler("")

	createReq := []byte(`{"modelPath":"/models/embed"}`)

	for b.Loop() {
		resp := h.handleCall("emb.create", createReq)
		var cr struct {
			Handle int64 `json:"handle"`
		}
		json.Unmarshal(resp, &cr)

		embedReq, _ := json.Marshal(map[string]any{"handle": cr.Handle, "text": "Hello"})
		h.handleCall("emb.embed", embedReq)

		closeReq, _ := json.Marshal(map[string]any{"handle": cr.Handle})
		h.handleCall("emb.close", closeReq)
	}
}

// =============================================================================
// Batch Embedding JSON Scaling
// =============================================================================

// BenchmarkEmbedBatchJSON measures JSON ser/deser scaling for batch embeddings.
func BenchmarkEmbedBatchJSON(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("texts=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			h := newHostHandler("")

			// Create embeddings handle
			resp := h.handleCall("emb.create", []byte(`{"modelPath":"/models/embed"}`))
			var cr struct {
				Handle int64 `json:"handle"`
			}
			json.Unmarshal(resp, &cr)

			// Build batch request
			texts := make([]string, n)
			for i := range texts {
				texts[i] = fmt.Sprintf("text number %d for embedding benchmark", i)
			}
			batchReq, _ := json.Marshal(map[string]any{
				"handle": cr.Handle,
				"texts":  texts,
			})
			b.SetBytes(int64(len(batchReq)))

			for b.Loop() {
				h.handleCall("emb.embed_batch", batchReq)
			}

			closeReq, _ := json.Marshal(map[string]any{"handle": cr.Handle})
			h.handleCall("emb.close", closeReq)
		})
	}
}

// =============================================================================
// Stream Iteration
// =============================================================================

// BenchmarkStreamIteration measures chunk-by-chunk iteration of a mock LLM stream.
func BenchmarkStreamIteration(b *testing.B) {
	b.ReportAllocs()
	h := newHostHandler("")

	streamReq := []byte(`{"handle":1,"prompt":"benchmark stream iteration test"}`)

	for b.Loop() {
		stream := h.handleStreamStart("llm.generate_stream", streamReq)
		if stream == nil {
			b.Fatal("expected non-nil stream")
		}

		for {
			stream.mu.Lock()
			if stream.index >= len(stream.chunks) {
				stream.mu.Unlock()
				break
			}
			_ = stream.chunks[stream.index]
			stream.index++
			stream.mu.Unlock()
		}
	}
}

// =============================================================================
// Concurrent Handle Operations
// =============================================================================

// BenchmarkConcurrentHandleOps exercises sync.Map + atomic.Int64 under contention.
func BenchmarkConcurrentHandleOps(b *testing.B) {
	b.ReportAllocs()
	h := newHostHandler("")

	createReq := []byte(`{"modelPath":"/models/test.gguf"}`)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp := h.handleCall("llm.create", createReq)
			var cr struct {
				Handle int64 `json:"handle"`
			}
			json.Unmarshal(resp, &cr)

			genReq, _ := json.Marshal(map[string]any{"handle": cr.Handle, "prompt": "concurrent"})
			h.handleCall("llm.generate", genReq)

			closeReq, _ := json.Marshal(map[string]any{"handle": cr.Handle})
			h.handleCall("llm.close", closeReq)
		}
	})
}

// =============================================================================
// Dispatch Overhead
// =============================================================================

// BenchmarkDispatchOverhead measures the bare handleCall for a no-op (capabilities)
// to establish the dispatch floor.
func BenchmarkDispatchOverhead(b *testing.B) {
	b.ReportAllocs()
	h := newHostHandler("")

	req := []byte("{}")

	for b.Loop() {
		h.handleCall("capabilities", req)
	}
}
