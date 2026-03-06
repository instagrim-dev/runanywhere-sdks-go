//go:build js && wasm

// runanywhere-wasm is the WASM entry point for the RunAnywhere Go SDK.
// Build with: GOOS=js GOARCH=wasm go build -o runanywhere.wasm ./cmd/runanywhere-wasm
//
// Load order: Web SDK → bridge script → wasm_exec.js → runanywhere.wasm
// The bridge must set __RunAnywhereDeviceBridge before any device export is called.
// For HTTP client, the bridge must set __RunAnywhereFetch (see transport_wasm.go).
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall/js"

	runanywhere "github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go"
	"github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go/device"
)

var (
	nextHandle int64
	llmHandles sync.Map // int64 -> *device.LLM
	sttHandles sync.Map // int64 -> *device.STT
	ttsHandles sync.Map // int64 -> *device.TTS
	embHandles sync.Map // int64 -> *device.Embeddings
)

func main() {
	// Verify bridge is present so callers get a clear error instead of panics
	bridge := js.Global().Get("__RunAnywhereDeviceBridge")
	if bridge.IsUndefined() {
		js.Global().Set("__RunAnywhereWasmReady", js.ValueOf(false))
		js.Global().Set("__RunAnywhereWasmError", js.ValueOf("__RunAnywhereDeviceBridge is not defined. Load the bridge script after the Web SDK and before runanywhere.wasm."))
		select {} // keep alive
	}

	ctx := context.Background()

	// Device exports (JSON in, callback with result for async; sync returns JSON string where possible).
	// For browser safety, each sync export also accepts an optional callback as the
	// last argument; when provided, execution runs in a goroutine and the result is
	// delivered asynchronously to avoid blocking the JS event loop.
	// NOTE: The js.FuncOf callbacks below are intentionally never Released. This binary runs for the
	// lifetime of the browser tab (select{} at the end of main), so these functions must remain
	// registered in the JS global namespace. Calling Release() would break the exports.
	jsonResult := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			return `{"error":"failed to marshal JSON result"}`
		}
		return string(b)
	}
	splitArgsAndCallback := func(args []js.Value) ([]js.Value, js.Value, bool) {
		if len(args) == 0 {
			return args, js.Value{}, false
		}
		last := args[len(args)-1]
		if last.Type() == js.TypeFunction {
			return args[:len(args)-1], last, true
		}
		return args, js.Value{}, false
	}
	asyncAck := func() js.Value {
		return js.ValueOf(jsonResult(map[string]any{"ok": true, "async": true}))
	}
	runWithOptionalCallback := func(args []js.Value, run func([]js.Value) string) (js.Value, bool) {
		callArgs, cb, hasCallback := splitArgsAndCallback(args)
		if !hasCallback {
			return js.Value{}, false
		}
		go func() {
			cb.Invoke(js.ValueOf(run(callArgs)))
		}()
		return asyncAck(), true
	}

	js.Global().Set("deviceInit", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			configJSON := "{}"
			if len(callArgs) > 0 && !callArgs[0].IsUndefined() {
				configJSON = callArgs[0].String()
			}
			var cfg *device.Config
			if configJSON != "" && configJSON != "{}" {
				cfg = &device.Config{}
				if err := json.Unmarshal([]byte(configJSON), cfg); err != nil {
					return jsonResult(map[string]any{"error": "invalid config JSON: " + err.Error()})
				}
			}
			err := device.InitWithConfig(ctx, cfg)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(map[string]any{"ok": true})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	js.Global().Set("deviceShutdown", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			err := device.Shutdown()
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(map[string]any{"ok": true})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	js.Global().Set("deviceNewLLM", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "modelPath required"})
			}
			modelPath := callArgs[0].String()
			llm, err := device.NewLLM(ctx, modelPath, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			h := atomic.AddInt64(&nextHandle, 1)
			llmHandles.Store(h, llm)
			return jsonResult(map[string]any{"handle": h})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	js.Global().Set("deviceLLMGenerate", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 2 {
				return jsonResult(map[string]any{"error": "handle and prompt required"})
			}
			h := int64(callArgs[0].Int())
			prompt := callArgs[1].String()
			v, ok := llmHandles.Load(h)
			if !ok {
				return jsonResult(map[string]any{"error": "invalid handle"})
			}
			llm, ok := v.(*device.LLM)
			if !ok || llm == nil {
				return jsonResult(map[string]any{"error": "invalid LLM handle type"})
			}
			text, err := llm.Generate(ctx, prompt, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(map[string]any{"text": text})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	js.Global().Set("deviceCloseLLM", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "handle required"})
			}
			h := int64(callArgs[0].Int())
			v, ok := llmHandles.LoadAndDelete(h)
			if !ok {
				return jsonResult(map[string]any{"ok": true})
			}
			llm, ok := v.(*device.LLM)
			if !ok || llm == nil {
				return jsonResult(map[string]any{"error": "invalid LLM handle type"})
			}
			_ = llm.Close()
			return jsonResult(map[string]any{"ok": true})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	// deviceLLMGenerateStream(handle, prompt, optsJson, onChunk) — onChunk(chunkJson) called per token, then {"done":true}
	js.Global().Set("deviceLLMGenerateStream", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 4 {
			return js.ValueOf(jsonResult(map[string]any{"error": "handle, prompt, optsJson, onChunk required"}))
		}
		h := int64(args[0].Int())
		prompt := args[1].String()
		optsJSON := args[2].String()
		onChunk := args[3]
		v, ok := llmHandles.Load(h)
		if !ok {
			return js.ValueOf(jsonResult(map[string]any{"error": "invalid handle"}))
		}
		llm, ok := v.(*device.LLM)
		if !ok || llm == nil {
			return js.ValueOf(jsonResult(map[string]any{"error": "invalid LLM handle type"}))
		}
		var opts *device.LLMOptions
		if optsJSON != "" && optsJSON != "{}" {
			opts = &device.LLMOptions{}
			if err := json.Unmarshal([]byte(optsJSON), opts); err != nil {
				return js.ValueOf(jsonResult(map[string]any{"error": "invalid options JSON: " + err.Error()}))
			}
		}
		it, err := llm.GenerateStream(ctx, prompt, opts)
		if err != nil {
			return js.ValueOf(jsonResult(map[string]any{"error": err.Error()}))
		}
		go func() {
			defer it.Close()
			for {
				token, isFinal, err := it.Next()
				if err == io.EOF {
					doneFrame, _ := device.NewLLMStreamFrame("", true, 0).ToJSONString()
					onChunk.Invoke(js.ValueOf(doneFrame))
					return
				}
				if err != nil {
					errFrame, _ := device.NewErrorStreamFrame(device.ErrCodeGenerationFailed, err.Error(), 0).ToJSONString()
					onChunk.Invoke(js.ValueOf(errFrame))
					return
				}
				frame, _ := device.NewLLMStreamFrame(token, isFinal, 0).ToJSONString()
				onChunk.Invoke(js.ValueOf(frame))
				if isFinal {
					return
				}
			}
		}()
		return js.ValueOf(jsonResult(map[string]any{"ok": true}))
	}))

	// STT exports
	js.Global().Set("deviceNewSTT", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "modelPath required"})
			}
			modelPath := callArgs[0].String()
			stt, err := device.NewSTT(ctx, modelPath, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			h := atomic.AddInt64(&nextHandle, 1)
			sttHandles.Store(h, stt)
			return jsonResult(map[string]any{"handle": h})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("deviceSTTTranscribe", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 2 {
				return jsonResult(map[string]any{"error": "handle and audioBase64 required"})
			}
			h := int64(callArgs[0].Int())
			audioB64 := callArgs[1].String()
			v, ok := sttHandles.Load(h)
			if !ok {
				return jsonResult(map[string]any{"error": "invalid handle"})
			}
			stt, ok := v.(*device.STT)
			if !ok || stt == nil {
				return jsonResult(map[string]any{"error": "invalid STT handle type"})
			}
			audioData, err := base64.StdEncoding.DecodeString(audioB64)
			if err != nil {
				return jsonResult(map[string]any{"error": "invalid audioBase64: " + err.Error()})
			}
			text, err := stt.Transcribe(ctx, audioData, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(map[string]any{"text": text})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("deviceCloseSTT", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "handle required"})
			}
			h := int64(callArgs[0].Int())
			v, ok := sttHandles.LoadAndDelete(h)
			if !ok {
				return jsonResult(map[string]any{"ok": true})
			}
			stt, ok := v.(*device.STT)
			if !ok || stt == nil {
				return jsonResult(map[string]any{"error": "invalid STT handle type"})
			}
			stt.Close()
			return jsonResult(map[string]any{"ok": true})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	// TTS exports
	js.Global().Set("deviceNewTTS", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "voicePath required"})
			}
			voicePath := callArgs[0].String()
			tts, err := device.NewTTS(ctx, voicePath, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			h := atomic.AddInt64(&nextHandle, 1)
			ttsHandles.Store(h, tts)
			return jsonResult(map[string]any{"handle": h})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("deviceTTSynthesize", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 2 {
				return jsonResult(map[string]any{"error": "handle and text required"})
			}
			h := int64(callArgs[0].Int())
			text := callArgs[1].String()
			v, ok := ttsHandles.Load(h)
			if !ok {
				return jsonResult(map[string]any{"error": "invalid handle"})
			}
			tts, ok := v.(*device.TTS)
			if !ok || tts == nil {
				return jsonResult(map[string]any{"error": "invalid TTS handle type"})
			}
			audio, err := tts.Synthesize(ctx, text, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(map[string]any{"audio": base64.StdEncoding.EncodeToString(audio)})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("deviceCloseTTS", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "handle required"})
			}
			h := int64(callArgs[0].Int())
			v, ok := ttsHandles.LoadAndDelete(h)
			if !ok {
				return jsonResult(map[string]any{"ok": true})
			}
			tts, ok := v.(*device.TTS)
			if !ok || tts == nil {
				return jsonResult(map[string]any{"error": "invalid TTS handle type"})
			}
			tts.Close()
			return jsonResult(map[string]any{"ok": true})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	// Embeddings exports
	js.Global().Set("deviceNewEmbeddings", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "modelPath required"})
			}
			modelPath := callArgs[0].String()
			emb, err := device.NewEmbeddings(ctx, modelPath, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			h := atomic.AddInt64(&nextHandle, 1)
			embHandles.Store(h, emb)
			return jsonResult(map[string]any{"handle": h})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("deviceEmbeddingsEmbed", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 2 {
				return jsonResult(map[string]any{"error": "handle and text required"})
			}
			h := int64(callArgs[0].Int())
			text := callArgs[1].String()
			v, ok := embHandles.Load(h)
			if !ok {
				return jsonResult(map[string]any{"error": "invalid handle"})
			}
			emb, ok := v.(*device.Embeddings)
			if !ok || emb == nil {
				return jsonResult(map[string]any{"error": "invalid embeddings handle type"})
			}
			vec, err := emb.Embed(ctx, text, nil)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(map[string]any{"embedding": vec})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("deviceCloseEmbeddings", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "handle required"})
			}
			h := int64(callArgs[0].Int())
			v, ok := embHandles.LoadAndDelete(h)
			if !ok {
				return jsonResult(map[string]any{"ok": true})
			}
			emb, ok := v.(*device.Embeddings)
			if !ok || emb == nil {
				return jsonResult(map[string]any{"error": "invalid embeddings handle type"})
			}
			emb.Close()
			return jsonResult(map[string]any{"ok": true})
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	// Client exports (require __RunAnywhereFetch from bridge)
	makeClient := func(baseURL string) *runanywhere.Client {
		return runanywhere.NewClient(baseURL, runanywhere.WithHTTPClient(&http.Client{Transport: runanywhere.DefaultWASMTransport()}))
	}
	js.Global().Set("clientHealth", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "baseURL required"})
			}
			health, err := makeClient(callArgs[0].String()).Health(ctx)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(health)
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("clientListModels", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 1 {
				return jsonResult(map[string]any{"error": "baseURL required"})
			}
			models, err := makeClient(callArgs[0].String()).ListModels(ctx)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(models)
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))
	js.Global().Set("clientChat", js.FuncOf(func(this js.Value, args []js.Value) any {
		run := func(callArgs []js.Value) string {
			if len(callArgs) < 2 {
				return jsonResult(map[string]any{"error": "baseURL and requestJson required"})
			}
			baseURL := callArgs[0].String()
			reqJSON := callArgs[1].String()
			var req runanywhere.ChatCompletionRequest
			if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
				return jsonResult(map[string]any{"error": "invalid request: " + err.Error()})
			}
			resp, err := makeClient(baseURL).Chat(ctx, &req)
			if err != nil {
				return jsonResult(map[string]any{"error": err.Error()})
			}
			return jsonResult(resp)
		}
		if ret, ok := runWithOptionalCallback(args, run); ok {
			return ret
		}
		return js.ValueOf(run(args))
	}))

	js.Global().Set("__RunAnywhereWasmReady", js.ValueOf(true))
	select {} // keep alive
}
