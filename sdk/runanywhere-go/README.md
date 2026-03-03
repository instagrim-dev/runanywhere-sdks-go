# RunAnywhere Go SDK

Go client for the RunAnywhere OpenAI-compatible HTTP server, with an optional local server launcher.

## Prerequisites

Build and run the **runanywhere-server** binary (from the RunAnywhere Commons C++ stack). From the repo root:

```bash
cmake -S sdk/runanywhere-commons -B build/runanywhere-server \
  -DRAC_BUILD_SERVER=ON \
  -DRAC_BUILD_BACKENDS=ON \
  -DRAC_BACKEND_LLAMACPP=ON \
  -DCMAKE_BUILD_TYPE=Release
cmake --build build/runanywhere-server --target runanywhere-server -j
```

Binary location:

- **Unix**: `build/runanywhere-server/tools/runanywhere-server`
- **Windows**: `build/runanywhere-server/tools/Release/runanywhere-server.exe`

Alternatively, use the script: `sdk/runanywhere-commons/scripts/build-server.sh` (output under `sdk/runanywhere-commons/build-server/`).

Then start the server (example):

```bash
./build/runanywhere-server/tools/runanywhere-server --model /path/to/model.gguf --port 8080
```

## Install

```bash
go get github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go
```

For local development with the example, use a `replace` in the example's `go.mod` (see `examples/basic-chat`).

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go"
)

