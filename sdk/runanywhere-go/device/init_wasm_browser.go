//go:build js && wasm

package device

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"
)

// =============================================================================
// Browser WASM Backend
// =============================================================================

// wasmBrowserBackend implements the Backend interface for browser WASM environments.
// It communicates with JavaScript via the __RunAnywhereDeviceBridge.
type wasmBrowserBackend struct {
	mu           sync.RWMutex
	initialized  bool
	config       *Config
	capabilities CapabilitySet
	llmHandles   *handleRegistry[*wasmLLM]
	sttHandles   *handleRegistry[*wasmSTT]
	ttsHandles   *handleRegistry[*wasmTTS]
	embHandles   *handleRegistry[*wasmEmbeddings]
}

const maxBridgeStreamQueue = 256
const minSupportedBridgeVersion = "1.0.0"

// newWASMBrowserBackend creates a new browser WASM backend.
func newWASMBrowserBackend() *wasmBrowserBackend {
	return &wasmBrowserBackend{
		llmHandles: newHandleRegistry[*wasmLLM](),
		sttHandles: newHandleRegistry[*wasmSTT](),
		ttsHandles: newHandleRegistry[*wasmTTS](),
		embHandles: newHandleRegistry[*wasmEmbeddings](),
	}
}

// Init initializes the browser WASM backend.
func (b *wasmBrowserBackend) Init(ctx context.Context, cfg *Config) error {
	if err := checkContextNotDone(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized {
		return &RACError{
			Code:    ErrCodeAlreadyInitialized,
			Message: "WASM browser backend already initialized",
		}
	}

	b.config = cfg
	if b.config == nil {
		b.config = &Config{}
	}

	if err := b.checkBridgeVersion(); err != nil {
		return err
	}

	cfgJSON, err := b.configToJSON()
	if err != nil {
		return err
	}

	// Initialize via JS bridge
	result, err := b.callBridgeSync("init", cfgJSON)
	if err != nil {
		return err
	}

	// Parse initialization result to determine capabilities
	var initResult struct {
		Success      bool     `json:"success"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(result), &initResult); err != nil {
		return &RACError{
			Code:    ErrCodeWASMError,
			Message: "failed to parse init result: " + err.Error(),
		}
	}

	if !initResult.Success {
		return &RACError{
			Code:    ErrCodeWASMError,
			Message: "WASM bridge initialization failed",
		}
	}

	// Set capabilities based on what the bridge reports
	b.capabilities = ParseCapabilityStrings(initResult.Capabilities)
	b.initialized = true

	Publish(NewLifecycleEvent("initialized"))
	return nil
}

// Shutdown shuts down the browser WASM backend.
func (b *wasmBrowserBackend) Shutdown(ctx context.Context) error {
	if err := checkContextNotDone(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	if !b.initialized {
		b.mu.Unlock()
		return nil
	}
	b.initialized = false
	b.capabilities = CapabilitySetNone
	b.mu.Unlock()

	// Close all handles
	b.llmHandles.CloseAll(func(h *wasmLLM) error { return h.Close() })
	b.sttHandles.CloseAll(func(h *wasmSTT) error { return h.Close() })
	b.ttsHandles.CloseAll(func(h *wasmTTS) error { return h.Close() })
	b.embHandles.CloseAll(func(h *wasmEmbeddings) error { return h.Close() })

	// Call bridge shutdown
	if _, err := b.callBridgeSync("shutdown", "{}"); err != nil {
		LogBridge.Error("shutdown error", err)
	}

	Publish(NewLifecycleEvent("shutdown"))
	return nil
}

// Capabilities returns the available capabilities.
func (b *wasmBrowserBackend) Capabilities() CapabilitySet {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.capabilities
}

// Mode returns BackendModeWASMBrowser.
func (b *wasmBrowserBackend) Mode() BackendMode { return BackendModeWASMBrowser }

// IsInitialized returns whether the backend is initialized.
func (b *wasmBrowserBackend) IsInitialized() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.initialized
}

// =============================================================================
// Handle Management (delegates to handleRegistry)
// =============================================================================

func (b *wasmBrowserBackend) registerLLM(id int64, llm *wasmLLM) { b.llmHandles.Register(id, llm) }
func (b *wasmBrowserBackend) unregisterLLM(id int64)             { b.llmHandles.Unregister(id) }
func (b *wasmBrowserBackend) registerSTT(id int64, stt *wasmSTT) { b.sttHandles.Register(id, stt) }
func (b *wasmBrowserBackend) unregisterSTT(id int64)             { b.sttHandles.Unregister(id) }
func (b *wasmBrowserBackend) registerTTS(id int64, tts *wasmTTS) { b.ttsHandles.Register(id, tts) }
func (b *wasmBrowserBackend) unregisterTTS(id int64)             { b.ttsHandles.Unregister(id) }
func (b *wasmBrowserBackend) registerEmbeddings(id int64, emb *wasmEmbeddings) {
	b.embHandles.Register(id, emb)
}
func (b *wasmBrowserBackend) unregisterEmbeddings(id int64) { b.embHandles.Unregister(id) }

// =============================================================================
// Bridge Communication
// =============================================================================

// callBridgeSync calls a JavaScript method synchronously via the bridge.
// This uses the syscall/js callback pattern to avoid re-entrancy deadlocks.
func (b *wasmBrowserBackend) callBridgeSync(method string, argsJSON string) (string, error) {
	// Create callback channel
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	var callback js.Func
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			callback.Release()
		})
	}

	// Create JavaScript callback (FuncOf: must not block; use buffered channel)
	callback = js.FuncOf(func(this js.Value, callbackArgs []js.Value) any {
		defer release()
		if len(callbackArgs) == 0 {
			select {
			case errCh <- &RACError{Code: ErrCodeWASMError, Message: "bridge call returned no arguments"}:
			default:
			}
			return nil
		}
		firstArg := callbackArgs[0]
		if firstArg.Type() == js.TypeObject {
			errObj := firstArg.Get("error")
			if !errObj.IsUndefined() {
				select {
				case errCh <- &RACError{Code: ErrCodeWASMError, Message: "bridge error: " + errObj.String()}:
				default:
				}
				return nil
			}
		}
		var resultStr string
		if firstArg.Type() == js.TypeObject {
			resultStr = js.Global().Get("JSON").Call("stringify", firstArg).String()
		} else {
			resultStr = firstArg.String()
		}
		select {
		case resultCh <- resultStr:
		default:
		}
		return nil
	})

	// Call the bridge method
	bridge := js.Global().Get("__RunAnywhereDeviceBridge")
	if bridge.IsUndefined() {
		release()
		return "", &RACError{
			Code:    ErrCodeBridgeDisconnected,
			Message: "__RunAnywhereDeviceBridge is not defined",
		}
	}

	if err := bridgeCallSafe(bridge, method, argsJSON, callback); err != nil {
		release()
		return "", err
	}

	// Use configurable timeout; default 30s
	timeout := 30 * time.Second
	if b.config != nil && b.config.BridgeTimeout > 0 {
		timeout = b.config.BridgeTimeout
	}

	// Wait for result or timeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		release()
		return result, nil
	case err := <-errCh:
		release()
		return "", err
	case <-timer.C:
		release()
		return "", &RACError{
			Code:    ErrCodeTimeout,
			Message: "bridge call timed out: " + method,
		}
	}
}

// wasmStreamChunk is a single chunk from a bridge stream (LLM token or STT segment).
type wasmStreamChunk struct {
	Content string
	Done    bool
	Err     error
}

// callBridgeStream invokes a bridge method that streams chunks via onChunk.
// The bridge may send (a) StreamFrame JSON: {"modality":"llm","payload":{"text":"..."},"done":false}, or
// (b) simple format: {"content":"..."} / {"text":"..."}, {"done":true}, {"error":"..."}.
// The returned channel is closed when the stream ends. The caller must call releaseChunkCallback when done.
func (b *wasmBrowserBackend) callBridgeStream(method string, argsJSON string) (chunkCh <-chan wasmStreamChunk, releaseChunkCallback func()) {
	ch := make(chan wasmStreamChunk, 64)
	var closeOnce sync.Once
	var queueMu sync.Mutex
	queue := make([]wasmStreamChunk, 0, 16)
	notifyDrainCh := make(chan struct{}, 1)
	stopDrainCh := make(chan struct{})
	closed := false
	overflowed := false
	closeOutCh := func() {
		closeOnce.Do(func() {
			queueMu.Lock()
			closed = true
			queue = nil
			queueMu.Unlock()
			close(ch)
		})
	}
	enqueue := func(chunk wasmStreamChunk) {
		dropped := false
		queueMu.Lock()
		if closed || overflowed {
			queueMu.Unlock()
			return
		}
		if len(queue) >= maxBridgeStreamQueue {
			// Do not silently truncate streamed output. Overflow is turned into a
			// terminal error chunk for deterministic failure semantics.
			overflowed = true
			queue = queue[:0]
			queue = append(queue, wasmStreamChunk{
				Err: &RACError{
					Code:    ErrCodeOutOfMemory,
					Message: "bridge stream queue overflow: consumer too slow",
				},
			})
			dropped = true
		} else {
			queue = append(queue, chunk)
		}
		queueMu.Unlock()
		if dropped {
			IncCounter("bridge_stream_queue_dropped_total", 1, map[string]string{
				"method": method,
			})
		}
		select {
		case notifyDrainCh <- struct{}{}:
		default:
		}
	}

	onChunk := js.FuncOf(func(this js.Value, callbackArgs []js.Value) any {
		if len(callbackArgs) == 0 {
			return nil
		}
		s := callbackArgs[0].String()
		// Prefer modality/payload format (StreamFrame)
		if frame, err := ParseStreamFrameFromString(s); err == nil && (frame.Modality != "" || frame.Done || frame.Error != nil) {
			if frame.Error != nil {
				enqueue(wasmStreamChunk{Err: &RACError{Code: ErrCodeGenerationFailed, Message: frame.Error.Message}})
				return nil
			}
			if frame.Done {
				enqueue(wasmStreamChunk{Done: true})
				return nil
			}
			content := frame.Payload.Text
			if content == "" && len(frame.Payload.Tokens) > 0 {
				content = frame.Payload.Tokens[0]
			}
			if content != "" {
				enqueue(wasmStreamChunk{Content: content, Done: false})
			}
			return nil
		}
		// Fallback: simple format
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(s), &m)
		if m == nil {
			return nil
		}
		if done, _ := m["done"].(bool); done {
			enqueue(wasmStreamChunk{Done: true})
			return nil
		}
		if errMsg, ok := m["error"].(string); ok && errMsg != "" {
			enqueue(wasmStreamChunk{Err: &RACError{Code: ErrCodeGenerationFailed, Message: errMsg}})
			return nil
		}
		if content, ok := m["content"].(string); ok {
			enqueue(wasmStreamChunk{Content: content, Done: false})
		} else if text, ok := m["text"].(string); ok {
			enqueue(wasmStreamChunk{Content: text, Done: false})
		}
		return nil
	})
	bridge := js.Global().Get("__RunAnywhereDeviceBridge")
	if bridge.IsUndefined() {
		ch <- wasmStreamChunk{Err: &RACError{Code: ErrCodeBridgeDisconnected, Message: "__RunAnywhereDeviceBridge is not defined"}}
		close(ch)
		return ch, onChunk.Release
	}
	go func() {
		retryTicker := time.NewTicker(2 * time.Millisecond)
		defer retryTicker.Stop()
		sendChunk := func(chunk wasmStreamChunk) bool {
			for {
				select {
				case <-stopDrainCh:
					closeOutCh()
					return false
				case ch <- chunk:
					return true
				case <-retryTicker.C:
				}
			}
		}
		for {
			select {
			case <-notifyDrainCh:
			case <-stopDrainCh:
				closeOutCh()
				return
			}
			for {
				queueMu.Lock()
				if closed || len(queue) == 0 {
					queueMu.Unlock()
					break
				}
				chunk := queue[0]
				queue[0] = wasmStreamChunk{}
				queue = queue[1:]
				queueMu.Unlock()

				if !sendChunk(chunk) {
					return
				}
				if chunk.Done || chunk.Err != nil {
					closeOutCh()
					return
				}
			}
		}
	}()
	var releaseOnce sync.Once
	releaseChunk := func() {
		releaseOnce.Do(func() {
			// closeOutCh() is idempotent and may race with the drain loop path.
			queueMu.Lock()
			closed = true
			queue = nil
			queueMu.Unlock()
			close(stopDrainCh)
			onChunk.Release()
		})
	}
	if err := bridgeCallSafe(bridge, method, argsJSON, onChunk); err != nil {
		ch <- wasmStreamChunk{Err: err}
		close(ch)
		close(stopDrainCh)
		return ch, onChunk.Release
	}
	return ch, releaseChunk
}

// bridgeCallSafe looks up method on the bridge object and calls it with args.
// Returns ErrCodeUnsupported if the method is missing or not a function.
func bridgeCallSafe(bridge js.Value, method string, args ...any) error {
	m := bridge.Get(method)
	if m.IsUndefined() || m.Type() != js.TypeFunction {
		return &RACError{
			Code:    ErrCodeUnsupported,
			Message: "bridge method not supported: " + method,
		}
	}
	m.Invoke(args...)
	return nil
}

// configToJSON converts the config to JSON for the bridge.
func (b *wasmBrowserBackend) configToJSON() (string, error) {
	type ConfigJSON struct {
		LogLevel     int    `json:"logLevel"`
		LogTag       string `json:"logTag"`
		RegisterONNX bool   `json:"registerONNX"`
	}
	cfg := ConfigJSON{
		LogLevel:     b.config.LogLevel,
		LogTag:       b.config.LogTag,
		RegisterONNX: b.config.RegisterONNX,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", &RACError{
			Code:    ErrCodeInvalidParam,
			Message: "failed to marshal init config: " + err.Error(),
		}
	}
	return string(data), nil
}

func (b *wasmBrowserBackend) checkBridgeVersion() error {
	bridge := js.Global().Get("__RunAnywhereDeviceBridge")
	if bridge.IsUndefined() {
		return &RACError{
			Code:    ErrCodeBridgeDisconnected,
			Message: "__RunAnywhereDeviceBridge is not defined",
		}
	}
	versionVal := bridge.Get("version")
	if versionVal.IsUndefined() || versionVal.Type() != js.TypeString {
		return nil
	}
	bridgeVersion := versionVal.String()
	if semverLess(bridgeVersion, minSupportedBridgeVersion) {
		return &RACError{
			Code:    ErrCodeUnsupported,
			Message: "bridge version " + bridgeVersion + " is below required " + minSupportedBridgeVersion,
		}
	}
	return nil
}

func semverLess(a, b string) bool {
	parse := func(v string) [3]int {
		var out [3]int
		parts := strings.SplitN(strings.TrimSpace(v), ".", 4)
		for i := 0; i < len(parts) && i < 3; i++ {
			part := strings.SplitN(parts[i], "-", 2)[0]
			n, err := strconv.Atoi(part)
			if err != nil {
				// Fail closed: malformed versions are treated as 0.0.0.
				return [3]int{}
			}
			out[i] = n
		}
		return out
	}
	av := parse(a)
	bv := parse(b)
	for i := 0; i < 3; i++ {
		if av[i] < bv[i] {
			return true
		}
		if av[i] > bv[i] {
			return false
		}
	}
	return false
}

// wasmBackend is the global WASM browser backend instance.
var wasmBackend = newWASMBrowserBackend()

// =============================================================================
// Package-Level Functions
// =============================================================================

// Init initializes the device stack for browser WASM environment.
// This is called by the public API and routes to the appropriate backend.
func Init(ctx context.Context) error {
	return InitWithConfig(ctx, nil)
}

// InitWithConfig initializes with explicit configuration.
func InitWithConfig(ctx context.Context, cfg *Config) error {
	return wasmBackend.Init(ctx, cfg)
}

// Shutdown shuts down the device stack.
func Shutdown() error {
	return wasmBackend.Shutdown(context.Background())
}

// isInitialized returns whether the backend is initialized.
func isInitialized() bool {
	return wasmBackend.IsInitialized()
}
