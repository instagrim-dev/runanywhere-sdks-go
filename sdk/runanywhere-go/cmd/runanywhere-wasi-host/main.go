// Command runanywhere-wasi-host is a reference WASI host adapter using wazero.
// It loads a WASI module (e.g. the runanywhere Go SDK compiled to wasip1/wasm)
// and provides the "runanywhere" host module with all required host functions.
//
// Usage:
//
//	go run ./cmd/runanywhere-wasi-host -module path/to/runanywhere.wasm
//
// The host implements the runanywhere host ABI:
//   - runanywhere.call(op, op_len, req, req_len, resp, resp_cap) → i32
//   - runanywhere.call_stream_start(op, op_len, req, req_len) → i64
//   - runanywhere.call_stream_next(stream, chunk, chunk_cap) → i32
//   - runanywhere.call_stream_cancel(stream) → i32
//
// By default it uses a mock backend that echoes prompts. The -server flag is
// currently reserved and does not yet proxy requests.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go/device"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func main() {
	modulePath := flag.String("module", "", "Path to WASI module (.wasm)")
	serverURL := flag.String("server", "", "Reserved: RunAnywhere server URL (proxy mode not implemented)")
	flag.Parse()

	if *modulePath == "" {
		fmt.Fprintln(os.Stderr, "usage: runanywhere-wasi-host -module <path.wasm>")
		os.Exit(1)
	}

	wasmBytes, err := os.ReadFile(*modulePath)
	if err != nil {
		log.Fatalf("failed to read module: %v", err)
	}

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Instantiate WASI preview1 (for fd_write, args_get, etc.)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	// Create the host handler
	handler := newHostHandler(*serverURL)

	// Register the "runanywhere" host module
	builder := rt.NewHostModuleBuilder("runanywhere")

	builder.NewFunctionBuilder().
		WithFunc(handler.hostCallFn).
		WithParameterNames("op_ptr", "op_len", "req_ptr", "req_len", "resp_ptr", "resp_cap").
		Export("call")

	builder.NewFunctionBuilder().
		WithFunc(handler.hostStreamStartFn).
		WithParameterNames("op_ptr", "op_len", "req_ptr", "req_len").
		Export("call_stream_start")

	builder.NewFunctionBuilder().
		WithFunc(handler.hostStreamNextFn).
		WithParameterNames("stream", "chunk_ptr", "chunk_cap").
		Export("call_stream_next")

	builder.NewFunctionBuilder().
		WithFunc(handler.hostStreamCancelFn).
		WithParameterNames("stream").
		Export("call_stream_cancel")

	if _, err := builder.Instantiate(ctx); err != nil {
		log.Fatalf("failed to instantiate runanywhere module: %v", err)
	}

	// Instantiate the guest module
	config := wazero.NewModuleConfig().
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithArgs("runanywhere")

	if _, err := rt.InstantiateWithConfig(ctx, wasmBytes, config); err != nil {
		log.Fatalf("failed to instantiate guest: %v", err)
	}
}

// =============================================================================
// Host Handler
// =============================================================================

type hostHandler struct {
	serverURL string
	handles   sync.Map // handle → mockHandle
	streams   sync.Map // streamID → *mockStream
	nextID    atomic.Int64
}

type mockHandle struct {
	kind      string // "llm", "stt", "tts", "emb"
	modelPath string
}

type mockStream struct {
	chunks [][]byte
	index  int
	mu     sync.Mutex
}

func newHostHandler(serverURL string) *hostHandler {
	h := &hostHandler{serverURL: serverURL}
	h.nextID.Store(1)
	return h
}

func (h *hostHandler) allocID() int64 {
	return h.nextID.Add(1)
}

// readMem reads bytes from guest linear memory.
func readMem(mod api.Module, ptr, length uint32) ([]byte, bool) {
	return mod.Memory().Read(ptr, length)
}

// writeMem writes bytes into guest linear memory.
func writeMem(mod api.Module, ptr uint32, data []byte) bool {
	return mod.Memory().Write(ptr, data)
}