func main() {
    client := runanywhere.NewClient("http://127.0.0.1:8080", runanywhere.WithTimeout(30*time.Second))

    ctx := context.Background()
    health, err := client.Health(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Status:", health.Status, "Model:", health.Model)
    // health.STTAvailable, health.TTSAvailable, health.EmbeddingsAvailable indicate v2 endpoints

    models, err := client.ListModels(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, m := range models.Data {
        fmt.Println("Model:", m.ID)
    }

    resp, err := client.Chat(ctx, &runanywhere.ChatCompletionRequest{
        Model: models.Data[0].ID,
        Messages: []runanywhere.ChatMessage{
            {Role: "user", Content: "Hello, say hi in one sentence."},
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    if len(resp.Choices) > 0 {
        fmt.Println("Reply:", resp.Choices[0].Message.Content)
    }
}
```

### Tool-call pass-through

Send a request with `tools` and optionally `tool_choice`; the server may return `tool_calls` in the assistant message:

```go
req := &runanywhere.ChatCompletionRequest{
    Model: "my-model",
    Messages: []runanywhere.ChatMessage{{Role: "user", Content: "What is 2+2?"}},
    Tools: []runanywhere.ToolDefinition{{
        Type: "function",
        Function: runanywhere.FunctionDefinition{
            Name:        "add",
            Description: "Add two numbers",
            Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "number"}, "b": map[string]interface{}{"type": "number"}}},
        },
    }},
}
resp, err := client.Chat(ctx, req)
// resp.Choices[0].Message.ToolCalls may contain tool calls from the model.
```

### Streaming

```go
stream, err := client.ChatStream(ctx, &runanywhere.ChatCompletionRequest{
    Model:    "my-model",
    Messages: []runanywhere.ChatMessage{{Role: "user", Content: "Hello"}},
})
if err != nil {
    log.Fatal(err)
}
defer stream.Close()
for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    for _, c := range chunk.Choices {
        if c.Delta.Content != "" {
            fmt.Print(c.Delta.Content)
        }
    }
}
```

### Transcribe (v2)

Requires the server to be started with `--stt-model /path/to/stt-model` (otherwise the endpoint returns 501).

```go
f, _ := os.Open("audio.wav")
defer f.Close()
resp, err := client.Transcribe(ctx, f, "audio.wav", &runanywhere.TranscribeOptions{Language: "en"})
// resp.Text contains the transcript
```

### Speech (v2)

Requires the server to be started with `--tts-model` (otherwise 501).

```go
audio, err := client.Speech(ctx, &runanywhere.SpeechRequest{Model: "tts", Input: "Hello world"})
// audio is raw bytes (e.g. WAV)
```

### Embeddings (v2)

Requires the server to be started with `--embeddings-model` (otherwise 501).

```go
resp, err := client.Embeddings(ctx, &runanywhere.EmbeddingsRequest{Model: "embed", Input: "hello"})
// resp.Data[0].Embedding is []float32
```

### Server launcher

To start the server from Go (e.g. in tests or examples), use the optional launcher in `server.go`:

```go
launcher := &runanywhere.ServerLauncher{
    Path:       runanywhere.RunAnywhereServerPath(), // or set RAC_SERVER_PATH
    ModelPath:  "/path/to/model.gguf",
    Port:       8080,
}
ctx := context.Background()
if err := launcher.Start(ctx); err != nil {
    log.Fatal(err)
}
defer launcher.Stop()
if err := launcher.WaitReady(ctx, "http://127.0.0.1:8080"); err != nil {
    log.Fatal(err)
}
// use client...
```

The primary use case is still running the server manually or via your own script; the launcher is for convenience in examples and local dev.

## On-device inference (Go)

The **device** subpackage (`runanywhere/device`) runs LLM (and optionally STT/TTS/embeddings) **inside the Go process** by binding to runanywhere-commons shared libraries. Use it when you want local inference without the HTTP server.

**Prerequisites:** CGo enabled, C toolchain, and runanywhere-commons built as **shared libraries** for your platform.

### Building shared libraries for the device package

Build the commons as shared libs so the Go device package can link against them.

**Linux (x86_64 or aarch64):**

```bash
cd sdk/runanywhere-commons
./scripts/build-linux.sh --shared llamacpp   # LLM only
# or
./scripts/build-linux.sh --shared all        # LLM + ONNX (STT/TTS)
```

Output: `sdk/runanywhere-commons/dist/linux/<arch>/` (e.g. `librac_commons.so`, `librac_backend_llamacpp.so`). Headers are under `dist/linux/<arch>/include/`.

**macOS (darwin/arm64 or amd64):**

```bash
cd sdk/runanywhere-commons
cmake -B build-shared -DCMAKE_BUILD_TYPE=Release \
  -DRAC_BUILD_SHARED=ON \
  -DRAC_BUILD_BACKENDS=ON \
  -DRAC_BACKEND_LLAMACPP=ON
cmake --build build-shared -j
```

Copy the `.dylib` files and `include/` from the build tree into a dist directory (e.g. `dist/darwin/<arch>/`) so you can set `RAC_COMMONS_LIB` and `RAC_COMMONS_INCLUDE` to that path.

### Building the Go package with device support

Set CGO to use the commons include and lib directories, then build:

```bash
export CGO_ENABLED=1
export RAC_COMMONS_INCLUDE="/path/to/runanywhere-commons/dist/linux/x86_64/include"   # or your dist/include
export RAC_COMMONS_LIB="/path/to/runanywhere-commons/dist/linux/x86_64"               # or your dist dir
export CGO_CPPFLAGS="-I${RAC_COMMONS_INCLUDE}"
export CGO_LDFLAGS="-L${RAC_COMMONS_LIB} -lrac_commons -lrac_backend_llamacpp"

go build ./...
```

For **LLM-only** you only need `librac_commons` and `librac_backend_llamacpp`. For STT/TTS add `-lrac_backend_onnx` and ensure the ONNX backend was built.

### Runtime (loading shared libs)

- **Linux:** `export LD_LIBRARY_PATH=/path/to/dist/linux/x86_64:$LD_LIBRARY_PATH` (or your dist dir) before running the Go binary.
- **macOS:** Prefer `@rpath` or `install_name_tool` so the binary finds the dylibs; otherwise `export DYLD_LIBRARY_PATH=/path/to/dist:$DYLD_LIBRARY_PATH` (note: SIP may strip `DYLD_LIBRARY_PATH` in some launch contexts).

### Usage (device package)

```go
import (
    "context"
    "fmt"
    "io"
    "log"

    "github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go/device"
)

func main() {
    ctx := context.Background()
    if err := device.Init(ctx); err != nil {
        log.Fatal(err)
    }
    defer device.Shutdown()

    llm, err := device.NewLLM(ctx, "/path/to/model.gguf", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer llm.Close()

    // Non-streaming
    out, err := llm.Generate(ctx, "Hello in one sentence.", nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(out)

    // Streaming
    it, err := llm.GenerateStream(ctx, "Count to 3.", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer it.Close()
    for {
        token, isFinal, err := it.Next()
        if err == io.EOF {
            break
        }
        if err != nil {
            log.Fatal(err)
        }
        fmt.Print(token)
        if isFinal {
            break
        }
    }
}
```

**Lifecycle:** Call `device.Init()` (or `device.InitWithConfig()`) once before creating any handle. Call `Close()` on every LLM/STT/TTS/Embeddings handle when done. Call `device.Shutdown()` when shutting down; it returns an error if any handle is still open.

**Without CGO:** If you build with `CGO_ENABLED=0` or do not link the shared libs, the device package builds as **stubs**: `Init`, `NewLLM`, etc. return `device.ErrUnsupported`. The rest of the SDK (HTTP client) works as usual.

See `examples/device-chat` for a runnable example.

## Scope

- **v1**: `ListModels`, `Chat`, `ChatStream`, `Health`, and tool-call pass-through. HTTP client only; no cgo.
- **v2**: `Transcribe` (POST /v1/audio/transcriptions), `Speech` (POST /v1/audio/speech), `Embeddings` (POST /v1/embeddings). The server must be built and configured with optional STT/TTS/embeddings model paths (e.g. `--stt-model`, `--tts-model`, `--embeddings-model`); otherwise those endpoints return 501.
- **v3**: GET /health includes `stt_available`, `tts_available`, `embeddings_available` so clients can avoid calling disabled endpoints. **HealthResponse** and **WithTimeout** option.
- **Device (on-device inference):** Subpackage `runanywhere/device` for in-process LLM (and optionally STT/TTS/embeddings) via CGo and runanywhere-commons shared libs. Requires `CGO_ENABLED=1` and built shared libraries; see "On-device inference (Go)" above.
- **Out of scope**: VAD over HTTP.

## API

- **NewClient(baseURL string, opts ...ClientOption) \*Client** — options: **WithHTTPClient**, **WithAPIKey**, **WithTimeout(d)** (request timeout; default no timeout)
- **Client.ListModels(ctx) (\*ModelsResponse, error)**
- **Client.Health(ctx) (\*HealthResponse, error)**
- **Client.Chat(ctx, req) (\*ChatCompletionResponse, error)**
- **Client.ChatStream(ctx, req) (\*ChatStreamReader, error)** — caller must call **Close()** when done
- **Client.Transcribe(ctx, audioReader, filename, opts) (\*TranscriptionResponse, error)** — v2
- **Client.Speech(ctx, req \*SpeechRequest) ([]byte, error)** — v2
- **Client.Embeddings(ctx, req \*EmbeddingsRequest) (\*EmbeddingsResponse, error)** — v2
- **ChatStreamReader.Next() (\*StreamChunk, error)** — returns **io.EOF** after the last chunk or `[DONE]`

Types mirror the server JSON (OpenAI-style); see `types.go`.
