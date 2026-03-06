// Package device provides on-device inference (LLM, STT, TTS, embeddings)
// with a unified API that works across multiple backends:
//
//   - Native (CGO): Full performance via runanywhere-commons shared libraries
//   - WASM Browser: In-browser inference via JavaScript bridge to Web SDK
//   - WASI/Edge: Server-side WASM with host-provided or remote inference
//   - Stub: Returns ErrUnsupported for unsupported configurations
//
// Build tags select the backend:
//   - cgo: Native backend with C++ shared libraries
//   - js && wasm: Browser WASM backend with JS bridge
//   - wasip1 && wasm: WASI backend for edge/server-side WASM
//   - !cgo && !js && !wasip1: Stub backend (always returns ErrUnsupported)
//
// # Inference
//
//	ctx := context.Background()
//	if err := device.Init(ctx); err != nil {
//	    // Handle error (e.g., ErrUnsupported for stub builds)
//	}
//	defer device.Shutdown()
//
//	llm, err := device.NewLLM(ctx, modelPath, nil)
//	if err != nil {
//	    return err
//	}
//	defer llm.Close()
//
//	result, err := llm.Generate(ctx, prompt, nil)
//
// # Observability
//
// Three pluggable subsystems follow the same interface → default → globals
// pattern as [SetMetricsRecorder]:
//
// Structured logging replaces bare log.Printf calls with a leveled,
// category-scoped logger. Use [SetLogDestination] to bridge to zerolog,
// zap, slog, or any other framework. Use [SetLogLevel] to control the
// threshold. Category-scoped convenience loggers are provided as package
// variables (e.g. [LogLLM], [LogBridge]).
//
//	device.SetLogLevel(device.LogLevelDebug)
//	device.SetLogDestination(myZerologAdapter)
//	device.LogLLM.Info("model loaded", map[string]string{"model": path})
//
// The event bus provides typed pub/sub for SDK lifecycle events.
// [Subscribe] filters by [EventCategory]; [SubscribeAll] receives
// everything. Init and Shutdown publish [LifecycleEvent]; bridge
// errors publish [DeviceErrorEvent].
//
//	sub := device.Subscribe(device.EventCategorySDK, func(e device.Event) {
//	    fmt.Println("event:", e.EventType())
//	})
//	defer device.Unsubscribe(sub)
//
// The circuit breaker provides failure isolation for unreliable
// operations. Use [GetCircuitBreaker] to obtain a named breaker from
// the package-level registry, and [ExecuteWithResult] for generic calls.
//
//	cb := device.GetCircuitBreaker("llm-inference", device.CircuitBreakerConfig{
//	    FailureThreshold: 3,
//	})
//	result, err := device.ExecuteWithResult(cb, func() (string, error) {
//	    return llm.Generate(ctx, prompt, nil)
//	})
package device
