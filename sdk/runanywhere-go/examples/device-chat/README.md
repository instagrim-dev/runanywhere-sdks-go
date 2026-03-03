# device-chat

Minimal example of **on-device LLM inference** using the RunAnywhere Go device package. It initializes the device stack, creates an LLM handle, runs one non-streaming generate and one streaming generate, then shuts down.

## Prerequisites

1. **Build runanywhere-commons shared libraries** for your platform.

   **Linux (x86_64 or aarch64):**
   ```bash
   cd sdk/runanywhere-commons
   ./scripts/build-linux.sh --shared llamacpp
   ```
   Output: `dist/linux/<arch>/` (e.g. `librac_commons.so`, `librac_backend_llamacpp.so`).

   **macOS:**
   ```bash
   cd sdk/runanywhere-commons
   cmake -B build-shared -DCMAKE_BUILD_TYPE=Release \
     -DRAC_BUILD_SHARED=ON -DRAC_BUILD_BACKENDS=ON -DRAC_BACKEND_LLAMACPP=ON
   cmake --build build-shared -j
   ```
   Copy the built `.dylib` files and `include/` into a directory (e.g. `dist/darwin/arm64/`).

2. **GGUF model:** a LlamaCPP-compatible model file (e.g. from Hugging Face).

## Build

From this directory (`examples/device-chat`), set CGO to point at the commons include and lib directories, then build:

```bash
export CGO_ENABLED=1
export RAC_COMMONS_INCLUDE="/path/to/runanywhere-commons/dist/linux/x86_64/include"
export RAC_COMMONS_LIB="/path/to/runanywhere-commons/dist/linux/x86_64"
export CGO_CPPFLAGS="-I${RAC_COMMONS_INCLUDE}"
export CGO_LDFLAGS="-L${RAC_COMMONS_LIB} -lrac_commons -lrac_backend_llamacpp"

go build -o device-chat .
```

Use your actual paths for `RAC_COMMONS_INCLUDE` and `RAC_COMMONS_LIB` (the directory containing `librac_commons.so` and `librac_backend_llamacpp.so`).

## Run

Set the library path so the binary can load the shared libs, then run:

**Linux:**
```bash
export LD_LIBRARY_PATH="/path/to/runanywhere-commons/dist/linux/x86_64:$LD_LIBRARY_PATH"
./device-chat --model=/path/to/model.gguf
```

**macOS:**
```bash
export DYLD_LIBRARY_PATH="/path/to/runanywhere-commons/build-shared/src:...:$DYLD_LIBRARY_PATH"
./device-chat --model=/path/to/model.gguf
```

(Or use `@rpath` / `install_name_tool` so the binary finds the dylibs without `DYLD_LIBRARY_PATH`.)

## Without CGO / without libs

If you build with `CGO_ENABLED=0` or without linking the commons libs, the device package is built as **stubs**. Running the example will exit with a message that on-device inference requires CGO and the shared libraries. The HTTP client in the main SDK still works; only the `device` subpackage is stubbed.
