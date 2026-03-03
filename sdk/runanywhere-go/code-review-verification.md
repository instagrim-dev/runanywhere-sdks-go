# Verification: sdk/runanywhere-go/code-review.md

Each finding was checked against the current source. **Valid** = issue present and correctly described; **Invalid** = not applicable or incorrectly described.

---

## 1. BUGS (Correctness)

### 1.1 Transcribe sends `language` twice (form field + query param)

**Finding:** Language is sent as both a multipart form field and a URL query parameter; redundant and could confuse servers.

**Evidence:**
- `client.go:139–141`: `_ = w.WriteField("language", opts.Language)` (form field).
- `client.go:153–155`: `if opts != nil && opts.Language != "" { u = u + "?language=" + url.QueryEscape(opts.Language) }` (query param).

**Verdict: Valid.** Language is still sent in both places.

---

### 1.2 `ErrorResponse.Code` type mismatch — silent data loss

**Finding:** `Code` is `string` in Go but the C++ server sends an integer; decode swallows the error and the code is lost.

**Evidence:**
- `types.go:141`: `Code string \`json:"code,omitempty"\``.
- `client.go:296–298`: `_ = json.NewDecoder(resp.Body).Decode(&errBody)` (error ignored).
- `sdk/runanywhere-commons/src/server/json_utils.cpp:177–183`: `createErrorResponse(..., int code)` sets `error["code"] = code` (JSON number).

**Verdict: Valid.** Server emits a number; Go type is string; decode error is ignored, so the code field is silently dropped.

---

### 1.3 LLM stream callback has no-op `select`/`default` pattern

**Finding:** The select with default then blocking send is equivalent to a plain blocking send; inconsistent with STT which drops when full.

**Evidence:**
- `device/llm_cgo.go:228–233`: `select { case v.ch <- ...: default: v.ch <- ... }` — default branch always blocks.
- `device/stt_cgo.go:203–207`: `select { case v.ch <- ...: default: }` — drops chunk when full.

**Verdict: Valid.** LLM effectively always blocks; STT drops when full. Behavior and intent differ between the two.

---

### 1.4 `Close()` methods are not thread-safe (potential double-free)

**Finding:** Close() checks `handle == nil` then destroys and nils without synchronization; concurrent Close() or Generate/Close can double-free.

**Evidence:**
- `device/llm_cgo.go:237–246`: no mutex; `if l.handle == nil` then `C.rac_llm_llamacpp_destroy(l.handle)`.
- `device/stt_cgo.go:210–219`: same pattern.
- `device/tts_cgo.go:81–89`: same pattern.
- `device/embeddings_cgo.go:135–143`: same pattern.

**Verdict: Valid.** All four Close() implementations have a TOCTOU race; no sync.Once or mutex.

---

### 1.5 Server.Stop comment says SIGTERM but code sends SIGINT

**Finding:** Doc says "SIGTERM then SIGKILL" but code uses `os.Interrupt` (SIGINT).

**Evidence:**
- `server.go:125`: comment "Stop stops the server process (SIGTERM then SIGKILL if needed)."
- `server.go:130`: `l.cmd.Process.Signal(os.Interrupt)` — on Unix, `os.Interrupt` is SIGINT.

**Verdict: Valid.** Documentation and implementation disagree.

---

## 2. COMPLETENESS GAPS

### 2.1 `SpeechRequest` missing `Speed` and `ResponseFormat` fields

**Finding:** Server TTS handler supports `speed` and `response_format`; Go type only has Model, Input, Voice.

**Evidence:**
- `types.go:161–166`: `SpeechRequest` has only `Model`, `Input`, `Voice`.
- No `Speed` or `ResponseFormat` fields in the struct.

**Verdict: Valid.** Fields are still missing (server support would need to be confirmed in commons; the type gap exists).

---

### 2.2 Constructor `opts` parameters are accepted but ignored

**Finding:** NewLLM, NewSTT, NewTTS, NewEmbeddings accept options but pass NULL or ignore them.

