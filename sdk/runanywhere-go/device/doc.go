// Package device provides on-device inference (LLM, STT, TTS, embeddings)
// by binding to runanywhere-commons (C API). Build with CGO_ENABLED=1 and
// the shared libs to use real implementations; with CGO_ENABLED=0 the package
// builds stubs that return ErrUnsupported.
package device
