# Deep Source Code Review: Go SDK

## Summary

The Go SDK is well-structured with a clean two-tier design (HTTP client + on-device CGO). The HTTP types align correctly with the server's JSON responses. However, I found **4 bugs**, **6 completeness gaps**, **5 consistency issues**, and **4 minor quality issues**.

---

## 1. BUGS (Correctness)

### 1.1 Transcribe sends `language` twice (form field + query param)

**File:** `client.go:136-156`

The language is sent both as a multipart form field (line 141) AND as a URL query parameter (lines 154-156):

```go
// First: form field
_ = w.WriteField("language", opts.Language)   // line 141

// Then again: query parameter
if opts != nil && opts.Language != "" {
    u = u + "?language=" + url.QueryEscape(opts.Language)   // lines 154-156
}
```

This double-send is redundant and could confuse servers. The OpenAI API expects it only in the form body.

### 1.2 `ErrorResponse.Code` type mismatch — silent data loss

**File:** `types.go:141`

The `Code` field is `string`, but the C++ server sends it as a JSON integer (`"code": 400`). Go's `json.Decoder` cannot unmarshal a JSON number into a string. The `decodeError` method at `client.go:298` swallows the error (`_ = json.NewDecoder(resp.Body).Decode(&errBody)`), so the code field is silently dropped. Not a crash, but the error code is lost.

### 1.3 LLM stream callback has no-op `select`/`default` pattern

**File:** `llm_cgo.go:228-233`

```go
select {
case v.ch <- streamChunk{token: tok, isFinal: isFinal != 0}:
default:
    // Channel full; backpressure by blocking is acceptable per plan
    v.ch <- streamChunk{token: tok, isFinal: isFinal != 0}
}
```

The `select` with `default` fallthrough to a blocking send is functionally identical to a plain blocking send. The `select` achieves nothing. Worse, the comment says "backpressure by blocking" but the intent is unclear — contrast with the STT callback (`stt_cgo.go:203-207`) which intentionally **drops** chunks when the channel is full. This inconsistency means one stream drops data silently while the other blocks the C thread.

### 1.4 `Close()` methods are not thread-safe (potential double-free)

**Files:** `llm_cgo.go:238-247`, `stt_cgo.go:211-220`, `tts_cgo.go:82-90`, `embeddings_cgo.go:136-144`

All `Close()` methods check `handle == nil` then set `handle = nil` without synchronization:

```go
func (l *LLM) Close() error {
    if l.handle == nil { return nil }  // TOCTOU race
    l.streamWg.Wait()
    C.rac_llm_llamacpp_destroy(l.handle)
    l.handle = nil
    decrementHandleCount()
    return nil
}
```

Two goroutines calling `Close()` concurrently can both pass the nil check and call the C destroy function twice (double-free / use-after-free). The same race exists between `Generate()`/`Close()`.

### 1.5 Server.Stop comment says SIGTERM but code sends SIGINT

**File:** `server.go:125-130`

```go
// Stop stops the server process (SIGTERM then SIGKILL if needed).   // <-- doc says SIGTERM
func (l *ServerLauncher) Stop() error {
    ...
    if err := l.cmd.Process.Signal(os.Interrupt); err != nil {       // <-- os.Interrupt = SIGINT
```

`os.Interrupt` maps to SIGINT (Ctrl+C), not SIGTERM. The behavior and the documentation disagree.

---

## 2. COMPLETENESS GAPS

### 2.1 `SpeechRequest` missing `Speed` and `ResponseFormat` fields

**File:** `types.go:162-166`

The server's TTS handler parses `speed` and `response_format` from the request body, but the Go type only has `Model`, `Input`, `Voice`:

```go
type SpeechRequest struct {
    Model  string `json:"model"`
    Input  string `json:"input"`
    Voice  string `json:"voice,omitempty"`
    // Missing: Speed float32 `json:"speed,omitempty"`
    // Missing: ResponseFormat string `json:"response_format,omitempty"`
}
```

### 2.2 Constructor `opts` parameters are accepted but ignored

**Files:** `llm_cgo.go:39`, `stt_cgo.go:38`, `tts_cgo.go:28`, `embeddings_cgo.go:28`

All `New*` functions accept an options parameter but discard it:

- `NewLLM(ctx, modelPath, opts *LLMOptions)` — ignores opts, passes NULL config to C
- `NewSTT(ctx, modelPath, opts *STTOptions)` — ignores opts
- `NewTTS(ctx, voicePath, opts *TTSOptions)` — ignores opts
- `NewEmbeddings(ctx, modelPath, opts *EmbedOptions)` — ignores opts

The C API's `rac_llm_llamacpp_config_t` supports `context_size`, `num_threads`, `gpu_layers`, `batch_size`, but none are exposed. Either use the parameter or remove it from the signature.

### 2.3 `Config.RegisterONNX` docs contradict behavior

**File:** `device/types.go:14-18`

```go
// RegisterONNX, if true, registers the ONNX backend (STT/TTS). If false,
// only the LlamaCPP backend is registered (LLM only). Default (false) for
// Init() is to register all available backends; ...
```