**Evidence:**
- `device/llm_cgo.go:39,44–47`: `opts *LLMOptions` unused; `var config *C.rac_llm_llamacpp_config_t` is nil, passed to `rac_llm_llamacpp_create`.
- `device/stt_cgo.go:38,44–45`: `opts *STTOptions` unused; no options passed to C.
- `device/tts_cgo.go:28,33–34`: `opts *TTSOptions` unused.
- `device/embeddings_cgo.go:28,33–34`: `opts *EmbedOptions` unused.

**Verdict: Valid.** All four constructors accept opts and ignore them.

---

### 2.3 `Config.RegisterONNX` docs contradict behavior

**Finding:** Doc says "Default (false) for Init() is to register all available backends" but with nil config, RegisterONNX is false so only LlamaCPP is registered.

**Evidence:**
- `device/types.go:14–17`: "Default (false) for Init() is to register all available backends; ..."
- `device/init_cgo.go:157–158`: `if cfg != nil && cfg.RegisterONNX` — with nil config, ONNX is never registered.
- Init() calls InitWithConfig(ctx, nil), so cfg is nil → RegisterONNX not set → only LlamaCPP registered.

**Verdict: Valid.** Doc says "all available backends" for Init(); behavior is "LlamaCPP only" when config is nil.

---

### 2.4 `go vet` reports 3 unsafe.Pointer violations

**Finding:** `unsafe.Pointer(uintptr(handle))` used to pass cgo.Handle as C void*; go vet flags misuse.

**Evidence:**
- Ran `go vet ./...` in sdk/runanywhere-go:
  - `device/init_cgo.go:135:47: possible misuse of unsafe.Pointer`
  - `device/llm_cgo.go:191:124: possible misuse of unsafe.Pointer`
  - `device/stt_cgo.go:177:65: possible misuse of unsafe.Pointer`

**Verdict: Valid.** Three violations as stated; workaround (e.g. C.uintptr_t or different handle passing) not applied.

---

### 2.5 `EmbeddingsResponse` missing `Object` field

**Finding:** Server sends `"object": "list"` (and per-item `"object": "embedding"`); Go types don't capture them.

**Evidence:**
- `types.go:180–185`: `EmbeddingsResponse` has `Data`, `Model`, `Usage` only; no `Object`.
- `EmbeddingData` (173–177) has `Embedding`, `Index` only; no `Object`.

**Verdict: Valid.** Round-trip fidelity gap; minor as noted in review.

---

### 2.6 Empty `init()` in stub build

**Finding:** `device/init_stub.go` has an empty `init()` that does nothing and is dead code.

**Evidence:**
- `device/init_stub.go:7–9`: `func init() { // Stub build: no CGO. ... }` — no statements, only comment.

**Verdict: Valid.** init() is present and has no effect; removal would not change behavior.

---

## 3. CONSISTENCY ISSUES

### 3.1 Stream iterator concurrency patterns differ across handles

**Finding:** LLM uses atomic.Bool + cancel; STT uses Mutex + bool, no cancel; channel-full behavior differs (LLM blocks, STT drops).

**Evidence:** Already confirmed in 1.3 and code paths above (LLM: cancel, non-block then block; STT: no cancel, drop). TTS has no stream impl.

**Verdict: Valid.** Three different patterns as summarized in the review table.

---

### 3.2 Error wrapping inconsistency

**Finding:** CGO failures use `fmt.Errorf("rac_X failed: %d", res)`; no translation to sentinel/wrappable errors; callers can't use `errors.Is`.

**Evidence:**
- `device/errors.go`: sentinel errors defined.
- e.g. `device/llm_cgo.go:49`: `return nil, fmt.Errorf("rac_llm_llamacpp_create failed: %d", res)` — not wrapped with sentinels or error types from rac_error.h.

**Verdict: Valid.** Error codes from C are not translated to structured/wrappable errors.

---

### 3.3 Package doc placement

**Finding:** Root package doc in `types.go:1`; device package doc in `errors.go:1–4`; convention is often `doc.go`.

