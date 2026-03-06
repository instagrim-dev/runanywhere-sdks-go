//go:build js && wasm

package device

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWasmSTTStreamIteratorCloseIsNonBlocking(t *testing.T) {
	ch := make(chan wasmStreamChunk)
	var released atomic.Int32

	it := &wasmSTTStreamIterator{
		chunkCh: ch,
		release: func() { released.Add(1) },
	}

	done := make(chan struct{})
	go func() {
		_ = it.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Close blocked on unfinished stream channel")
	}

	if got := released.Load(); got != 1 {
		t.Fatalf("release called %d times, want 1", got)
	}

	if err := it.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	if got := released.Load(); got != 1 {
		t.Fatalf("release called %d times after second Close, want 1", got)
	}
}