But `Init()` calls `InitWithConfig(ctx, nil)`, and with nil config, `RegisterONNX` is false (`init_cgo.go:159`), so **Init() only registers LlamaCPP**. The doc says "register all available backends" but the code does the opposite.

### 2.4 `go vet` reports 3 unsafe.Pointer violations

**Files:** `init_cgo.go:135`, `llm_cgo.go:191`, `stt_cgo.go:177`

All are `unsafe.Pointer(uintptr(handle))` used to pass `cgo.Handle` as C `void*`. While functionally necessary, `go vet` flags them as violations of Go's unsafe.Pointer rules. The canonical workaround is to use `C.uintptr_t` or store the handle value differently.

### 2.5 `EmbeddingsResponse` missing `Object` field

**File:** `types.go:181-185`

The server sends `"object": "list"` in embeddings responses and `"object": "embedding"` in each data entry. Neither is captured in the Go types. Minor but breaks full round-trip fidelity.

### 2.6 Empty `init()` in stub build

**File:** `device/init_stub.go:7-9`

```go
func init() {
    // Stub build: no CGO. All Init/Shutdown/New* will return ErrUnsupported.
}
```

This is dead code — it does nothing and should be removed.

---

## 3. CONSISTENCY ISSUES

### 3.1 Stream iterator concurrency patterns differ across handles

| Handle | Closed field | Cancel support | Channel-full behavior |
|--------|-------------|----------------|----------------------|
| LLM | `atomic.Bool` | Yes (`cancelFn`, `cancelOnce`, `cancelled`) | Tries non-blocking, then blocks |
| STT | `sync.Mutex` + `bool` | No | Drops chunk (non-blocking only) |
| TTS | N/A (not implemented) | N/A | N/A |

Three different concurrency patterns for the same conceptual operation. The LLM iterator is the most mature (supports cancellation), but STT uses a simpler but inconsistent approach.

### 3.2 Error wrapping inconsistency

The `device/errors.go` properly defines sentinel errors. But CGO failures use `fmt.Errorf("rac_X failed: %d", res)` which creates unwrappable errors. These error codes map to the comprehensive `rac_error.h` error code system (100+ codes) but no translation is done. Callers cannot use `errors.Is` to check for specific failure reasons.

### 3.3 Package doc placement

Root package doc is in `types.go:1`. Device package doc is in `errors.go:1-4`. Conventionally, Go uses a `doc.go` file for this.

### 3.4 Test handlers set headers after WriteHeader

**File:** `client_test.go:143-145`, and same pattern in lines 232-234, 284-286, 306-308

```go
w.WriteHeader(http.StatusBadRequest)
w.Header().Set("Content-Type", "application/json")  // too late, headers already sent
```

In Go's `net/http`, headers must be set **before** `WriteHeader()`. After `WriteHeader()`, `Header().Set()` has no effect. These tests still pass because the SDK doesn't check `Content-Type`, but the test code is incorrect.

### 3.5 `SpeechRequest` nil check missing

`Speech()` at `client.go:181` doesn't nil-check the request, unlike `Chat()`/`ChatStream()` which check for nil and return `errNilRequest`. If `req` is nil, `json.Marshal(nil)` produces `"null"`, and the server likely returns 400.

---

## 4. MINOR QUALITY ISSUES

### 4.1 `WaitReady` has no default timeout

**File:** `server.go:149-167`

`WaitReady` polls forever if the context has no deadline. Callers using `context.Background()` will hang indefinitely if the server never starts.

### 4.2 Embeddings example `splitBatch` has dead code

**File:** `examples/embeddings/main.go:90-92`

```go
if start < len(s) {
    out = append(out, s[start:])
}
```

After the loop, `start` is always `>= len(s)`, so this branch is unreachable.

### 4.3 `ChatCompletionRequest` field alignment

**File:** `types.go:58-71`

`Messages` has 8 spaces of indent while `FrequencyPenalty` uses 1 space before the tag. This is cosmetic but the field alignment is inconsistent (some fields have extra whitespace before their json tags, others don't).

### 4.4 Device CGO tests can't run without shared libs

The `device_test.go` (no build tag) only tests error sentinels. There are no integration-level CGO tests. When CGO is enabled, the device package attempts to link against `rac_commons`/`rac_backend_*` which fails without the shared libraries, causing `go test ./...` to fail even for the sentinel tests that don't need CGO.

---

## Verdict

The SDK is functional and well-designed at the architectural level. The HTTP client layer is solid — types match the server, tests are comprehensive, and the streaming implementation handles SSE correctly. The device CGO layer shows good engineering (handle lifecycle, stream callbacks, build tags) but has thread-safety gaps in `Close()` methods and inconsistent patterns across handles. The most impactful issues to fix are:

1. **Thread-safe Close()** — add a mutex or `sync.Once` to prevent double-free
2. **Transcribe double-language** — remove the query parameter append
3. **Config.RegisterONNX doc fix** — either fix the doc or fix the default behavior
4. **LLM stream callback** — choose one strategy (drop or block) and use it consistently
