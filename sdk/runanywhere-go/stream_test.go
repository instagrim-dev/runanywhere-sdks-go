package runanywhere

import (
	"io"
	"strings"
	"testing"
)

func TestChatStreamReader_Next(t *testing.T) {
	sse := `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"Hello"}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":" world"}}]}

data: [DONE]

`
	r := NewChatStreamReader(io.NopCloser(strings.NewReader(sse)))
	defer r.Close()

	chunk1, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if chunk1 == nil || len(chunk1.Choices) == 0 || chunk1.Choices[0].Delta.Content != "Hello" {
		t.Errorf("first chunk: got %+v", chunk1)
	}

	chunk2, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if chunk2 == nil || chunk2.Choices[0].Delta.Content != " world" {
		t.Errorf("second chunk: got %+v", chunk2)
	}

	chunk3, err := r.Next()
	if err != io.EOF {
		t.Errorf("expected EOF after [DONE], got %v %+v", err, chunk3)
	}
}

func TestChatStreamReader_EmptyThenDone(t *testing.T) {
	sse := "data: [DONE]\n\n"
	r := NewChatStreamReader(io.NopCloser(strings.NewReader(sse)))
	defer r.Close()
	chunk, err := r.Next()
	if err != io.EOF || chunk != nil {
		t.Errorf("expected EOF and nil chunk, got err=%v chunk=%+v", err, chunk)
	}
}

func TestChatStreamReader_Close(t *testing.T) {
	r := NewChatStreamReader(io.NopCloser(strings.NewReader("data: [DONE]\n\n")))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := r.Next()
	if err != io.EOF {
		t.Errorf("after Close expected EOF, got %v", err)
	}
}
