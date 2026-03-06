# Deploying the Go SDK (CGO/Device) to Kubernetes

The CGO "device" backend runs in-process inference via `runanywhere-commons` shared libraries. It works the same in a container as on bare metal — no special SDK changes are needed. The considerations below are operational.

---

## Container Image

Use a **glibc-based** base image (e.g. `debian:bookworm-slim`), **not** Alpine (musl). CGO binaries link against glibc and will segfault or fail to start on musl-based images.

Copy the shared libraries into the image and set `LD_LIBRARY_PATH`:

```dockerfile
FROM debian:bookworm-slim

# Install minimal runtime deps
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy shared libs
COPY dist/linux/x86_64/librac_commons.so /usr/local/lib/
COPY dist/linux/x86_64/librac_backend_llamacpp.so /usr/local/lib/
# Optional: COPY dist/linux/x86_64/librac_backend_onnx.so /usr/local/lib/

ENV LD_LIBRARY_PATH=/usr/local/lib

# Copy Go binary
COPY myapp /usr/local/bin/myapp

ENTRYPOINT ["myapp"]
```

Build the Go binary for Linux:

```bash
CGO_ENABLED=1 \
CGO_CPPFLAGS="-I${RAC_COMMONS_INCLUDE}" \
CGO_LDFLAGS="-L${RAC_COMMONS_LIB} -lrac_commons -lrac_backend_llamacpp" \
GOOS=linux GOARCH=amd64 \
go build -o myapp ./cmd/myapp
```

---

## Model Files

The `device.NewLLM(ctx, modelPath, nil)` path must resolve inside the container. Options:

| Approach | Pros | Cons |
|---|---|---|
| **PersistentVolumeClaim** | Shared across pods, survives restarts | Requires PV provisioner, slower first-load |
| **Init container download** | Fresh models per deploy, no baked images | Startup latency, needs network access |
| **Baked into image** | Fastest cold start, fully self-contained | Large images (multi-GB), slow CI builds |
| **S3/GCS + local cache** | Flexible, shared source of truth | Requires cache management, startup latency |

Example with PVC:

```yaml
volumes:
  - name: models
    persistentVolumeClaim:
      claimName: runanywhere-models

containers:
  - name: app
    volumeMounts:
      - name: models
        mountPath: /models
    env:
      - name: MODEL_PATH
        value: /models/llama-7b.gguf
```

---

## Resource Limits

LLM inference is memory- and CPU-heavy. Set resource requests generously to avoid OOM kills (the #1 failure mode).

```yaml
resources:
  requests:
    memory: "8Gi"    # 7B GGUF model needs ~4-8 GB
    cpu: "4"         # Match llamacpp thread count
  limits:
    memory: "12Gi"
    cpu: "8"
```

Guidelines:
- **Memory**: A 7B-parameter GGUF model typically needs 4–8 GB RAM depending on quantization. 13B models need 8–16 GB.
- **CPU**: The llamacpp backend uses multiple threads. Set CPU requests to match the expected thread count for predictable performance.
- **No swap**: Kubernetes doesn't use swap by default — if the model doesn't fit in the memory limit, the pod gets OOM-killed.

---

## GPU (Optional)

If cluster nodes have GPUs, use the [NVIDIA device plugin](https://github.com/NVIDIA/k8s-device-plugin) and request GPU resources. The llamacpp backend picks up CUDA automatically when available.

```yaml
resources:
  limits:
    nvidia.com/gpu: 1
```

Without GPU resources requested, inference runs CPU-only — still functional, just slower.

---

## No "Device Detection" Concern

Unlike mobile SDKs where "device" implies hardware discovery, the Go SDK's `device.Init(ctx)` simply initializes the C runtime and registers backends. It does not probe for mobile hardware. In Kubernetes it behaves identically to any Linux process:

```go
// Works the same in a container as on bare metal
if err := device.Init(ctx); err != nil {
    log.Fatal(err)
}
defer device.Shutdown()

llm, err := device.NewLLM(ctx, os.Getenv("MODEL_PATH"), nil)
if err != nil {
    log.Fatal(err)
}
defer llm.Close()
```

---

## Health Checks

Use the SDK's initialization state for liveness/readiness probes:

```go
http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
    // device.Init succeeded and model is loaded
    if llm != nil {
        w.WriteHeader(http.StatusOK)
        return
    }
    w.WriteHeader(http.StatusServiceUnavailable)
})
```

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 30   # Model loading can take time
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 5
```

Set `initialDelaySeconds` high enough for model loading — large models can take 10–30+ seconds to load from disk.

---

## Alternative: WASI Sidecar Deployment

Instead of CGO in-process inference, you can deploy the Go SDK compiled to WASI (`GOOS=wasip1 GOARCH=wasm`) with a WASI host runtime as a sidecar. This avoids CGO/glibc dependencies entirely.

### Architecture

```
┌─────────────────────────────────────────┐
│ Pod                                     │
│  ┌──────────────┐  ┌─────────────────┐  │
│  │  App (WASI)  │  │  WASI Host      │  │
│  │  .wasm module│──│  (wazero/       │  │
│  │              │  │   wasmtime)     │  │
│  └──────────────┘  └────────┬────────┘  │
│                             │            │
│                    ┌────────▼────────┐   │
│                    │  Models Volume  │   │
│                    └─────────────────┘   │
└─────────────────────────────────────────┘
```

### When to Use WASI vs CGO

| Factor | CGO (in-process) | WASI (sidecar) |
|--------|-------------------|----------------|
| **Performance** | Best (native) | ~10-20% overhead |
| **Image size** | Larger (glibc + .so files) | Smaller (.wasm module) |
| **Sandboxing** | Process-level only | WASM sandbox (memory-safe) |
| **Portability** | Linux/glibc only | Any WASI runtime |
| **GPU access** | Direct CUDA/Metal | Host-mediated |
| **Startup** | Faster | Slightly slower (WASM compilation) |

### Build the WASI Module

```bash
cd sdk/runanywhere-go
GOOS=wasip1 GOARCH=wasm go build -o runanywhere.wasm ./device/...
```

### Dockerfile (WASI Host Sidecar)

```dockerfile
FROM golang:1.26 AS builder
WORKDIR /app
COPY . .
RUN go build -o wasi-host ./cmd/runanywhere-wasi-host

FROM debian:bookworm-slim
COPY --from=builder /app/wasi-host /usr/local/bin/
COPY runanywhere.wasm /app/
ENTRYPOINT ["wasi-host", "-module", "/app/runanywhere.wasm"]
```

### Kubernetes Manifest

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runanywhere-wasi
spec:
  template:
    spec:
      containers:
        - name: wasi-host
          image: your-registry/runanywhere-wasi-host:latest
          args: ["-module", "/app/runanywhere.wasm"]
          volumeMounts:
            - name: models
              mountPath: /models
          resources:
            requests:
              memory: "4Gi"
              cpu: "2"
      volumes:
        - name: models
          persistentVolumeClaim:
            claimName: runanywhere-models
```

### Resource Considerations

WASI modules have lower base memory overhead than CGO binaries, but inference memory requirements remain the same (model weights must still fit in RAM). The WASM linear memory grows as needed.

For more details on the host contract, see [wasi-host-contract.md](wasi-host-contract.md).
