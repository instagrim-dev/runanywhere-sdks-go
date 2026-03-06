package device

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleRegistryCloseAllDoesNotHoldLockDuringCloser(t *testing.T) {
	r := newHandleRegistry[int64]()
	const n = 8
	for i := int64(1); i <= n; i++ {
		r.Register(i, i)
	}

	var closed atomic.Int32
	done := make(chan struct{})
	go func() {
		r.CloseAll(func(id int64) error {
			// Simulate current shutdown behavior where handle Close() re-enters the registry.
			r.Unregister(id)
			closed.Add(1)
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("CloseAll blocked while closer re-entered the registry")
	}

	if got := closed.Load(); got != n {
		t.Fatalf("closer called %d times, want %d", got, n)
	}
	if got := r.Count(); got != 0 {
		t.Fatalf("registry count after CloseAll = %d, want 0", got)
	}
}
