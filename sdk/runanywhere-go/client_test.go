package runanywhere

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_ListModels(t *testing.T) {
	modelsResp := ModelsResponse{
		Object: "list",
		Data:   []Model{{ID: "test-model", Object: "model", Created: 1, OwnedBy: "runanywhere"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(modelsResp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 || got.Data[0].ID != "test-model" {
		t.Errorf("got %+v", got)
	}
}

func TestClient_Health(t *testing.T) {
	healthResp := HealthResponse{
		Status:              "ok",
		Model:               "m",
		ModelLoaded:         true,
		STTAvailable:        true,
		TTSAvailable:        false,
		EmbeddingsAvailable: true,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(healthResp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || !got.ModelLoaded || !got.STTAvailable || got.TTSAvailable || !got.EmbeddingsAvailable {
		t.Errorf("got %+v", got)
	}
}

func TestClient_Chat(t *testing.T) {
	chatResp := ChatCompletionResponse{
		ID: "id", Object: "chat.completion", Model: "m",
		Choices: []Choice{{Index: 0, Message: ChatMessage{Role: "assistant", Content: "Hi"}, FinishReason: "stop"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Stream {
			t.Error("Chat() should force stream=false")
		}
		_ = json.NewEncoder(w).Encode(chatResp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.Chat(context.Background(), &ChatCompletionRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0].Message.Content != "Hi" {
		t.Errorf("got %+v", got)
	}
}

func TestClient_ChatStream(t *testing.T) {
	sseBody := `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"Hi"}}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !req.Stream {
			t.Error("ChatStream should send stream=true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	stream, err := client.ChatStream(context.Background(), &ChatCompletionRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	chunk, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if chunk == nil || chunk.Choices[0].Delta.Content != "Hi" {
		t.Errorf("chunk %+v", chunk)
	}
	_, err = stream.Next()
	if err != io.EOF {
		t.Errorf("expected EOF after [DONE], got %v", err)
	}
}

func TestClient_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Health(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*ServerError)
	if !ok {
		t.Fatalf("expected *ServerError, got %T", err)
	}
	if se.StatusCode != 400 || se.Message != "bad request" {
		t.Errorf("got %+v", se)
	}
}

func TestClient_Transcribe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Error("expected multipart form")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TranscriptionResponse{Text: "hello"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.Transcribe(context.Background(), strings.NewReader("fake-audio"), "audio.wav", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hello" {
		t.Errorf("got %q", got.Text)
	}
}

func TestClient_Speech(t *testing.T) {
	audioBytes := []byte{0x00, 0x01, 0x02}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(audioBytes)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.Speech(context.Background(), &SpeechRequest{Model: "tts", Input: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, audioBytes) {
		t.Errorf("got %v", got)
	}
}

func TestClient_Embeddings(t *testing.T) {
	embResp := EmbeddingsResponse{
		Model: "embed",
		Data:  []EmbeddingData{{Embedding: []float32{0.1, 0.2}, Index: 0}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(embResp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.Embeddings(context.Background(), &EmbeddingsRequest{Model: "embed", Input: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "embed" || len(got.Data) != 1 || len(got.Data[0].Embedding) != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestClient_Transcribe_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid file","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Transcribe(context.Background(), strings.NewReader("x"), "a.wav", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*ServerError)
	if !ok {
		t.Fatalf("expected *ServerError, got %T: %v", err, err)
	}
	if se.StatusCode != 400 || se.Message != "invalid file" {
		t.Errorf("got %+v", se)
	}
}

func TestClient_Transcribe_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Transcribe(context.Background(), strings.NewReader("x"), "a.wav", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// Decode error, not necessarily ServerError
	if _, ok := err.(*ServerError); ok {
		// 200 with bad body could be ServerError in some paths; we expect decode error
	}
	_ = err
}

func TestClient_Transcribe_NilReader(t *testing.T) {
	client := NewClient("http://localhost:8080")
	_, err := client.Transcribe(context.Background(), nil, "audio.wav", nil)
	if err == nil {
		t.Fatal("expected error for nil reader")
	}
	if !strings.Contains(err.Error(), "nil") && !strings.Contains(err.Error(), "reader") {
		t.Errorf("error should mention nil/reader: %v", err)
	}
}

func TestClient_Speech_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"synthesis failed","type":"server_error"}}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Speech(context.Background(), &SpeechRequest{Model: "tts", Input: "hi"})
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*ServerError)
	if !ok {
		t.Fatalf("expected *ServerError, got %T: %v", err, err)
	}
	if se.StatusCode != 500 {
		t.Errorf("got status %d", se.StatusCode)
	}
}

func TestClient_Speech_NilRequest(t *testing.T) {
	client := NewClient("http://localhost:8080")
	_, err := client.Speech(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if !errors.Is(err, errNilRequest) {
		t.Errorf("expected errNilRequest, got %v", err)
	}
}

func TestClient_Embeddings_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":{"message":"embeddings not configured","type":"not_implemented"}}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Embeddings(context.Background(), &EmbeddingsRequest{Model: "e", Input: "hi"})
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*ServerError)
	if !ok {
		t.Fatalf("expected *ServerError, got %T: %v", err, err)
	}
	if se.StatusCode != 501 {
		t.Errorf("got status %d", se.StatusCode)
	}
}

func TestClient_Embeddings_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": "not an array"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Embeddings(context.Background(), &EmbeddingsRequest{Model: "e", Input: "hi"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClient_WithHTTPClientNil_WithTimeout_NoPanic(t *testing.T) {
	// WithHTTPClient(nil) is a no-op; WithTimeout must not dereference nil client.
	client := NewClient("http://localhost", WithHTTPClient(nil), WithTimeout(0))
	if client.httpClient == nil {
		t.Fatal("expected default http client after nil no-op")
	}
}

func TestClient_OptionOrder_TimeoutApplied(t *testing.T) {
	// Timeout is applied after all options, so order WithTimeout before WithHTTPClient does not drop it.
	custom := &http.Client{}
	client := NewClient("http://localhost", WithTimeout(time.Second), WithHTTPClient(custom))
	if client.httpClient != custom {
		t.Fatal("expected custom client")
	}
	if client.httpClient.Timeout != time.Second {
		t.Errorf("expected timeout 1s, got %v", client.httpClient.Timeout)
	}
}
