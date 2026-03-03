package runanywhere

import (
	"encoding/json"
	"testing"
)

func TestModelsResponseRoundTrip(t *testing.T) {
	in := &ModelsResponse{
		Object: "list",
		Data: []Model{
			{ID: "my-model", Object: "model", Created: 12345, OwnedBy: "runanywhere"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ModelsResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Object != in.Object || len(out.Data) != len(in.Data) || out.Data[0].ID != in.Data[0].ID {
		t.Errorf("round-trip: got %+v", out)
	}
}

func TestChatCompletionRequestRoundTrip(t *testing.T) {
	in := &ChatCompletionRequest{
		Model: "m",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
		Stream:      false,
		Temperature: 0.7,
		MaxTokens:   100,
		Tools: []ToolDefinition{{
			Type: "function",
			Function: FunctionDefinition{Name: "f", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
		}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ChatCompletionRequest
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != in.Model || len(out.Messages) != 1 || out.Messages[0].Content != "Hi" || len(out.Tools) != 1 {
		t.Errorf("round-trip: got %+v", out)
	}
}

func TestChatCompletionResponseRoundTrip(t *testing.T) {
	in := &ChatCompletionResponse{
		ID: "id", Object: "chat.completion", Created: 1, Model: "m",
		Choices: []Choice{{
			Index: 0,
			Message: ChatMessage{
				Role: "assistant", Content: "Hello",
				ToolCalls: []ChatMessageToolCall{{
					ID: "call_1", Type: "function",
					Function: ChatMessageToolCallFunc{Name: "f", Arguments: "{}"},
				}},
			},
			FinishReason: "stop",
		}},
		Usage: &Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ChatCompletionResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Choices[0].Message.Content != "Hello" || len(out.Choices[0].Message.ToolCalls) != 1 {
		t.Errorf("round-trip: got %+v", out)
	}
}

func TestHealthResponseRoundTrip(t *testing.T) {
	in := &HealthResponse{
		Status:              "ok",
		Model:               "m",
		ModelLoaded:         true,
		STTAvailable:        true,
		TTSAvailable:        false,
		EmbeddingsAvailable: true,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out HealthResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != in.Status || out.ModelLoaded != in.ModelLoaded ||
		out.STTAvailable != in.STTAvailable || out.TTSAvailable != in.TTSAvailable || out.EmbeddingsAvailable != in.EmbeddingsAvailable {
		t.Errorf("round-trip: got %+v", out)
	}
}

func TestStreamChunkRoundTrip(t *testing.T) {
	reason := "stop"
	in := &StreamChunk{
		ID: "id", Object: "chat.completion.chunk", Created: 1, Model: "m",
		Choices: []StreamChoice{{
			Index:        0,
			Delta:        StreamDelta{Content: "hi"},
			FinishReason: &reason,
		}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out StreamChunk
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Choices) != 1 || out.Choices[0].Delta.Content != "hi" || (out.Choices[0].FinishReason == nil || *out.Choices[0].FinishReason != "stop") {
		t.Errorf("round-trip: got %+v", out)
	}
}

func TestTranscriptionResponseRoundTrip(t *testing.T) {
	in := &TranscriptionResponse{Text: "hello world"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out TranscriptionResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Text != in.Text {
		t.Errorf("round-trip: got %+v", out)
	}
}

func TestEmbeddingsResponseRoundTrip(t *testing.T) {
	in := &EmbeddingsResponse{
		Model: "embed-model",
		Data:  []EmbeddingData{{Embedding: []float32{0.1, 0.2}, Index: 0}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out EmbeddingsResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != in.Model || len(out.Data) != 1 || len(out.Data[0].Embedding) != 2 {
		t.Errorf("round-trip: got %+v", out)
	}
}
