package device

// Tests in this package that do not need CGO (e.g. TestErrorSentinels) run with
// CGO_ENABLED=0. When CGO_ENABLED=1, the package links against rac_commons and
// backend libs; go test ./device/... then requires those shared libraries to be
// on the library path (e.g. LD_LIBRARY_PATH or dyld path).

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

type modeTestBackend struct {
	initErr     error
	caps        CapabilitySet
	initialized bool
	mode        BackendMode
}

func (b *modeTestBackend) Init(ctx context.Context, cfg *Config) error {
	if b.initErr != nil {
		return b.initErr
	}
	b.initialized = true
	return nil
}

func (b *modeTestBackend) Shutdown(ctx context.Context) error {
	b.initialized = false
	return nil
}

func (b *modeTestBackend) Capabilities() CapabilitySet {
	return b.caps
}

func (b *modeTestBackend) IsInitialized() bool {
	return b.initialized
}

func (b *modeTestBackend) Mode() BackendMode {
	return b.mode
}

func TestErrorSentinels(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("ErrUnsupported should not be nil")
	}
	if ErrNotInitialized == nil {
		t.Fatal("ErrNotInitialized should not be nil")
	}
	if ErrHandlesStillOpen == nil {
		t.Fatal("ErrHandlesStillOpen should not be nil")
	}
	if ErrCancelled == nil {
		t.Fatal("ErrCancelled should not be nil")
	}
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("errors.Is(ErrUnsupported, ErrUnsupported) should be true")
	}
}

func TestCapabilitySet(t *testing.T) {
	var c CapabilitySet
	if c.Has(CapabilityLLM) {
		t.Error("empty set should not have LLM")
	}
	c = c.With(CapabilityLLM, CapabilitySTT)
	if !c.Has(CapabilityLLM) || !c.Has(CapabilitySTT) {
		t.Error("With then Has should be true")
	}
	if !c.HasAny(CapabilityLLM, CapabilityTTS) {
		t.Error("HasAny(LLM, TTS) should be true when LLM present")
	}
	if c.HasAll(CapabilityLLM, CapabilityTTS) {
		t.Error("HasAll(LLM, TTS) should be false when TTS not present")
	}
	c = c.Without(CapabilitySTT)
	if c.Has(CapabilitySTT) {
		t.Error("Without should remove capability")
	}
	if c.String() == "" {
		t.Error("String() should not be empty when set has caps")
	}
	// Predefined sets
	if !CapabilitySetLLM.Has(CapabilityLLM) || CapabilitySetLLM.Has(CapabilitySTT) {
		t.Error("CapabilitySetLLM should have only LLM")
	}
	if !CapabilitySetAll.Has(CapabilityStreaming) {
		t.Error("CapabilitySetAll should have streaming")
	}
	if CapabilitySetNone != 0 {
		t.Error("CapabilitySetNone should be zero")
	}
}

func TestFallbackChain(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{}

	chain := NewFallbackChain(&stubBackend{})
	if chain.Len() != 1 {
		t.Errorf("Len() = %d, want 1", chain.Len())
	}
	chain.Add(&stubBackend{})
	if chain.Len() != 2 {
		t.Errorf("after Add, Len() = %d, want 2", chain.Len())
	}
	if chain.Get(0) == nil || chain.Get(1) == nil || chain.Get(2) != nil {
		t.Error("Get() should return backends for 0,1 and nil for 2")
	}

	be, err := chain.TryInit(ctx, cfg)
	if err != nil {
		t.Fatalf("TryInit: %v", err)
	}
	if be == nil {
		t.Fatal("TryInit should return first successful backend")
	}
	if !be.IsInitialized() {
		t.Error("returned backend should be initialized")
	}
}

func TestTryInitWithModeWASI(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{}
	wasiBack := &modeTestBackend{caps: CapabilitySetLLM, mode: BackendModeWASI}

	chain := NewFallbackChain(wasiBack)
	be, err := chain.TryInitWithMode(ctx, cfg, BackendModeWASI)
	if err != nil {
		t.Fatalf("TryInitWithMode(WASI): %v", err)
	}
	if be == nil || !be.IsInitialized() {
		t.Fatal("expected WASI backend to be selected and initialized")
	}
}

