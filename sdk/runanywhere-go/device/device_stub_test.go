//go:build !cgo

package device

import (
	"context"
	"errors"
	"testing"
)

func TestStubInitReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	err := Init(ctx)
	if err == nil {
		t.Fatal("Init expected to return error when built without CGO")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("Init: got %v, want ErrUnsupported", err)
	}
}

func TestStubInitWithConfigReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	err := InitWithConfig(ctx, nil)
	if err == nil {
		t.Fatal("InitWithConfig expected to return error when built without CGO")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("InitWithConfig: got %v, want ErrUnsupported", err)
	}
}

func TestStubShutdownReturnsErrUnsupported(t *testing.T) {
	err := Shutdown()
	if err == nil {
		t.Fatal("Shutdown expected to return error when built without CGO")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("Shutdown: got %v, want ErrUnsupported", err)
	}
}

func TestStubNewLLMReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	llm, err := NewLLM(ctx, "/path/to/model.gguf", nil)
	if err == nil {
		t.Fatal("NewLLM expected to return error when built without CGO")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("NewLLM: got %v, want ErrUnsupported", err)
	}
	if llm != nil {
		t.Errorf("NewLLM: got %v, want nil", llm)
	}
}

func TestStubNewSTTReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	stt, err := NewSTT(ctx, "/path/to/stt-model", nil)
	if err == nil {
		t.Fatal("NewSTT expected to return error when built without CGO")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("NewSTT: got %v, want ErrUnsupported", err)
	}
	if stt != nil {
		t.Errorf("NewSTT: got %v, want nil", stt)
	}
}

func TestStubNewTTSReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	tts, err := NewTTS(ctx, "/path/to/tts-model", nil)
	if err == nil {
		t.Fatal("NewTTS expected to return error when built without CGO")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("NewTTS: got %v, want ErrUnsupported", err)
	}
	if tts != nil {
		t.Errorf("NewTTS: got %v, want nil", tts)
	}
}

func TestStubNewEmbeddingsReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	emb, err := NewEmbeddings(ctx, "/path/to/embed-model", nil)
	if err == nil {
		t.Fatal("NewEmbeddings expected to return error when built without CGO")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("NewEmbeddings: got %v, want ErrUnsupported", err)
	}
	if emb != nil {
		t.Errorf("NewEmbeddings: got %v, want nil", emb)
	}
}
