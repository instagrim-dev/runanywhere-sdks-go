# basic-chat

Minimal example: use the RunAnywhere Go client to call Health, ListModels, and Chat (or ChatStream) against a local runanywhere-server.

## 1. Build the server

From the **repository root**:

```bash
cmake -S sdk/runanywhere-commons -B build/runanywhere-server \
  -DRAC_BUILD_SERVER=ON \
  -DRAC_BUILD_BACKENDS=ON \
  -DRAC_BACKEND_LLAMACPP=ON \
  -DCMAKE_BUILD_TYPE=Release
cmake --build build/runanywhere-server --target runanywhere-server -j
```

Binary: `build/runanywhere-server/tools/runanywhere-server` (Unix) or `build/runanywhere-server/tools/Release/runanywhere-server.exe` (Windows).

## 2. Run the server

In a separate terminal:

```bash
./build/runanywhere-server/tools/runanywhere-server --model /path/to/your/model.gguf --port 8080
```

## 3. Run the example

From this directory (`sdk/runanywhere-go/examples/basic-chat/`):

```bash
go run .
```

Options:

- `-url=http://127.0.0.1:8080` — server base URL (default)
- `-stream` — use streaming chat
- `-tools` — send a request with a tool definition and print any tool_calls in the response

Example with streaming:

```bash
go run . -stream
```

Example with tool-call pass-through:

```bash
go run . -tools
```
