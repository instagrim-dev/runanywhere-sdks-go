package device

import "strings"

// =============================================================================
// Capability Flags
// =============================================================================

// Capability represents a single capability that a backend may support.
type Capability uint32

const (
	// CapabilityLLM indicates the backend supports LLM text generation.
	CapabilityLLM Capability = 1 << iota

	// CapabilitySTT indicates the backend supports speech-to-text.
	CapabilitySTT

	// CapabilityTTS indicates the backend supports text-to-speech.
	CapabilityTTS

	// CapabilityEmbeddings indicates the backend supports embeddings.
	CapabilityEmbeddings

	// CapabilityStreaming indicates the backend supports streaming responses.
	CapabilityStreaming

	// CapabilityGPU indicates the backend has GPU acceleration available.
	CapabilityGPU

	// CapabilityWebGPU indicates the backend has WebGPU acceleration (browser).
	CapabilityWebGPU
)

// CapabilitySet is a bitmask set of capabilities. It is a value type and safe
// to copy, compare, and use as a map key or const.
type CapabilitySet uint32

// Has reports whether the set contains the given capability.
func (c CapabilitySet) Has(cap Capability) bool {
	return c&CapabilitySet(cap) != 0
}

// HasAny reports whether the set contains any of the given capabilities.
func (c CapabilitySet) HasAny(caps ...Capability) bool {
	for _, cap := range caps {
		if c&CapabilitySet(cap) != 0 {
			return true
		}
	}
	return false
}

// HasAll reports whether the set contains all of the given capabilities.
func (c CapabilitySet) HasAll(caps ...Capability) bool {
	for _, cap := range caps {
		if c&CapabilitySet(cap) == 0 {
			return false
		}
	}
	return true
}

// With returns a new CapabilitySet with the given capabilities added.
func (c CapabilitySet) With(caps ...Capability) CapabilitySet {
	for _, cap := range caps {
		c |= CapabilitySet(cap)
	}
	return c
}

// Without returns a new CapabilitySet with the given capabilities removed.
func (c CapabilitySet) Without(caps ...Capability) CapabilitySet {
	for _, cap := range caps {
		c &^= CapabilitySet(cap)
	}
	return c
}

// Merge returns a new CapabilitySet that is the union of c and other.
func (c CapabilitySet) Merge(other CapabilitySet) CapabilitySet {
	return c | other
}

// String returns a human-readable string representation of the capability set.
func (c CapabilitySet) String() string {
	if c == 0 {
		return "none"
	}
	var caps []string
	if c.Has(CapabilityLLM) {
		caps = append(caps, "llm")
	}
	if c.Has(CapabilitySTT) {
		caps = append(caps, "stt")
	}
	if c.Has(CapabilityTTS) {
		caps = append(caps, "tts")
	}
	if c.Has(CapabilityEmbeddings) {
		caps = append(caps, "embeddings")
	}
	if c.Has(CapabilityStreaming) {
		caps = append(caps, "streaming")
	}
	if c.Has(CapabilityGPU) {
		caps = append(caps, "gpu")
	}
	if c.Has(CapabilityWebGPU) {
		caps = append(caps, "webgpu")
	}
	return strings.Join(caps, ",")
}

// =============================================================================
// Capability Metadata
// =============================================================================

// CapabilityMetadata provides additional information about a capability.
type CapabilityMetadata struct {
	// MaxContextLength is the maximum context length in tokens (for LLM).
	MaxContextLength int `json:"max_context_length,omitempty"`

	// MaxGeneratedLength is the maximum generated tokens (for LLM).
	MaxGeneratedLength int `json:"max_generated_length,omitempty"`

	// SupportedModelFormats lists the supported model formats.
	SupportedModelFormats []string `json:"supported_model_formats,omitempty"`

	// StreamingSupported indicates if streaming is supported.
	StreamingSupported bool `json:"streaming_supported,omitempty"`

	// GPUDeviceName is the name of the GPU device (if available).
	GPUDeviceName string `json:"gpu_device_name,omitempty"`

	// MemoryLimit is the memory limit in bytes.
	MemoryLimit uint64 `json:"memory_limit,omitempty"`
}

// BackendCapabilities holds all capability information for a backend.
//
// NOTE: This type is intentionally kept as a stable forward-compatible surface.
// Even if some call paths do not consume it yet, do not delete it as dead code
// unless a replacement shape is introduced in the same change.
type BackendCapabilities struct {
	// Capabilities is the set of available capabilities.
	Capabilities CapabilitySet `json:"capabilities"`

	// Metadata contains additional information about each capability.
	// Note: JSON marshaling uses numeric keys because Capability is uint32.
	Metadata map[Capability]CapabilityMetadata `json:"metadata,omitempty"`
}

// HasCapability reports whether the backend has the given capability.
func (b *BackendCapabilities) HasCapability(cap Capability) bool {
	return b.Capabilities.Has(cap)
}

// GetMetadata returns the metadata for a capability, or nil if not present.
func (b *BackendCapabilities) GetMetadata(cap Capability) *CapabilityMetadata {
	if b.Metadata == nil {
		return nil
	}
	m, ok := b.Metadata[cap]
	if !ok {
		return nil
	}
	return &m
}

// =============================================================================
// Capability Parsing
// =============================================================================

// ParseCapabilityStrings converts string capability names to a CapabilitySet.
// This is the shared parser used by all backends (WASM browser, WASI, etc.).
func ParseCapabilityStrings(caps []string) CapabilitySet {
	var result CapabilitySet
	for _, c := range caps {
		switch c {
		case "llm":
			result = result.With(CapabilityLLM)
		case "stt":
			result = result.With(CapabilitySTT)
		case "tts":
			result = result.With(CapabilityTTS)
		case "embeddings":
			result = result.With(CapabilityEmbeddings)
		case "streaming":
			result = result.With(CapabilityStreaming)
		case "gpu":
			result = result.With(CapabilityGPU)
		case "webgpu":
			result = result.With(CapabilityWebGPU)
		}
	}
	return result
}

// =============================================================================
// Common Capability Sets
// =============================================================================

const (
	// CapabilitySetNone is an empty capability set.
	CapabilitySetNone CapabilitySet = 0

	// CapabilitySetLLM is the capability set for LLM-only backends.
	CapabilitySetLLM = CapabilitySet(CapabilityLLM)

	// CapabilitySetSTT is the capability set for STT-only backends.
	CapabilitySetSTT = CapabilitySet(CapabilitySTT)

	// CapabilitySetTTS is the capability set for TTS-only backends.
	CapabilitySetTTS = CapabilitySet(CapabilityTTS)

	// CapabilitySetEmbeddings is the capability set for embeddings-only backends.
	CapabilitySetEmbeddings = CapabilitySet(CapabilityEmbeddings)

	// CapabilitySetAll is the capability set for full-featured backends.
	CapabilitySetAll = CapabilitySet(CapabilityLLM | CapabilitySTT | CapabilityTTS | CapabilityEmbeddings | CapabilityStreaming)
)
