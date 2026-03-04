package runanywhere

import "encoding/json"

// Models

// Model represents a single model in the list (OpenAI Model object).
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse is the response from GET /v1/models.
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// Chat

// ChatMessage is a single message in a conversation (OpenAI ChatCompletionRequestMessage).
type ChatMessage struct {
	Role       string                `json:"role"`
	Content    string                `json:"content,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
	Name       string                `json:"name,omitempty"`
	ToolCalls  []ChatMessageToolCall `json:"tool_calls,omitempty"`
}

// ChatMessageToolCall is a tool call in an assistant message.
type ChatMessageToolCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function ChatMessageToolCallFunc `json:"function"`
}

// ChatMessageToolCallFunc is the function part of a tool call.
type ChatMessageToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition is a tool (function) the model may call (OpenAI ChatCompletionTool).
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition describes a callable function.
type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"` // JSON Schema object
}

// ChatCompletionRequest is the request body for POST /v1/chat/completions.
type ChatCompletionRequest struct {
	Model            string           `json:"model"`
	Messages         []ChatMessage    `json:"messages"`
	Stream           bool             `json:"stream,omitempty"`
	Temperature      float32          `json:"temperature,omitempty"`
	TopP             float32          `json:"top_p,omitempty"`
	MaxTokens        int              `json:"max_tokens,omitempty"`
	Stop             []string         `json:"stop,omitempty"`
	PresencePenalty  float32          `json:"presence_penalty,omitempty"`
	FrequencyPenalty float32          `json:"frequency_penalty,omitempty"`
	Tools            []ToolDefinition `json:"tools,omitempty"`
	ToolChoice       any              `json:"tool_choice,omitempty"` // "none" | "auto" | "required" | { "type": "function", "function": { "name": "..." } }
	User             string           `json:"user,omitempty"`
}

// ChatCompletionResponse is the non-streaming response from POST /v1/chat/completions.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice is a single choice in a chat completion response.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage is token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Streaming

// StreamChunk is a single SSE chunk (chat.completion.chunk).
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// StreamChoice is a choice in a streaming chunk.
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason,omitempty"`
}

// StreamDelta is the delta content in a streaming chunk.
type StreamDelta struct {
	Role      string                `json:"role,omitempty"`
	Content   string                `json:"content,omitempty"`
	ToolCalls []ChatMessageToolCall `json:"tool_calls,omitempty"`
}

// Health

// HealthResponse is the response from GET /health.
type HealthResponse struct {
	Status              string `json:"status"`
	Model               string `json:"model"`
	ModelLoaded         bool   `json:"model_loaded"`
	STTAvailable        bool   `json:"stt_available"`
	TTSAvailable        bool   `json:"tts_available"`
	EmbeddingsAvailable bool   `json:"embeddings_available"`
}

// Errors

// errorCodeFlex unmarshals server "code" as either JSON number (e.g. 400) or string.
type errorCodeFlex string

func (e *errorCodeFlex) UnmarshalJSON(data []byte) error {
	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*e = errorCodeFlex(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*e = errorCodeFlex(n.String())
	return nil
}

// ErrorResponse is the body of 4xx/5xx responses (OpenAI-style).
type ErrorResponse struct {
	Error struct {
		Message string        `json:"message"`
		Type    string        `json:"type"`
		Code    errorCodeFlex `json:"code,omitempty"`
	} `json:"error"`
}

// =============================================================================
// V2: Transcriptions, Speech, Embeddings
// =============================================================================

// TranscriptionResponse is the response from POST /v1/audio/transcriptions.
type TranscriptionResponse struct {
	Text string `json:"text"`
}

// TranscribeOptions are optional parameters for Transcribe.
type TranscribeOptions struct {
	Model          string `json:"model,omitempty"`
	Language       string `json:"language,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

// SpeechRequest is the request body for POST /v1/audio/speech.
type SpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice,omitempty"`
	Speed          float32 `json:"speed,omitempty"`
	ResponseFormat string  `json:"response_format,omitempty"`
}

// EmbeddingsRequest is the request body for POST /v1/embeddings.
type EmbeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"` // string or []string
}

// EmbeddingData is a single embedding in the response.
type EmbeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index,omitempty"`
	Object    string    `json:"object,omitempty"`
}

// EmbeddingsResponse is the response from POST /v1/embeddings.
type EmbeddingsResponse struct {
	Object string           `json:"object,omitempty"`
	Data   []EmbeddingData  `json:"data"`
	Model  string           `json:"model"`
	Usage  *EmbeddingsUsage `json:"usage,omitempty"`
}

// EmbeddingsUsage is token usage for embeddings (optional).
type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
