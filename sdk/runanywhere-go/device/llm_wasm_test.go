//go:build js && wasm

package device

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestWasmLLMStreamIteratorCloseIsNonBlocking(t *testing.T) {
	ch := make(chan wasmStreamChunk)
	var released atomic.Int32

	it := &wasmLLMStreamIterator{
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

func TestWasmLLMStreamIteratorCloseUnblocksBlockedNext(t *testing.T) {
	ch := make(chan wasmStreamChunk)
	var released atomic.Int32

	it := &wasmLLMStreamIterator{
		ctx:     context.Background(),
		chunkCh: ch,
		closeCh: make(chan struct{}),
		release: func() { released.Add(1) },
	}

	resultCh := make(chan error, 1)
	go func() {
		_, done, err := it.Next()
		if !done {
			resultCh <- errors.New("expected done=true after Close")
			return
		}
		if !errors.Is(err, io.EOF) {
			resultCh <- errors.New("expected io.EOF after Close")
			return
		}
		resultCh <- nil
	}()

	time.Sleep(50 * time.Millisecond)
	if err := it.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Next remained blocked after Close")
	}

	if got := released.Load(); got != 1 {
		t.Fatalf("release called %d times, want 1", got)
	}
}
