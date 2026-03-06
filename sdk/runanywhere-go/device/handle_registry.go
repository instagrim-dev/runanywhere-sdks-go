package device

import "sync"

// handleRegistry is a thread-safe registry for typed handles keyed by int64 IDs.
// It is used by both the WASM browser and WASI backends to manage LLM/STT/TTS/Embeddings handles.
type handleRegistry[T any] struct {
	mu      sync.RWMutex
	handles map[int64]T
}

func newHandleRegistry[T any]() *handleRegistry[T] {
	return &handleRegistry[T]{handles: make(map[int64]T)}
}

// Register stores a handle under the given ID. The caller provides the ID
// (typically the handle ID returned by a bridge or backend).
func (r *handleRegistry[T]) Register(id int64, item T) {
	r.mu.Lock()
	r.handles[id] = item
	r.mu.Unlock()
}

// Unregister removes a handle by ID.
func (r *handleRegistry[T]) Unregister(id int64) {
	r.mu.Lock()
	delete(r.handles, id)
	r.mu.Unlock()
}

// Get returns the handle for the given ID and whether it was found.
func (r *handleRegistry[T]) Get(id int64) (T, bool) {
	r.mu.RLock()
	item, ok := r.handles[id]
	r.mu.RUnlock()
	return item, ok
}

// CloseAll calls closer on each registered handle, then clears the registry.
// Any errors returned by closer are logged via the structured logger so
// shutdown cleanup failures are visible.
func (r *handleRegistry[T]) CloseAll(closer func(T) error) {
	r.mu.Lock()
	items := make([]T, 0, len(r.handles))
	for _, item := range r.handles {
		items = append(items, item)
	}
	r.handles = make(map[int64]T)
	r.mu.Unlock()

	for _, item := range items {
		if err := closer(item); err != nil {
			LogCore.Warn("handle close error during shutdown", map[string]string{"error": err.Error()})
		}
	}
}

// Count returns the number of registered handles.
func (r *handleRegistry[T]) Count() int {
	r.mu.RLock()
	n := len(r.handles)
	r.mu.RUnlock()
	return n
}
