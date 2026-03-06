# RunAnywhere Device Bridge (JS)

This bridge implements the `__RunAnywhereDeviceBridge` contract and `__RunAnywhereFetch` (for the WASM HTTP client) expected by the Go WASM SDK. Load it **after** the RunAnywhere Web SDK and **before** `wasm_exec.js` and `runanywhere.wasm`.

**Versioning**: The bridge exposes `version` and `minWebSdkVersion`; use them for compatibility checks when using a Web SDK–backed implementation.

## Load order

1. RunAnywhere Web SDK (core + llamacpp + onnx)
2. This bridge script
3. `wasm_exec.js` (from Go distribution)
4. `runanywhere.wasm`

## Contract (stable)

The bridge object must expose these methods. All async methods accept a **callback** as the last argument; the callback is invoked with a single argument (JSON string result or error).

### Lifecycle

- **init(configJson?: string)** — Initialize; callback receives `{"success":true,"capabilities":["llm","stt",...]}` or error.
- **shutdown()** — Shutdown; callback receives `{"ok":true}`.

### LLM

- **createLLM(argsJson)** — args: `{modelPath, options}`. Callback: `{"success":true,"handle":number}` or `{"success":false,"error":"..."}`.
- **llmGenerate(argsJson)** — args: `{handle,prompt,opts}`. Callback: `{"success":true,"text":"..."}` or `{"success":false,"error":"..."}`.
- **llmGenerateStream(argsJson, onChunk)** — Stream tokens; call `onChunk('{"content":"token"}')` per token, then `onChunk('{"done":true}')`. Then invoke the callback with `"ok"`.
- **closeLLM(argsJson)** — args: `{handle}`. Callback: `"ok"`.

### STT

- **createSTT(argsJson)** — args: `{modelPath?, options}`. Callback: `{"success":true,"handle":number}` or error.
- **sttTranscribe(argsJson)** — args: `{handle,audioData,opts}`. Callback: `{"success":true,"text":"..."}` or error.
- **sttTranscribeStream(argsJson, onChunk)** — Call `onChunk('{"text":"..."}')` per segment, then `onChunk('{"done":true}')`. Then invoke callback.
- **closeSTT(argsJson)** — args: `{handle}`. Callback: `"ok"`.

### TTS / Embeddings

- **createTTS**, **ttsSynthesize**, **closeTTS** — Same pattern (handle, options, callback).
- **createEmbeddings**, **embeddingsEmbed**, **embeddingsEmbedBatch**, **closeEmbeddings** — Same pattern.

### WASM HTTP Transport

- **__RunAnywhereFetch(url, optsJson, callback)** — callback is `(statusCode, headersJson, bodyBase64, errorMsg)`.
- Request/response bodies are base64-encoded binary payloads (decoded/encoded via `Uint8Array`), not text-only strings.
- Returns an object with **abort()** so Go can cancel in-flight requests when context is canceled.

## Error simulation (stub-only)

Use globals before loading `bridge.js`:

- `globalThis.__RunAnywhereBridgeSimulatedErrors = { createLLM: true, embeddingsEmbedBatch: "custom error" }`
- `globalThis.__RunAnywhereFetchSimulatedError = "network unavailable"`

Any method listed in `__RunAnywhereBridgeSimulatedErrors` returns a simulated error.

## Stub implementation

`bridge.js` provides a **stub** implementation that satisfies the contract but does not run real inference. Use it to verify loading and exports. Replace with a Web SDK–backed implementation for real in-browser inference.
