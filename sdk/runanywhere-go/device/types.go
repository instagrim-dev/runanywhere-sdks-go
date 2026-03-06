package device

import "time"

// Config holds options for device initialization. Pass to InitWithConfig.
// Zero value registers only the LlamaCPP backend (LLM). Set RegisterONNX
// to true to also register the ONNX backend (STT/TTS/Embeddings).
type Config struct {
	// LogLevel controls when the platform adapter forwards log messages from
	// the C layer. 0 means default (INFO). Higher values = less verbose
	// (e.g. DEBUG=1, INFO=2, WARNING=3, ERROR=4). Only messages with level >= LogLevel are forwarded.
	LogLevel int

	// LogTag is an application-specific tag included in log output.
	LogTag string

	// RegisterONNX, if true, registers the ONNX backend (STT/TTS). If false,
	// only the LlamaCPP backend is registered (LLM only). Init() calls InitWithConfig(ctx, nil),
	// so with nil config only LlamaCPP is registered; pass a config with RegisterONNX: true
	// to enable STT/TTS when librac_backend_onnx is linked.
	RegisterONNX bool

	// BridgeTimeout is the maximum time to wait for a synchronous WASM bridge call.
	// Zero means the default (30s). Only used by the browser WASM backend.
	BridgeTimeout time.Duration
}

// LLMOptions holds options for LLM generation (Generate and GenerateStream).
type LLMOptions struct {
	MaxTokens     int
	Temperature   float32
	TopP          float32
	SystemPrompt  string
	StopSequences []string

	// ModelSource optionally provides model data from a custom source (e.g. RemoteModel, Base64Model).
	// When set, NewLLM may use it instead of or in addition to modelPath. Backend-specific;
	// WASM bridge may accept URL or base64 via modelPath; native may resolve to a temp path.
	ModelSource ModelSource
}

// STTOptions holds options for speech-to-text. Zero value uses backend defaults.
type STTOptions struct {
	Language        string
	EnableTimestamps bool
}

// TTSOptions holds options for text-to-speech. Zero value uses backend defaults.
type TTSOptions struct {
	Voice string
	Rate  float32
}

// EmbedOptions holds options for embeddings. Zero value uses backend defaults.
type EmbedOptions struct {
	Normalize int32 // -1 = use config default
	Pooling   int32 // -1 = use config default
	NThreads  int32 // 0 = auto
}

// EmbedBatchResult is the structured return type for EmbedBatch.
// Embeddings[i] is the embedding for the i-th input text; each row has length Dimension.
type EmbedBatchResult struct {
	Embeddings [][]float32
	Dimension  int
}

// LLMStreamIterator is the iterator returned by GenerateStream. Call Next()
// until isFinal is true or err is non-nil. Implementations must implement io.Closer;
// call Close() when done to release resources and cancel the C stream if supported.
type LLMStreamIterator interface {
	Next() (token string, isFinal bool, err error)
	// Close releases resources and cancels the stream if still active. Idempotent.
	Close() error
}

// STTStreamIterator is the iterator returned by TranscribeStream.
type STTStreamIterator interface {
	Next() (text string, isFinal bool, err error)
	Close() error
}

// TTSStreamIterator is the iterator returned by SynthesizeStream.
type TTSStreamIterator interface {
	Next() (chunk []byte, isFinal bool, err error)
	Close() error
}
