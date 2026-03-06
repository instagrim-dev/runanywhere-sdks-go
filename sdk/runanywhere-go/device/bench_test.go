package device

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// =============================================================================
// Helpers
// =============================================================================

// benchBase64 generates a base64-encoded string of approximately n raw bytes.
func benchBase64(n int) string {
	raw := make([]byte, n)
	for i := range raw {
		raw[i] = byte(i % 256)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// benchVec builds a float32 slice of the given dimension.
func benchVec(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(i) * 0.001
	}
	return vec
}

// benchEmbeddingsFrameJSONArray builds an embeddings frame using the legacy
// JSON float array (vector field) for comparison benchmarks.
func benchEmbeddingsFrameJSONArray(dim int) []byte {
	frame := &StreamFrame{
		Modality: ModalityEmbeddings,
		Payload:  StreamPayload{Vector: benchVec(dim)},
		Done:     true,
	}
	data, _ := json.Marshal(frame)
	return data
}

// benchEmbeddingsFrameVectorBin builds an embeddings frame using the binary
// encoding (vector_bin field) via the builder.
func benchEmbeddingsFrameVectorBin(dim int) []byte {
	frame := NewEmbeddingsStreamFrame(benchVec(dim), true, 0)
	data, _ := frame.ToJSON()
	return data
}

// =============================================================================
// ParseStreamFrame Benchmarks
// =============================================================================

func BenchmarkParseStreamFrame(b *testing.B) {
	cases := map[string][]byte{
		"llm_token":       []byte(`{"modality":"llm","payload":{"text":"Hello"},"done":false,"ts":1709000000000}`),
		"llm_done":        []byte(`{"modality":"llm","payload":{"text":""},"done":true,"ts":1709000000000}`),
		"stt":             []byte(`{"modality":"stt","payload":{"text":"transcribed text here"},"done":true,"ts":1709000000000}`),
		"tts_audio":       fmt.Appendf(nil, `{"modality":"tts","payload":{"audio":"%s"},"done":false,"ts":1709000000000}`, benchBase64(1024)),
		"emb_json_array":  benchEmbeddingsFrameJSONArray(384),
		"emb_vector_bin":  benchEmbeddingsFrameVectorBin(384),
		"error":           fmt.Appendf(nil, `{"error":{"code":"%s","message":"generation failed"},"done":true,"ts":1709000000000}`, ErrCodeGenerationFailed),
	}

	for name, data := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				_, err := ParseStreamFrame(data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// StreamFrame Builder Benchmarks
// =============================================================================

func BenchmarkStreamFrameBuilders(b *testing.B) {
	audioB64 := benchBase64(512)
	vec := benchVec(128)
	ts := int64(1709000000000)

	b.Run("LLM", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewLLMStreamFrame("Hello world", false, ts)
		}
	})
	b.Run("STT", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewSTTStreamFrame("transcribed", true, ts)
		}
	})
	b.Run("TTS", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewTTSStreamFrame(audioB64, false, ts)
		}
	})
	b.Run("Embeddings", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewEmbeddingsStreamFrame(vec, true, ts)
		}
	})
	b.Run("Error", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewErrorStreamFrame(ErrCodeGenerationFailed, "generation failed", ts)
		}
	})
}

// =============================================================================
// Marshal Round-Trip Benchmarks
// =============================================================================

// BenchmarkStreamFrameMarshalRoundTrip measures JSON marshal + ParseStreamFrame
// for embeddings frames at varying dimensions using the binary vector encoding.
func BenchmarkStreamFrameMarshalRoundTrip(b *testing.B) {
	for _, dim := range []int{4, 128, 384, 1024} {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			b.ReportAllocs()

			frame := NewEmbeddingsStreamFrame(benchVec(dim), true, 0)
			data, _ := frame.ToJSON()
			b.SetBytes(int64(len(data)))

			for b.Loop() {
				encoded, err := frame.ToJSON()
				if err != nil {
					b.Fatal(err)
				}
				_, err = ParseStreamFrame(encoded)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkEmbeddingsEncoding compares the legacy JSON float array path
// against the binary vector_bin encoding for marshal + parse round-trips.
func BenchmarkEmbeddingsEncoding(b *testing.B) {
	for _, dim := range []int{128, 384, 1024} {
		// JSON float array path
		b.Run(fmt.Sprintf("json_array/dim=%d", dim), func(b *testing.B) {
			b.ReportAllocs()
			vec := benchVec(dim)
			frame := &StreamFrame{
				Modality: ModalityEmbeddings,
				Payload:  StreamPayload{Vector: vec},
				Done:     true,
			}
			data, _ := json.Marshal(frame)
			b.SetBytes(int64(len(data)))

			for b.Loop() {
				encoded, _ := json.Marshal(frame)
				var decoded StreamFrame
				json.Unmarshal(encoded, &decoded)
			}
		})

		// Binary vector_bin path
		b.Run(fmt.Sprintf("vector_bin/dim=%d", dim), func(b *testing.B) {
			b.ReportAllocs()
			frame := NewEmbeddingsStreamFrame(benchVec(dim), true, 0)
			data, _ := frame.ToJSON()
			b.SetBytes(int64(len(data)))

			for b.Loop() {
				encoded, _ := frame.ToJSON()
				ParseStreamFrame(encoded)
			}
		})
	}
}

// =============================================================================
// Large Text Parsing Benchmark
// =============================================================================

// BenchmarkParseStreamFrameLargeText measures parsing overhead for large LLM text payloads.
func BenchmarkParseStreamFrameLargeText(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("chars=%d", size), func(b *testing.B) {
			b.ReportAllocs()

			text := strings.Repeat("x", size)
			frame := &StreamFrame{
				Modality: ModalityLLM,
				Payload:  StreamPayload{Text: text},
				Done:     true,
			}
			data, _ := json.Marshal(frame)
			b.SetBytes(int64(len(data)))

			for b.Loop() {
				_, err := ParseStreamFrame(data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Timestamp Benchmarks
// =============================================================================

// BenchmarkBuilderTimestampOverhead compares builder cost with and without
// an explicit timestamp to show the time.Now() syscall overhead.
func BenchmarkBuilderTimestampOverhead(b *testing.B) {
	b.Run("with_time.Now", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewLLMStreamFrame("token", false)
		}
	})
	b.Run("with_explicit_ts", func(b *testing.B) {
		b.ReportAllocs()
		ts := int64(1709000000000)
		for b.Loop() {
			NewLLMStreamFrame("token", false, ts)
		}
	})
}
