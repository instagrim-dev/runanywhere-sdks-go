//go:build wasip1 && wasm

package device

import "sync"

// wasiLLM is the WASI LLM handle. Methods call the host via callHost.
// Concurrency: Generate uses an exclusive lock to serialize per-handle calls
// because host-side thread-safety guarantees are backend-dependent.
type wasiLLM struct {
	mu     sync.Mutex
	handle int64
}

// wasiSTT is the WASI STT handle.
type wasiSTT struct {
	mu     sync.Mutex
	handle int64
}

// wasiTTS is the WASI TTS handle.
type wasiTTS struct {
	mu     sync.Mutex
	handle int64
}

// wasiEmbeddings is the WASI embeddings handle.
type wasiEmbeddings struct {
	mu     sync.Mutex
	handle int64
}

// Public type aliases.
type LLM = wasiLLM
type STT = wasiSTT
type TTS = wasiTTS
type Embeddings = wasiEmbeddings
