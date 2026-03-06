package device

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"time"
)

// =============================================================================
// Stream Modality
// =============================================================================

// Modality represents the type of streaming operation.
type Modality string

const (
	// ModalityLLM represents LLM text generation.
	ModalityLLM Modality = "llm"

	// ModalitySTT represents speech-to-text transcription.
	ModalitySTT Modality = "stt"

	// ModalityTTS represents text-to-speech synthesis.
	ModalityTTS Modality = "tts"

	// ModalityEmbeddings represents embeddings computation.
	ModalityEmbeddings Modality = "embeddings"
)

// =============================================================================
// Stream Frame
// =============================================================================

// StreamFrame is the unified JSON format for all streaming modalities.
// This is the wire format used between Go and JS/WASM.
type StreamFrame struct {
	// Modality indicates the type of operation (llm, stt, tts, embeddings).
	Modality Modality `json:"modality"`

	// Payload contains the modality-specific data.
	Payload StreamPayload `json:"payload"`

	// Done indicates whether this is the final frame.
	Done bool `json:"done"`

	// Error contains error information if an error occurred.
	Error *StreamError `json:"error,omitempty"`

	// Sequence is the frame sequence number for ordering.
	Sequence int64 `json:"seq,omitempty"`

	// Timestamp is the Unix timestamp in milliseconds.
	Timestamp int64 `json:"ts,omitempty"`

	// Metadata contains additional context.
	Metadata map[string]any `json:"meta,omitempty"`
}

// StreamPayload contains modality-specific data.
//
// For embeddings, Vector is the canonical Go-side representation. VectorBin
// is the fast wire encoding (base64 little-endian float32). When both are set,
// MarshalJSON emits only vector_bin to avoid doubling the payload.
// ParseStreamFrame decodes vector_bin back to Vector transparently.
type StreamPayload struct {
	// Text contains text data (for LLM, STT).
	Text string `json:"text,omitempty"`

	// Audio contains base64-encoded audio data (for TTS).
	Audio string `json:"audio,omitempty"`

	// Vector contains embedding vector data (for Embeddings).
	// When parsing, this is populated from either the JSON array or VectorBin.
	Vector []float32 `json:"-"`

	// VectorBin contains base64-encoded little-endian float32 vector data.
	// This is 3-4x faster to marshal/unmarshal than the JSON float array.
	// Producers should prefer this field; consumers should read Vector
	// which is populated automatically by ParseStreamFrame.
	VectorBin string `json:"-"`

	// Tokens contains token data for LLM streaming.
	Tokens []string `json:"tokens,omitempty"`
}

// streamPayloadWire is the JSON wire representation. It mirrors StreamPayload
// but emits either vector OR vector_bin, never both.
type streamPayloadWire struct {
	Text      string    `json:"text,omitempty"`
	Audio     string    `json:"audio,omitempty"`
	Vector    []float32 `json:"vector,omitempty"`
	VectorBin string    `json:"vector_bin,omitempty"`
	Tokens    []string  `json:"tokens,omitempty"`
}

