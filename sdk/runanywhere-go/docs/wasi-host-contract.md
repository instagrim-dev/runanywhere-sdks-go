# WASI Host Contract

This document specifies the host-side interface that a WASI runtime must implement to support the RunAnywhere Go SDK compiled to `GOOS=wasip1 GOARCH=wasm`.

## Overview

The Go SDK WASI module imports four functions from a `runanywhere` host module. All data exchange uses **JSON payloads in shared linear memory**. This design mirrors the browser WASM bridge protocol, so the same `StreamFrame` format, error codes, and capability strings work across both backends.

## Host Module: `runanywhere`

### Imports

| Function | Signature | Description |
|----------|-----------|-------------|
| `call` | `(op_ptr: i32, op_len: i32, req_ptr: i32, req_len: i32, resp_ptr: i32, resp_cap: i32) → i32` | Synchronous request/response. Returns bytes written to `resp`, or negative error code. |
| `call_stream_start` | `(op_ptr: i32, op_len: i32, req_ptr: i32, req_len: i32) → i64` | Start a streaming operation. Returns stream handle (>0) or negative error. |
| `call_stream_next` | `(stream: i64, chunk_ptr: i32, chunk_cap: i32) → i32` | Read next chunk. Returns bytes written (>0), 0 = done, negative = error. |
| `call_stream_cancel` | `(stream: i64) → i32` | Cancel and release a streaming operation. Returns 0 on success, negative error on failure. |

### Memory Protocol

- **Request**: Guest allocates memory, writes JSON, passes `(ptr, len)`. Host reads only.
- **Response**: Guest allocates a buffer (default 64KB), passes `(ptr, capacity)`. Host writes JSON response into it.
  - Returns bytes written on success.
  - Returns `-2` if response exceeds capacity (guest retries with larger buffer, up to 16MB).
  - Returns `-1` for general errors.
- **Stream chunks**: Same as response, but per-chunk with 4KB default buffer.

### Thread Safety

Host functions may be called from any goroutine. The host must handle concurrent calls safely.

## Operations

Operations are identified by a string key passed as the `op` parameter.

### Lifecycle

| Operation | Request | Response |
|-----------|---------|----------|
| `init` | `{"logLevel": 0, "logTag": ""}` | `{"success": true, "capabilities": ["llm", "stt", ...]}` |
| `shutdown` | `{}` | `{"success": true}` |
| `capabilities` | `{}` | `{"capabilities": ["llm", "stt", "tts", "embeddings", "streaming"]}` |

**Capability strings**: `llm`, `stt`, `tts`, `embeddings`, `streaming`, `gpu`

### LLM

| Operation | Request | Response |
|-----------|---------|----------|
| `llm.create` | `{"modelPath": "path/to/model.gguf", "opts": {...}}` | `{"handle": 1}` |
| `llm.generate` | `{"handle": 1, "prompt": "Hello", "opts": {...}}` | `{"text": "Generated response"}` |
| `llm.generate_stream` | `{"handle": 1, "prompt": "Hello", "opts": {...}}` | (streaming — see below) |
| `llm.close` | `{"handle": 1}` | `{"success": true}` |

### STT (Speech-to-Text)

| Operation | Request | Response |
|-----------|---------|----------|
| `stt.create` | `{"modelPath": "path/to/whisper.bin", "opts": {...}}` | `{"handle": 2}` |
| `stt.transcribe` | `{"handle": 2, "audioData": "<base64>", "opts": {...}}` | `{"text": "Transcribed text"}` |
| `stt.transcribe_stream` | `{"handle": 2, "audioData": "<base64>", "opts": {...}}` | (streaming) |
| `stt.close` | `{"handle": 2}` | `{"success": true}` |

Audio data is base64-encoded PCM bytes.

### TTS (Text-to-Speech)

| Operation | Request | Response |
|-----------|---------|----------|
| `tts.create` | `{"voicePath": "path/to/voice", "opts": {...}}` | `{"handle": 3}` |
| `tts.synthesize` | `{"handle": 3, "text": "Hello world", "opts": {...}}` | `{"audioData": "<base64>"}` |
| `tts.close` | `{"handle": 3}` | `{"success": true}` |

Response audio data is base64-encoded.

### Embeddings

