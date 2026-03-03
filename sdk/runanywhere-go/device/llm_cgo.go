//go:build cgo

package device

/*
#cgo CFLAGS: -I${SRCDIR}/../../runanywhere-commons/include
#cgo LDFLAGS: -lrac_commons -lrac_backend_llamacpp

#include <stdlib.h>
#include <stdint.h>
#include "rac/backends/rac_llm_llamacpp.h"

// Go export for stream callback; declare to match cgo export header (char*, int32_t, void*).
extern int32_t go_rac_llm_stream_callback(char* token, int32_t is_final, void* user_data);

static rac_bool_t stream_cb_wrapper(const char* token, rac_bool_t is_final, void* user_data) {
	return (rac_bool_t)go_rac_llm_stream_callback((char*)token, (int32_t)is_final, user_data);
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"runtime/cgo"
)

// LLM is an on-device LLM handle (CGO implementation).
type LLM struct {
	handle    C.rac_handle_t
	streamWg  sync.WaitGroup // blocks Close() until in-flight streams finish
	closeOnce sync.Once
}

// NewLLM creates an LLM handle for the given model path. Call device.Init() first.
// opts is reserved for future use (e.g. context_size, num_threads via C API config).
func NewLLM(ctx context.Context, modelPath string, opts *LLMOptions) (*LLM, error) {
	if !isInitialized() {
		return nil, ErrNotInitialized
	}
	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))
	var config *C.rac_llm_llamacpp_config_t
	// NULL config = use defaults
	var outHandle C.rac_handle_t
	res := C.rac_llm_llamacpp_create(cPath, config, &outHandle)
	if res != C.RAC_SUCCESS {
		return nil, fmt.Errorf("%w", &RACError{Op: "rac_llm_llamacpp_create", Code: int(res)})
	}
	incrementHandleCount()
	return &LLM{handle: outHandle}, nil
}

// Generate runs non-streaming generation.
func (l *LLM) Generate(ctx context.Context, prompt string, opts *LLMOptions) (string, error) {
	if l.handle == nil {
		return "", fmt.Errorf("LLM handle closed")
	}
	cPrompt := C.CString(prompt)
	defer C.free(unsafe.Pointer(cPrompt))
	var cOpts *C.rac_llm_options_t
	var cStopStrs []*C.char
	if opts != nil {
		cOpts = &C.rac_llm_options_t{
			max_tokens:         C.int32_t(opts.MaxTokens),
			temperature:        C.float(opts.Temperature),
			top_p:              C.float(opts.TopP),
			stop_sequences:     nil,
			num_stop_sequences: 0,
			streaming_enabled:  C.rac_bool_t(0),
			system_prompt:      nil,
		}
		if opts.SystemPrompt != "" {
			cOpts.system_prompt = C.CString(opts.SystemPrompt)
			defer C.free(unsafe.Pointer(cOpts.system_prompt))
		}
		if len(opts.StopSequences) > 0 {
			cStopStrs = make([]*C.char, len(opts.StopSequences))
			for i, s := range opts.StopSequences {
				cStopStrs[i] = C.CString(s)
				defer C.free(unsafe.Pointer(cStopStrs[i]))
			}
			cOpts.stop_sequences = (**C.char)(unsafe.Pointer(&cStopStrs[0]))
			cOpts.num_stop_sequences = C.size_t(len(cStopStrs))
		}
	}
	var result C.rac_llm_result_t
	res := C.rac_llm_llamacpp_generate(l.handle, cPrompt, cOpts, &result)
	if res != C.RAC_SUCCESS {
		return "", fmt.Errorf("%w", &RACError{Op: "rac_llm_llamacpp_generate", Code: int(res)})
	}
	defer C.rac_llm_result_free(&result)
	if result.text == nil {
		return "", nil
	}
	return C.GoString(result.text), nil
}

// streamChunk is sent from the C stream callback to the iterator.
type streamChunk struct {
	token   string
	isFinal bool
}

// llmStreamIter implements LLMStreamIterator for GenerateStream.
type llmStreamIter struct {
	ch        <-chan streamChunk
	cancelFn  func()
	cancelOnce sync.Once
	cancelled atomic.Bool
	closed    atomic.Bool
}

func (it *llmStreamIter) Next() (token string, isFinal bool, err error) {
	if it.closed.Load() {
		return "", true, nil
	}
	chunk, ok := <-it.ch
	if !ok {
		if it.cancelled.Load() {
			return "", true, ErrCancelled
		}
		return "", true, nil
	}
	return chunk.token, chunk.isFinal, nil
}

func (it *llmStreamIter) Close() error {
	if it.closed.Swap(true) {
		return nil
	}
	it.cancelOnce.Do(func() {
		if it.cancelFn != nil {
			it.cancelFn()
		}
	})
	return nil
}

// GenerateStream returns a stream iterator. The C stream runs in a goroutine; cancel via context or Close().
func (l *LLM) GenerateStream(ctx context.Context, prompt string, opts *LLMOptions) (LLMStreamIterator, error) {
	if l.handle == nil {
		return nil, fmt.Errorf("LLM handle closed")
	}
	const bufSize = 128
	ch := make(chan streamChunk, bufSize)
	iter := &llmStreamIter{ch: ch, cancelFn: func() { C.rac_llm_llamacpp_cancel(l.handle) }}

	state := &streamState{ch: ch, iter: iter}
	h := cgo.NewHandle(state)
	state.handleID = uintptr(h)

	streamDone := make(chan struct{})
	l.streamWg.Add(1)
	go func() {
		defer l.streamWg.Done()
		// Allocate C args in the goroutine so they are not freed by defers in GenerateStream before the C call runs.
		cPrompt := C.CString(prompt)
		defer C.free(unsafe.Pointer(cPrompt))
		var cOpts *C.rac_llm_options_t
		if opts != nil {
			cOpts = &C.rac_llm_options_t{
				max_tokens:         C.int32_t(opts.MaxTokens),
				temperature:        C.float(opts.Temperature),
				top_p:              C.float(opts.TopP),
				stop_sequences:     nil,
				num_stop_sequences: 0,
				streaming_enabled:  C.rac_bool_t(1),
				system_prompt:      nil,
			}
			if opts.SystemPrompt != "" {
				cOpts.system_prompt = C.CString(opts.SystemPrompt)
				defer C.free(unsafe.Pointer(cOpts.system_prompt))
			}
			if len(opts.StopSequences) > 0 {
				cStopStrs := make([]*C.char, len(opts.StopSequences))
				for i, s := range opts.StopSequences {
					cStopStrs[i] = C.CString(s)
					defer C.free(unsafe.Pointer(cStopStrs[i]))
				}
				cOpts.stop_sequences = (**C.char)(unsafe.Pointer(&cStopStrs[0]))
				cOpts.num_stop_sequences = C.size_t(len(cStopStrs))
			}
		}
		defer func() {
			h.Delete()
			close(ch)
			close(streamDone)
		}()
		C.rac_llm_llamacpp_generate_stream(l.handle, cPrompt, cOpts, C.rac_llm_llamacpp_stream_callback_fn(C.stream_cb_wrapper), unsafe.Pointer(&state.handleID))
	}()

	go func() {
		select {
		case <-ctx.Done():
			iter.cancelled.Store(true)
			iter.cancelOnce.Do(func() {
				C.rac_llm_llamacpp_cancel(l.handle)
			})
		case <-streamDone:
			// Stream finished normally; exit so this goroutine does not outlive the stream.
		}
	}()

	return iter, nil
}

type streamState struct {
	ch       chan<- streamChunk
	iter     *llmStreamIter
	handleID uintptr // passed to C as void*; callback reads *uintptr (avoids unsafe.Pointer(uintptr) misuse)
}

//export go_rac_llm_stream_callback
func go_rac_llm_stream_callback(token *C.char, isFinal C.rac_bool_t, userData unsafe.Pointer) C.rac_bool_t {
	if userData == nil {
		return C.RAC_TRUE
	}
	id := *(*uintptr)(userData)
	v, ok := cgo.Handle(id).Value().(*streamState)
	if !ok || v == nil {
		return C.RAC_TRUE
	}
	tok := ""
	if token != nil {
		tok = C.GoString(token)
	}
	// Non-blocking send; drop chunk when full so C stream can complete (consistent with STT).
	select {
	case v.ch <- streamChunk{token: tok, isFinal: isFinal != 0}:
	default:
		// Channel full; drop chunk so C stream can complete and defer cleanup runs
	}
	return C.RAC_TRUE
}

// Close releases the handle. Idempotent and safe for concurrent use. Blocks until any in-flight GenerateStream has finished.
func (l *LLM) Close() error {
	l.closeOnce.Do(func() {
		if l.handle == nil {
			return
		}
		l.streamWg.Wait()
		C.rac_llm_llamacpp_destroy(l.handle)
		l.handle = nil
		decrementHandleCount()
	})
	return nil
}
