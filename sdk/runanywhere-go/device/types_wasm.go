//go:build js && wasm

package device

// Type aliases for WASM build so the public API returns the same types as native/stub.
// Native and stub define LLM, STT, TTS, Embeddings as structs; in WASM we use the
// wasm* implementation types and alias the public names to them.

type LLM = wasmLLM
type STT = wasmSTT
type TTS = wasmTTS
type Embeddings = wasmEmbeddings