// =============================================================================
// Host Function: call (synchronous)
// =============================================================================

func (h *hostHandler) hostCallFn(ctx context.Context, mod api.Module, opPtr, opLen, reqPtr, reqLen, respPtr, respCap uint32) int32 {
	opBytes, ok := readMem(mod, opPtr, opLen)
	if !ok {
		return -1
	}
	reqBytes, ok := readMem(mod, reqPtr, reqLen)
	if !ok {
		return -1
	}

	op := string(opBytes)
	resp := h.handleCall(op, reqBytes)

	if uint32(len(resp)) > respCap {
		return -2 // overflow
	}

	if !writeMem(mod, respPtr, resp) {
		return -1
	}

	return int32(len(resp))
}

// =============================================================================
// Host Function: call_stream_start
// =============================================================================

func (h *hostHandler) hostStreamStartFn(ctx context.Context, mod api.Module, opPtr, opLen, reqPtr, reqLen uint32) int64 {
	opBytes, ok := readMem(mod, opPtr, opLen)
	if !ok {
		return -1
	}
	reqBytes, ok := readMem(mod, reqPtr, reqLen)
	if !ok {
		return -1
	}

	op := string(opBytes)
	stream := h.handleStreamStart(op, reqBytes)
	if stream == nil {
		return -1
	}

	id := h.allocID()
	h.streams.Store(id, stream)
	return id
}

// =============================================================================
// Host Function: call_stream_next
// =============================================================================

func (h *hostHandler) hostStreamNextFn(ctx context.Context, mod api.Module, stream int64, chunkPtr, chunkCap uint32) int32 {
	val, ok := h.streams.Load(stream)
	if !ok {
		return -1
	}

	s := val.(*mockStream)
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index >= len(s.chunks) {
		h.streams.Delete(stream)
		return 0 // done
	}

	chunk := s.chunks[s.index]
	s.index++

	if uint32(len(chunk)) > chunkCap {
		return -2
	}

	if !writeMem(mod, chunkPtr, chunk) {
		return -1
	}

	return int32(len(chunk))
}

// =============================================================================
// Host Function: call_stream_cancel
// =============================================================================

func (h *hostHandler) hostStreamCancelFn(ctx context.Context, mod api.Module, stream int64) int32 {
	if _, ok := h.streams.Load(stream); !ok {
		return -1
	}
	h.streams.Delete(stream)
	return 0
}

// =============================================================================
// Response Types
// =============================================================================

type initResponse struct {
	Success      bool     `json:"success"`
	Capabilities []string `json:"capabilities"`
}

type capabilitiesResponse struct {
	Capabilities []string `json:"capabilities"`
}

type successResponse struct {
	Success bool `json:"success"`
}

type handleResponse struct {
	Handle int64 `json:"handle"`
}

type textResponse struct {
	Text string `json:"text"`
}