// MarshalJSON emits vector_bin when set, otherwise vector. Never both.
func (p StreamPayload) MarshalJSON() ([]byte, error) {
	w := streamPayloadWire{
		Text:   p.Text,
		Audio:  p.Audio,
		Tokens: p.Tokens,
	}
	if p.VectorBin != "" {
		w.VectorBin = p.VectorBin
	} else {
		w.Vector = p.Vector
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads both vector and vector_bin from the wire.
func (p *StreamPayload) UnmarshalJSON(data []byte) error {
	var w streamPayloadWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	p.Text = w.Text
	p.Audio = w.Audio
	p.Vector = w.Vector
	p.VectorBin = w.VectorBin
	p.Tokens = w.Tokens
	return nil
}

// StreamError contains error information.
type StreamError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// =============================================================================
// Stream Frame Parsing
// =============================================================================

// ParseStreamFrame parses a JSON stream frame from bytes.
// If the payload contains a VectorBin field, it is decoded into Vector.
func ParseStreamFrame(data []byte) (*StreamFrame, error) {
	var frame StreamFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return nil, &RACError{
			Code:    ErrCodeInvalidParam,
			Message: "failed to parse stream frame: " + err.Error(),
		}
	}
	if frame.Payload.VectorBin != "" {
		if len(frame.Payload.Vector) == 0 {
			vec, err := DecodeVectorBin(frame.Payload.VectorBin)
			if err != nil {
				return nil, &RACError{
					Code:    ErrCodeInvalidParam,
					Message: "failed to decode vector_bin: " + err.Error(),
				}
			}
			frame.Payload.Vector = vec
		}
		frame.Payload.VectorBin = "" // always clear to avoid redundant output on re-marshal
	}
	return &frame, nil
}

// ParseStreamFrameFromString parses a JSON stream frame from a string.
func ParseStreamFrameFromString(s string) (*StreamFrame, error) {
	return ParseStreamFrame([]byte(s))
}

// =============================================================================
// Stream Frame Builders
// =============================================================================

// NewLLMStreamFrame creates a new stream frame for LLM output.
// An optional timestamp (Unix millis) can be provided to avoid repeated
// time.Now() calls in tight loops; if omitted, the current time is used.
func NewLLMStreamFrame(text string, done bool, ts ...int64) *StreamFrame {
	return &StreamFrame{
		Modality: ModalityLLM,
		Payload: StreamPayload{
			Text: text,
		},
		Done:      done,
		Timestamp: pickTimestamp(ts),
	}
}

// NewSTTStreamFrame creates a new stream frame for STT output.
// An optional timestamp (Unix millis) can be provided; see NewLLMStreamFrame.
func NewSTTStreamFrame(text string, done bool, ts ...int64) *StreamFrame {
	return &StreamFrame{
		Modality: ModalitySTT,
		Payload: StreamPayload{
			Text: text,
		},
		Done:      done,
		Timestamp: pickTimestamp(ts),
	}
}

// NewTTSStreamFrame creates a new stream frame for TTS output.
// An optional timestamp (Unix millis) can be provided; see NewLLMStreamFrame.
func NewTTSStreamFrame(audioBase64 string, done bool, ts ...int64) *StreamFrame {
	return &StreamFrame{
		Modality: ModalityTTS,
		Payload: StreamPayload{
			Audio: audioBase64,
		},
		Done:      done,
		Timestamp: pickTimestamp(ts),
	}
}

// NewEmbeddingsStreamFrame creates a new stream frame for embeddings output.
// The vector is encoded as base64 little-endian float32 (VectorBin) for fast
// serialization. ParseStreamFrame decodes VectorBin back to Vector transparently.
// An optional timestamp (Unix millis) can be provided; see NewLLMStreamFrame.
func NewEmbeddingsStreamFrame(vector []float32, done bool, ts ...int64) *StreamFrame {
	return &StreamFrame{
		Modality: ModalityEmbeddings,
		Payload: StreamPayload{
			Vector:    vector,
			VectorBin: EncodeVectorBin(vector),
		},
		Done:      done,
		Timestamp: pickTimestamp(ts),
	}
}

// NewErrorStreamFrame creates a new stream frame for errors.
// An optional timestamp (Unix millis) can be provided; see NewLLMStreamFrame.
func NewErrorStreamFrame(code ErrorCode, message string, ts ...int64) *StreamFrame {
	return &StreamFrame{
		Error: &StreamError{
			Code:    code,
			Message: message,
		},
		Done:      true,
		Timestamp: pickTimestamp(ts),
	}
}

// =============================================================================
// Vector Binary Encoding
// =============================================================================

// EncodeVectorBin encodes a float32 slice as base64 little-endian bytes.
// This is 3-4x faster than JSON float array encoding for large vectors.
func EncodeVectorBin(vec []float32) string {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// DecodeVectorBin decodes a base64 little-endian float32 vector.
func DecodeVectorBin(s string) ([]float32, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(raw)%4 != 0 {
		return nil, &RACError{
			Code:    ErrCodeInvalidParam,
			Message: "vector_bin length is not a multiple of 4 bytes",
		}
	}
	vec := make([]float32, len(raw)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return vec, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// pickTimestamp returns the first element of ts if provided, otherwise now.
func pickTimestamp(ts []int64) int64 {
	if len(ts) > 0 {
		return ts[0]
	}
	return time.Now().UnixMilli()
}

// ToJSON converts the stream frame to JSON bytes.
func (f *StreamFrame) ToJSON() ([]byte, error) {
	return json.Marshal(f)
}

// ToJSONString converts the stream frame to a JSON string.
func (f *StreamFrame) ToJSONString() (string, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