| Operation | Request | Response |
|-----------|---------|----------|
| `emb.create` | `{"modelPath": "path/to/model", "opts": {...}}` | `{"handle": 4}` |
| `emb.embed` | `{"handle": 4, "text": "Hello", "opts": {...}}` | `{"embedding": [0.1, 0.2, ...]}` |
| `emb.embed_batch` | `{"handle": 4, "texts": ["a", "b"], "opts": {...}}` | `{"embeddings": [[0.1, ...], [0.2, ...]]}` |
| `emb.close` | `{"handle": 4}` | `{"success": true}` |

## Streaming Protocol

For streaming operations (`llm.generate_stream`, `stt.transcribe_stream`):

1. Guest calls `call_stream_start` with the operation and request JSON.
2. Host returns a stream handle.
3. Guest repeatedly calls `call_stream_next(stream, chunk_ptr, chunk_cap)`.
4. Each chunk is a JSON object:
   ```json
   {"text": "token", "done": false}
   ```
5. Final chunk:
   ```json
   {"text": "", "done": true}
   ```
6. `call_stream_next` returns `0` when the stream is complete.
7. If the guest closes early, it should call `call_stream_cancel(stream)` to release host resources.

## Error Responses

Any operation may return an error instead of a normal response:

```json
{"error": {"code": "unsupported", "message": "LLM not available"}}
```

### Error Codes

| Code | Description |
|------|-------------|
| `unsupported` | Operation not supported by this host |
| `not_initialized` | Backend not initialized |
| `model_not_found` | Model file not found |
| `model_load_failed` | Model failed to load |
| `out_of_memory` | Insufficient memory |
| `invalid_param` | Invalid request parameter |
| `generation_failed` | Inference failed |
| `timeout` | Operation timed out |
| `cancelled` | Operation was cancelled |

## Reference Implementation

A reference host adapter using [wazero](https://wazero.io/) is provided at:

```
sdk/runanywhere-go/cmd/runanywhere-wasi-host/
```

Run it with:

```bash
# Build the WASI module
cd sdk/runanywhere-go
GOOS=wasip1 GOARCH=wasm go build -o runanywhere.wasm ./cmd/runanywhere-wasm

# Run with reference host (mock backend)
go run ./cmd/runanywhere-wasi-host -module runanywhere.wasm

# Reserved for future proxy mode (currently mock backend only)
go run ./cmd/runanywhere-wasi-host -module runanywhere.wasm -server http://localhost:8080
```

## Implementing a Custom Host

To implement the `runanywhere` host module in your WASI runtime:

1. Register a module named `runanywhere` with the four exported functions.
2. Implement the JSON-based operation dispatch (switch on `op` string).
3. For streaming: maintain a map of stream handles → iterator state.
4. Return capabilities in `init` response based on what your host provides.

### Example: Wasmtime (Rust)

```rust
linker.func_wrap("runanywhere", "call",
    |caller: Caller<'_, _>, op_ptr: i32, op_len: i32,
     req_ptr: i32, req_len: i32, resp_ptr: i32, resp_cap: i32| -> i32 {
        let memory = caller.get_memory("memory").unwrap();
        let op = read_string(&memory, op_ptr as usize, op_len as usize);
        let req = read_bytes(&memory, req_ptr as usize, req_len as usize);
        let resp = handle_operation(&op, &req);
        if resp.len() > resp_cap as usize { return -2; }
        write_bytes(&memory, resp_ptr as usize, &resp);
        resp.len() as i32
    }
)?;
```

### Example: WasmEdge

Similar pattern — register host functions under the `runanywhere` module namespace.

## Deployment

### Kubernetes Sidecar

See [kubernetes-deployment.md](kubernetes-deployment.md) for deploying WASI modules in Kubernetes. The WASI host adapter can run as a sidecar container or as a standalone service.

### Edge Runtimes

The WASI module works with any runtime that implements WASI Preview 1 plus the `runanywhere` host module:

- **Wasmtime** — Production-grade, supports component model
- **WasmEdge** — Optimized for edge/AI workloads, has wasi-nn support
- **wazero** — Pure Go, no CGO dependency
- **Spin/Fermyon** — Serverless WASM platform
- **wasmCloud** — Distributed WASM runtime