func TestTryInitWithModeFiltersBackends(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{}

	stubBe := &modeTestBackend{caps: CapabilitySetNone, mode: BackendModeStub}
	wasiBe := &modeTestBackend{caps: CapabilitySetLLM, mode: BackendModeWASI}

	// Chain has stub first, wasi second. Requesting WASI should skip stub.
	chain := NewFallbackChain(stubBe, wasiBe)
	be, err := chain.TryInitWithMode(ctx, cfg, BackendModeWASI)
	if err != nil {
		t.Fatalf("TryInitWithMode(WASI): %v", err)
	}
	if be != wasiBe {
		t.Fatal("expected WASI backend to be selected, not stub")
	}
	if stubBe.initialized {
		t.Fatal("stub backend should not have been initialized when requesting WASI mode")
	}
}

func TestTryInitWithModeWASMMatchesBoth(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{}

	wasmBrowserBe := &modeTestBackend{caps: CapabilitySetLLM, mode: BackendModeWASMBrowser}
	wasiBe := &modeTestBackend{caps: CapabilitySetLLM, mode: BackendModeWASI}

	// BackendModeWASM should match both browser and WASI; picks first.
	chain := NewFallbackChain(wasmBrowserBe, wasiBe)
	be, err := chain.TryInitWithMode(ctx, cfg, BackendModeWASM)
	if err != nil {
		t.Fatalf("TryInitWithMode(WASM): %v", err)
	}
	if be != wasmBrowserBe {
		t.Fatal("expected WASM browser backend to be selected first")
	}
}

