# embeddings (v2)

Example that calls the Embeddings API (POST /v1/embeddings): Health to check capabilities, then embed a single string or a batch of strings.

## Prerequisites

1. Build and run the server (see main SDK README). For embeddings to work, start the server with an embeddings model:
   ```bash
   runanywhere-server --model /path/to/llm.gguf --embeddings-model /path/to/embeddings-model --port 8080
   ```
   If `--embeddings-model` is omitted, the endpoint returns 501 and this example will report that embeddings are not configured.

## Run

From this directory:

```bash
go run .
```

Options:

- `-url` — server base URL (default `http://127.0.0.1:8080`)
- `-batch` — comma-separated strings to embed as a batch (default: single string `"hello"`)

Examples:

```bash
go run .
go run . -batch "first,second,third"
```
