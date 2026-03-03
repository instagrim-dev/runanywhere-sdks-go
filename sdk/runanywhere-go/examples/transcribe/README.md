# transcribe (v2)

Example that calls the Transcribe API (POST /v1/audio/transcriptions) to send an audio file and print the transcript.

## Prerequisites

1. Build and run the server (see main SDK README). For transcriptions to work, start the server with an STT model:
   ```bash
   runanywhere-server --model /path/to/llm.gguf --stt-model /path/to/stt-model --port 8080
   ```
   If `--stt-model` is omitted, the endpoint returns 501 and this example will report that transcriptions are not configured.

2. Have an audio file (e.g. WAV) to transcribe.

## Run

From this directory:

```bash
go run . -audio /path/to/audio.wav
```

Options:

- `-url` — server base URL (default `http://127.0.0.1:8080`)
- `-audio` — path to audio file (required)
- `-lang` — optional language code (e.g. `en`)

Example:

```bash
go run . -audio sample.wav -lang en
```