type synthesizeResponse struct {
	AudioData string `json:"audioData"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

type embedBatchResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type hostError struct {
	Error hostErrorDetail `json:"error"`
}

type hostErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// =============================================================================
// Request Types
// =============================================================================

type createRequest struct {
	ModelPath string `json:"modelPath"`
	VoicePath string `json:"voicePath"`
}

type generateRequest struct {
	Handle int64  `json:"handle"`
	Prompt string `json:"prompt"`
}

type embedBatchRequest struct {
	Handle int64    `json:"handle"`
	Texts  []string `json:"texts"`
}

type closeRequest struct {
	Handle int64 `json:"handle"`
}

// =============================================================================
// Pre-computed Static Responses
//
// These are shared read-only slices returned directly by handleCall.
// Callers (hostCallFn) must NOT mutate the returned []byte.
// =============================================================================

var (
	initResp         = mustMarshal(initResponse{Success: true, Capabilities: defaultCapabilities})
	shutdownResp     = mustMarshal(successResponse{Success: true})
	capsResp         = mustMarshal(capabilitiesResponse{Capabilities: defaultCapabilities})
	closeSuccessResp = mustMarshal(successResponse{Success: true})
	sttTranscribeResp = mustMarshal(textResponse{Text: "[mock STT] transcribed audio"})
	ttsSynthesizeResp = mustMarshal(synthesizeResponse{AudioData: "bW9jayBhdWRpbyBkYXRh"}) // base64("mock audio data")
	embEmbedResp      = mustMarshal(embedResponse{Embedding: []float32{0.1, 0.2, 0.3, 0.4}})
	streamDoneChunkLLM = mustMarshal(device.NewLLMStreamFrame("", true, 0))
	streamDoneChunkSTT = mustMarshal(device.NewSTTStreamFrame("", true, 0))
)

var defaultCapabilities = []string{"llm", "stt", "tts", "embeddings", "streaming"}

// =============================================================================
// Mock Backend
// =============================================================================

func (h *hostHandler) handleCall(op string, req []byte) []byte {
	decode := func(dst any) []byte {
		if err := json.Unmarshal(req, dst); err != nil {
			return mustMarshal(hostError{Error: hostErrorDetail{
				Code:    "invalid_param",
				Message: "invalid request JSON: " + err.Error(),
			}})
		}
		return nil
	}

	switch op {
	case "init":
		return initResp

	case "shutdown":
		return shutdownResp

	case "capabilities":
		return capsResp

	case "llm.create", "stt.create", "tts.create", "emb.create":
		var r createRequest
		if errResp := decode(&r); errResp != nil {
			return errResp
		}
		id := h.allocID()
		kind := op[:3]
		modelPath := r.ModelPath
		if modelPath == "" {
			modelPath = r.VoicePath
		}
		h.handles.Store(id, &mockHandle{kind: kind, modelPath: modelPath})
		return mustMarshal(handleResponse{Handle: id})

	case "llm.generate":
		var r generateRequest
		if errResp := decode(&r); errResp != nil {
			return errResp
		}
		return mustMarshal(textResponse{Text: "[mock LLM] Echo: " + r.Prompt})

	case "stt.transcribe":
		return sttTranscribeResp

	case "tts.synthesize":
		return ttsSynthesizeResp

	case "emb.embed":
		return embEmbedResp

	case "emb.embed_batch":
		var r embedBatchRequest
		if errResp := decode(&r); errResp != nil {
			return errResp
		}
		embeddings := make([][]float32, len(r.Texts))
		for i := range r.Texts {
			embeddings[i] = []float32{0.1, 0.2, float32(i)}
		}
		return mustMarshal(embedBatchResponse{Embeddings: embeddings})

	case "llm.close", "stt.close", "tts.close", "emb.close":
		var r closeRequest
		if errResp := decode(&r); errResp != nil {
			return errResp
		}
		h.handles.Delete(r.Handle)
		return closeSuccessResp

	default:
		return mustMarshal(hostError{Error: hostErrorDetail{
			Code:    "unsupported",
			Message: "unknown operation: " + op,
		}})
	}
}

func (h *hostHandler) handleStreamStart(op string, req []byte) *mockStream {
	switch op {
	case "llm.generate_stream":
		var r generateRequest
		if err := json.Unmarshal(req, &r); err != nil {
			return nil
		}

		// Simulate streaming by splitting into word tokens
		tokens := []string{"[mock]", " ", "Echo:", " ", r.Prompt}
		chunks := make([][]byte, 0, len(tokens)+1)
		for _, t := range tokens {
			chunks = append(chunks, mustMarshal(device.NewLLMStreamFrame(t, false, 0)))
		}
		chunks = append(chunks, streamDoneChunkLLM)
		return &mockStream{chunks: chunks}

	case "stt.transcribe_stream":
		return &mockStream{chunks: [][]byte{
			mustMarshal(device.NewSTTStreamFrame("[mock]", false, 0)),
			mustMarshal(device.NewSTTStreamFrame(" transcribed", false, 0)),
			streamDoneChunkSTT,
		}}

	default:
		return nil
	}
}

// mustMarshal marshals v to JSON. It panics if marshaling fails, which
// indicates a programming error (e.g. unmarshallable types like chan or func).
func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic("mustMarshal: " + err.Error())
	}
	return data
}