func TestTryInitWithModeNoMatch(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{}
	stubBe := &modeTestBackend{caps: CapabilitySetNone, mode: BackendModeStub}

	chain := NewFallbackChain(stubBe)
	_, err := chain.TryInitWithMode(ctx, cfg, BackendModeWASI)
	if err == nil {
		t.Fatal("expected error when no WASI backend is in chain")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestRACErrorUnwrapAndIs(t *testing.T) {
	// Unwrap maps to sentinels for errors.Is compatibility
	e := &RACError{Code: ErrCodeUnsupported, Message: "test"}
	if e.Unwrap() != ErrUnsupported {
		t.Errorf("Unwrap() for ErrCodeUnsupported should return ErrUnsupported, got %v", e.Unwrap())
	}
	if !errors.Is(e, ErrUnsupported) {
		t.Error("errors.Is(RACError with Unsupported code, ErrUnsupported) should be true")
	}

	e2 := &RACError{Code: ErrCodeNotInitialized, Message: "test"}
	if e2.Unwrap() != ErrNotInitialized {
		t.Errorf("Unwrap() for ErrCodeNotInitialized should return ErrNotInitialized")
	}

	e3 := &RACError{Code: ErrCodeTimeout, Message: "test"}
	if e3.Unwrap() != context.DeadlineExceeded {
		t.Errorf("Unwrap() for ErrCodeTimeout should return DeadlineExceeded")
	}

	e4 := &RACError{Code: ErrCodeUnknown, Message: "test"}
	if e4.Unwrap() != nil {
		t.Error("Unwrap() for unknown code should return nil")
	}

	// ErrCodeCancelled should unwrap to ErrCancelled (the package sentinel)
	e5 := &RACError{Code: ErrCodeCancelled, Message: "test"}
	if e5.Unwrap() != ErrCancelled {
		t.Errorf("Unwrap() for ErrCodeCancelled should return ErrCancelled, got %v", e5.Unwrap())
	}
	if !errors.Is(e5, ErrCancelled) {
		t.Error("errors.Is(RACError with Cancelled code, ErrCancelled) should be true")
	}
}

func TestParseStreamFrame(t *testing.T) {
	// Modality + payload format
	f, err := ParseStreamFrameFromString(`{"modality":"llm","payload":{"text":"hi"},"done":false}`)
	if err != nil {
		t.Fatalf("ParseStreamFrameFromString: %v", err)
	}
	if f.Modality != ModalityLLM || f.Payload.Text != "hi" || f.Done {
		t.Errorf("got modality=%q payload.text=%q done=%v", f.Modality, f.Payload.Text, f.Done)
	}
	// Done frame
	f2, err := ParseStreamFrameFromString(`{"done":true}`)
	if err != nil {
		t.Fatalf("ParseStreamFrameFromString done: %v", err)
	}
	if !f2.Done {
		t.Error("expected done=true")
	}
	// Error frame
	f3, err := ParseStreamFrameFromString(`{"error":{"code":"err","message":"msg"},"done":true}`)
	if err != nil {
		t.Fatalf("ParseStreamFrameFromString error: %v", err)
	}
	if f3.Error == nil || f3.Error.Message != "msg" {
		t.Errorf("expected error message, got %v", f3.Error)
	}
}

func TestStreamFrameBuilders(t *testing.T) {
	f := NewLLMStreamFrame("token", false)
	if f.Modality != ModalityLLM || f.Payload.Text != "token" || f.Done {
		t.Errorf("NewLLMStreamFrame: %+v", f)
	}
	fDone := NewLLMStreamFrame("", true)
	if !fDone.Done {
		t.Error("expected done=true")
	}
	errF := NewErrorStreamFrame(ErrorCode("code"), "message")
	if errF.Error == nil || errF.Error.Code != ErrorCode("code") || !errF.Done {
		t.Errorf("NewErrorStreamFrame: %+v", errF)
	}
}

func TestVectorBinRoundTrip(t *testing.T) {
	t.Run("exact fidelity", func(t *testing.T) {
		want := []float32{1.0, -2.5, 3.14, 0, 1e-7}
		encoded := EncodeVectorBin(want)
		got, err := DecodeVectorBin(encoded)
		if err != nil {
			t.Fatalf("DecodeVectorBin: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("len: got %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("[%d] got %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("empty/nil", func(t *testing.T) {
		if s := EncodeVectorBin(nil); s != "" {
			t.Errorf("EncodeVectorBin(nil) = %q, want empty", s)
		}
		got, err := DecodeVectorBin("")
		if err != nil {
			t.Fatalf("DecodeVectorBin(\"\") should not error, got %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty slice, got len %d", len(got))
		}
	})

	t.Run("NaN and Inf", func(t *testing.T) {
		want := []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}
		encoded := EncodeVectorBin(want)
		got, err := DecodeVectorBin(encoded)
		if err != nil {
			t.Fatalf("DecodeVectorBin: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("len: got %d, want 3", len(got))
		}
		// Compare bit patterns for NaN (NaN != NaN)
		if math.Float32bits(got[0]) != math.Float32bits(want[0]) {
			t.Errorf("NaN bits: got %x, want %x", math.Float32bits(got[0]), math.Float32bits(want[0]))
		}
		if got[1] != want[1] {
			t.Errorf("+Inf: got %v, want %v", got[1], want[1])
		}
		if got[2] != want[2] {
			t.Errorf("-Inf: got %v, want %v", got[2], want[2])
		}
	})

	t.Run("malformed base64", func(t *testing.T) {
		_, err := DecodeVectorBin("not-base64!!!")
		if err == nil {
			t.Fatal("expected error for malformed base64")
		}
	})

	t.Run("truncated data", func(t *testing.T) {
		// 5 bytes is not a multiple of 4; use valid base64 for 5 bytes.
		// base64("ABCDE") = "QUJDREU="
		_, err := DecodeVectorBin("QUJDREU=")
		if err == nil {
			t.Fatal("expected error for truncated data (not multiple of 4)")
		}
		if !errors.Is(err, ErrUnsupported) {
			// Error should be a RACError with ErrCodeInvalidParam
			var racErr *RACError
			if !errors.As(err, &racErr) || racErr.Code != ErrCodeInvalidParam {
				t.Errorf("expected RACError with code invalid_param, got %v", err)
			}
		}
	})
}

func TestStreamPayloadMarshalJSON(t *testing.T) {
	t.Run("builder frame emits vector_bin not vector", func(t *testing.T) {
		frame := NewEmbeddingsStreamFrame([]float32{1.0, 2.0, 3.0}, false)
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		var raw map[string]json.RawMessage
		json.Unmarshal(data, &raw)

		var payload map[string]json.RawMessage
		json.Unmarshal(raw["payload"], &payload)

		if _, ok := payload["vector_bin"]; !ok {
			t.Error("expected vector_bin in JSON output")
		}
		if _, ok := payload["vector"]; ok {
			t.Error("should not have vector when vector_bin is set")
		}
	})

	t.Run("manual vector only emits vector", func(t *testing.T) {
		frame := &StreamFrame{
			Modality: ModalityEmbeddings,
			Payload:  StreamPayload{Vector: []float32{1.0, 2.0}},
		}
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		var raw map[string]json.RawMessage
		json.Unmarshal(data, &raw)

		var payload map[string]json.RawMessage
		json.Unmarshal(raw["payload"], &payload)

		if _, ok := payload["vector"]; !ok {
			t.Error("expected vector in JSON output")
		}
		if _, ok := payload["vector_bin"]; ok {
			t.Error("should not have vector_bin when only vector is set")
		}
	})

	t.Run("parse vector_bin populates Vector", func(t *testing.T) {
		want := []float32{1.5, 2.5, 3.5}
		frame := NewEmbeddingsStreamFrame(want, false)
		data, _ := json.Marshal(frame)

		parsed, err := ParseStreamFrame(data)
		if err != nil {
			t.Fatalf("ParseStreamFrame: %v", err)
		}
		if parsed.Payload.VectorBin != "" {
			t.Error("VectorBin should be cleared after parse")
		}
		if len(parsed.Payload.Vector) != len(want) {
			t.Fatalf("Vector len: got %d, want %d", len(parsed.Payload.Vector), len(want))
		}
		for i := range want {
			if parsed.Payload.Vector[i] != want[i] {
				t.Errorf("[%d] got %v, want %v", i, parsed.Payload.Vector[i], want[i])
			}
		}
	})

	t.Run("parse legacy vector array", func(t *testing.T) {
		legacyJSON := `{"modality":"embeddings","payload":{"vector":[4.0,5.0,6.0]},"done":false}`
		parsed, err := ParseStreamFrameFromString(legacyJSON)
		if err != nil {
			t.Fatalf("ParseStreamFrameFromString: %v", err)
		}
		if parsed.Payload.VectorBin != "" {
			t.Error("VectorBin should be empty for legacy frames")
		}
		if len(parsed.Payload.Vector) != 3 {
			t.Fatalf("Vector len: got %d, want 3", len(parsed.Payload.Vector))
		}
		if parsed.Payload.Vector[0] != 4.0 || parsed.Payload.Vector[1] != 5.0 || parsed.Payload.Vector[2] != 6.0 {
			t.Errorf("unexpected vector values: %v", parsed.Payload.Vector)
		}
	})

	t.Run("round trip build-marshal-parse", func(t *testing.T) {
		want := []float32{0.1, 0.2, 0.3, 100.0, -50.5}
		frame := NewEmbeddingsStreamFrame(want, true)
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		parsed, err := ParseStreamFrame(data)
		if err != nil {
			t.Fatalf("ParseStreamFrame: %v", err)
		}
		if !parsed.Done {
			t.Error("expected done=true after round trip")
		}
		if len(parsed.Payload.Vector) != len(want) {
			t.Fatalf("Vector len: got %d, want %d", len(parsed.Payload.Vector), len(want))
		}
		for i := range want {
			if parsed.Payload.Vector[i] != want[i] {
				t.Errorf("[%d] got %v, want %v", i, parsed.Payload.Vector[i], want[i])
			}
		}
	})
}

func TestStreamFrameTimestamp(t *testing.T) {
	t.Run("auto timestamp", func(t *testing.T) {
		f := NewLLMStreamFrame("hi", false)
		if f.Timestamp <= 0 {
			t.Errorf("expected positive timestamp, got %d", f.Timestamp)
		}
	})

	t.Run("explicit timestamp", func(t *testing.T) {
		f := NewLLMStreamFrame("hi", false, 42)
		if f.Timestamp != 42 {
			t.Errorf("expected timestamp=42, got %d", f.Timestamp)
		}
	})
}