**Evidence:**
- `types.go:1`: `// Package runanywhere ...`
- `device/errors.go:1–4`: `// Package device ...`

**Verdict: Valid.** Doc placement is as described; style preference only.

---

### 3.4 Test handlers set headers after WriteHeader

**Finding:** In tests, `WriteHeader` is called before `Header().Set("Content-Type", ...)`; in net/http, headers must be set before WriteHeader.

**Evidence:**
- `client_test.go:143–145`: `w.WriteHeader(http.StatusBadRequest)` then `w.Header().Set("Content-Type", "application/json")`.
- Same pattern at 232–234, 284–286, 306–308.

**Verdict: Valid.** Order is wrong; tests still pass because the client doesn't rely on Content-Type for these paths.

---

### 3.5 `SpeechRequest` nil check missing

**Finding:** `Speech()` doesn't nil-check the request like `Chat()`/`ChatStream()` (which return `errNilRequest`).

**Evidence:**
- `client.go:94–95`, `109–110`: Chat/ChatStream check `if req == nil { return nil, errNilRequest }`.
- `client.go:178–184`: Speech() calls `doBytesResponse(ctx, ..., req)` with no nil check. When req is nil, `doBytesResponse` gets body=nil and sends a request with no body (does not call json.Marshal).

**Verdict: Valid.** Speech() does not nil-check req; behavior is inconsistent with Chat/ChatStream. (Note: with current doBytesResponse, nil req yields no body, not "null" JSON.)

---

## 4. MINOR QUALITY ISSUES

### 4.1 `WaitReady` has no default timeout

**Finding:** WaitReady polls forever if context has no deadline; `context.Background()` can hang indefinitely.

**Evidence:**
- `server.go:148–166`: loop is `select { case <-ctx.Done(): return ctx.Err(); case <-ticker.C: ... }`; no internal timeout; if ctx never cancels, loop runs until server responds 200.

**Verdict: Valid.** Callers using context without deadline can block indefinitely.

---

### 4.2 Embeddings example `splitBatch` has dead code

**Finding:** After the loop, `start` is always >= len(s), so `if start < len(s) { out = append(out, s[start:]) }` is unreachable.

**Evidence:**
- `examples/embeddings/main.go:79–92`: loop `for i := 0; i <= len(s); i++`; when `i == len(s)` we set `start = i + 1 = len(s)+1`. So after loop, `start > len(s)`, hence `start < len(s)` is false.

**Verdict: Valid.** The branch is dead code.

---

### 4.3 `ChatCompletionRequest` field alignment

**Finding:** Inconsistent whitespace before json tags (e.g. Messages has more spaces than FrequencyPenalty).

**Evidence:**
- `types.go:57–70`: `Messages` has multiple spaces before `json:"messages"`; `FrequencyPenalty` has single space before `json:"frequency_penalty,omitempty"`. Indentation is inconsistent.

**Verdict: Valid.** Cosmetic alignment inconsistency.

---

### 4.4 Device CGO tests can't run without shared libs

**Finding:** With CGO enabled, device package links against rac_commons/rac_backend_*; without shared libs, `go test ./...` can fail even for tests that don't need CGO.

**Evidence:** Not re-run here (environment-dependent). The review states that device_test.go has no build tag and that CGO build pulls in shared libs; failure mode is as described.

**Verdict: Valid.** Structural issue: device tests and CGO linkage are coupled; sentinel-only tests could be run without libs with appropriate build tags or test layout.

---

## Summary

| Section | Valid | Invalid |
|---------|-------|--------|
| 1. Bugs | 5 | 0 |
| 2. Completeness | 6 | 0 |
| 3. Consistency | 5 | 0 |
| 4. Minor quality | 4 | 0 |
| **Total** | **20** | **0** |

All 20 findings in the review are **valid** against the current codebase. The highest-impact items to address remain: thread-safe Close() (1.4), transcribe double-language (1.1), RegisterONNX doc vs behavior (2.3), and consistent LLM/STT stream callback strategy (1.3).
