package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go/device"
)

func TestHandleCallInit(t *testing.T) {
	h := newHostHandler("")
	resp := h.handleCall("init", []byte("{}"))

	var result struct {
		Success      bool     `json:"success"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if len(result.Capabilities) == 0 {
		t.Error("expected non-empty capabilities")
	}
}

func TestHandleCallCapabilities(t *testing.T) {
	h := newHostHandler("")
	resp := h.handleCall("capabilities", []byte("{}"))

	var result struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]bool{"llm": true, "stt": true, "tts": true, "embeddings": true, "streaming": true}
	for _, cap := range result.Capabilities {
		delete(want, cap)
	}
	if len(want) > 0 {
		t.Errorf("missing capabilities: %v", want)
	}
}

func TestHandleCallLLMLifecycle(t *testing.T) {
	h := newHostHandler("")

	// Create
	resp := h.handleCall("llm.create", []byte(`{"modelPath":"/models/test.gguf"}`))
	var createResult struct {
		Handle int64 `json:"handle"`
	}
	if err := json.Unmarshal(resp, &createResult); err != nil {
		t.Fatalf("create unmarshal: %v", err)
	}
	if createResult.Handle <= 0 {
		t.Fatalf("expected positive handle, got %d", createResult.Handle)
	}

	// Generate
	req, _ := json.Marshal(map[string]interface{}{
		"handle": createResult.Handle,
		"prompt": "Hello world",
	})
	resp = h.handleCall("llm.generate", req)
	var genResult struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(resp, &genResult); err != nil {
		t.Fatalf("generate unmarshal: %v", err)
	}
	if genResult.Text == "" {
		t.Error("expected non-empty generated text")
	}

	// Close
	closeReq, _ := json.Marshal(map[string]interface{}{"handle": createResult.Handle})
	resp = h.handleCall("llm.close", closeReq)
	var closeResult struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(resp, &closeResult); err != nil {
		t.Fatalf("close unmarshal: %v", err)
	}
	if !closeResult.Success {
		t.Error("expected success=true on close")
	}
}

func TestHandleCallSTTTranscribe(t *testing.T) {
	h := newHostHandler("")

	// Create
	resp := h.handleCall("stt.create", []byte(`{"modelPath":"/models/whisper.bin"}`))
	var createResult struct {
		Handle int64 `json:"handle"`
	}
	json.Unmarshal(resp, &createResult)

	// Transcribe
	req, _ := json.Marshal(map[string]interface{}{
		"handle":    createResult.Handle,
		"audioData": "SGVsbG8=", // base64("Hello")
	})
	resp = h.handleCall("stt.transcribe", req)
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty transcription")
	}

	// Close
	h.handleCall("stt.close", []byte(`{"handle":`+itoa(createResult.Handle)+`}`))
}

func TestHandleCallTTSSynthesize(t *testing.T) {
	h := newHostHandler("")

	resp := h.handleCall("tts.create", []byte(`{"voicePath":"/voices/default"}`))
	var createResult struct {
		Handle int64 `json:"handle"`
	}
	json.Unmarshal(resp, &createResult)

	req, _ := json.Marshal(map[string]interface{}{
		"handle": createResult.Handle,
		"text":   "Hello world",
	})
	resp = h.handleCall("tts.synthesize", req)
	var result struct {
		AudioData string `json:"audioData"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.AudioData == "" {
		t.Error("expected non-empty audio data")
	}

	h.handleCall("tts.close", []byte(`{"handle":`+itoa(createResult.Handle)+`}`))
}

func TestHandleCallEmbeddings(t *testing.T) {
	h := newHostHandler("")

	resp := h.handleCall("emb.create", []byte(`{"modelPath":"/models/embed"}`))
	var createResult struct {
		Handle int64 `json:"handle"`
	}
	json.Unmarshal(resp, &createResult)

	// Single embed
	req, _ := json.Marshal(map[string]interface{}{
		"handle": createResult.Handle,
		"text":   "Hello",
	})
	resp = h.handleCall("emb.embed", req)
	var embedResult struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(resp, &embedResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(embedResult.Embedding) == 0 {
		t.Error("expected non-empty embedding")
	}

	// Batch embed
	batchReq, _ := json.Marshal(map[string]interface{}{
		"handle": createResult.Handle,
		"texts":  []string{"Hello", "World"},
	})
	resp = h.handleCall("emb.embed_batch", batchReq)
	var batchResult struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(resp, &batchResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(batchResult.Embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(batchResult.Embeddings))
	}

	h.handleCall("emb.close", []byte(`{"handle":`+itoa(createResult.Handle)+`}`))
}

func TestHandleCallUnknownOp(t *testing.T) {
	h := newHostHandler("")
	resp := h.handleCall("unknown.op", []byte("{}"))

	var result struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Error == nil {
		t.Fatal("expected error response")
	}
	if result.Error.Code != "unsupported" {
		t.Errorf("expected code=unsupported, got %q", result.Error.Code)
	}
}

func TestStreamLLM(t *testing.T) {
	h := newHostHandler("")

	// Create LLM handle first
	h.handleCall("llm.create", []byte(`{"modelPath":"/models/test.gguf"}`))

	// Start stream
	req, _ := json.Marshal(map[string]interface{}{
		"handle": 2, // first allocID (after create's allocID)
		"prompt": "test",
	})
	stream := h.handleStreamStart("llm.generate_stream", req)
	if stream == nil {
		t.Fatal("expected non-nil stream")
	}

	// Read all chunks, parsing as StreamFrame
	var texts []string
	for {
		stream.mu.Lock()
		if stream.index >= len(stream.chunks) {
			stream.mu.Unlock()
			break
		}
		chunk := stream.chunks[stream.index]
		stream.index++
		stream.mu.Unlock()

		frame, err := device.ParseStreamFrame(chunk)
		if err != nil {
			t.Fatalf("ParseStreamFrame: %v", err)
		}
		if frame.Done {
			break
		}
		if frame.Modality != device.ModalityLLM {
			t.Errorf("expected modality llm, got %q", frame.Modality)
		}
		texts = append(texts, frame.Payload.Text)
	}

	if len(texts) == 0 {
		t.Error("expected at least one text chunk")
	}
}

func TestStreamUnsupported(t *testing.T) {
	h := newHostHandler("")
	stream := h.handleStreamStart("unknown.stream", []byte("{}"))
	if stream != nil {
		t.Error("expected nil stream for unknown op")
	}
}

func TestHostStreamCancel(t *testing.T) {
	h := newHostHandler("")
	stream := h.handleStreamStart("llm.generate_stream", []byte(`{"handle":1,"prompt":"cancel me"}`))
	if stream == nil {
		t.Fatal("expected non-nil stream")
	}

	id := h.allocID()
	h.streams.Store(id, stream)

	if rc := h.hostStreamCancelFn(context.Background(), nil, id); rc != 0 {
		t.Fatalf("hostStreamCancelFn rc=%d, want 0", rc)
	}
	if _, ok := h.streams.Load(id); ok {
		t.Fatal("expected stream to be removed after cancel")
	}
	if rc := h.hostStreamNextFn(context.Background(), nil, id, 0, 0); rc != -1 {
		t.Fatalf("hostStreamNextFn after cancel rc=%d, want -1", rc)
	}
}

func TestHostStreamCancelMissing(t *testing.T) {
	h := newHostHandler("")
	if rc := h.hostStreamCancelFn(context.Background(), nil, 4242); rc != -1 {
		t.Fatalf("hostStreamCancelFn missing stream rc=%d, want -1", rc)
	}
}

func TestShutdown(t *testing.T) {
	h := newHostHandler("")
	resp := h.handleCall("shutdown", []byte("{}"))

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
