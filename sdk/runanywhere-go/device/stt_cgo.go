//go:build cgo

package device

/*
#cgo CFLAGS: -I${SRCDIR}/../../runanywhere-commons/include
#cgo LDFLAGS: -lrac_commons -lrac_backend_llamacpp -lrac_backend_onnx

#include <stdlib.h>
#include <stdint.h>
#include "rac/features/stt/rac_stt_service.h"
#include "rac/features/stt/rac_stt_types.h"

extern void go_rac_stt_stream_callback(char* partial_text, int32_t is_final, void* user_data);

static void stt_stream_cb_wrapper(const char* partial_text, rac_bool_t is_final, void* user_data) {
	go_rac_stt_stream_callback((char*)partial_text, (int32_t)is_final, user_data);
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

// STT is an on-device speech-to-text handle (CGO implementation).
type STT struct {
	handle    C.rac_handle_t
	streamWg  sync.WaitGroup // blocks Close() until in-flight TranscribeStream finishes
	closeOnce sync.Once
}

// NewSTT creates an STT handle for the given model path. Call device.Init() first with RegisterONNX: true.
// opts is used for stream/transcribe options; creation-time options are reserved for future use.
func NewSTT(ctx context.Context, modelPath string, opts *STTOptions) (*STT, error) {
	if !isInitialized() {
		return nil, ErrNotInitialized
	}
	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))
	var outHandle C.rac_handle_t
	res := C.rac_stt_create(cPath, &outHandle)
	if res != C.RAC_SUCCESS {
		return nil, fmt.Errorf("%w", &RACError{Op: "rac_stt_create", Code: int(res)})
	}
	incrementHandleCount()
	return &STT{handle: outHandle}, nil
}

// Transcribe runs non-streaming transcription.
func (s *STT) Transcribe(ctx context.Context, audioData []byte, opts *STTOptions) (string, error) {
	if s.handle == nil {
		return "", fmt.Errorf("STT handle closed")
	}
	if len(audioData) == 0 {
		return "", nil
	}
	var cOpts *C.rac_stt_options_t
	if opts != nil {
		cOpts = &C.rac_stt_options_t{
			language:          nil,
			detect_language:   C.RAC_FALSE,
			enable_timestamps: C.rac_bool_t(0),
			sample_rate:       C.int32_t(16000),
		}
		if opts.Language != "" {
			cOpts.language = C.CString(opts.Language)
			defer C.free(unsafe.Pointer(cOpts.language))
		}
		if opts.EnableTimestamps {
			cOpts.enable_timestamps = C.RAC_TRUE
		}
	}
	var result C.rac_stt_result_t
	res := C.rac_stt_transcribe(s.handle, unsafe.Pointer(&audioData[0]), C.size_t(len(audioData)), cOpts, &result)
	if res != C.RAC_SUCCESS {
		return "", fmt.Errorf("%w", &RACError{Op: "rac_stt_transcribe", Code: int(res)})
	}
	defer C.rac_stt_result_free(&result)
	if result.text == nil {
		return "", nil
	}
	return C.GoString(result.text), nil
}

// sttStreamChunk is sent from the C stream callback to the iterator.
type sttStreamChunk struct {
	text    string
	isFinal bool
}

// sttStreamIter implements STTStreamIterator for TranscribeStream.
type sttStreamIter struct {
	ch     <-chan sttStreamChunk
	closed atomic.Bool
}

func (it *sttStreamIter) Next() (text string, isFinal bool, err error) {
	if it.closed.Load() {
		return "", true, nil
	}
	chunk, ok := <-it.ch
	if !ok {
		return "", true, nil
	}
	return chunk.text, chunk.isFinal, nil
}

// Close marks the iterator closed. The C STT backend does not expose cancellation;
// the underlying stream may continue until completion.
func (it *sttStreamIter) Close() error {
	it.closed.Store(true)
	return nil
}

// TranscribeStream returns a stream iterator. The C stream runs in a separate goroutine.
// Context ctx is currently unused (the STT backend does not support cancellation); it is reserved for future use.
// Callers should still pass a context for API consistency.
func (s *STT) TranscribeStream(ctx context.Context, audioData []byte, opts *STTOptions) (STTStreamIterator, error) {
	if s.handle == nil {
		return nil, fmt.Errorf("STT handle closed")
	}
	if len(audioData) == 0 {
		return nil, fmt.Errorf("audio data is empty")
	}
	// Copy audio into C-owned memory so the stream goroutine does not hold a Go slice pointer.
	cAudio := C.CBytes(audioData)

	const bufSize = 128
	ch := make(chan sttStreamChunk, bufSize)
	iter := &sttStreamIter{ch: ch}

	state := &sttStreamState{ch: ch}
	h := cgo.NewHandle(state)
	state.handleID = uintptr(h)

	handle := s.handle
	audioLen := len(audioData)
	optsCopy := opts
	s.streamWg.Add(1)
	go func() {
		defer s.streamWg.Done()
		// Allocate C options in the goroutine so they are not freed by defers in TranscribeStream before the C call runs.
		var cOpts *C.rac_stt_options_t
		if optsCopy != nil {
			cOpts = &C.rac_stt_options_t{
				language:          nil,
				detect_language:   C.RAC_FALSE,
				enable_timestamps: C.rac_bool_t(0),
				sample_rate:       C.int32_t(16000),
			}
			if optsCopy.Language != "" {
				cOpts.language = C.CString(optsCopy.Language)
				defer C.free(unsafe.Pointer(cOpts.language))
			}
			if optsCopy.EnableTimestamps {
				cOpts.enable_timestamps = C.RAC_TRUE
			}
		}
		defer func() {
			C.free(cAudio)
			h.Delete()
			close(ch)
		}()
		C.rac_stt_transcribe_stream(handle, cAudio, C.size_t(audioLen),
			cOpts, C.rac_stt_stream_callback_t(C.stt_stream_cb_wrapper), unsafe.Pointer(&state.handleID))
	}()

	return iter, nil
}

type sttStreamState struct {
	ch       chan<- sttStreamChunk
	handleID uintptr // passed to C as void*; callback reads *uintptr (avoids unsafe.Pointer(uintptr) misuse)
}

//export go_rac_stt_stream_callback
func go_rac_stt_stream_callback(partialText *C.char, isFinal C.int32_t, userData unsafe.Pointer) {
	if userData == nil {
		return
	}
	id := *(*uintptr)(userData)
	v, ok := cgo.Handle(id).Value().(*sttStreamState)
	if !ok || v == nil {
		return
	}
	text := ""
	if partialText != nil {
		text = C.GoString(partialText)
	}
	// Non-blocking send so the stream goroutine never blocks on a full channel
	// (avoids goroutine/memory leak when caller stops reading and calls Close()).
	select {
	case v.ch <- sttStreamChunk{text: text, isFinal: isFinal != 0}:
	default:
		// Channel full; drop chunk so C stream can complete and defer cleanup runs
	}
}

// Close releases the handle. Idempotent and safe for concurrent use. Blocks until any in-flight TranscribeStream has finished.
func (s *STT) Close() error {
	s.closeOnce.Do(func() {
		if s.handle == nil {
			return
		}
		s.streamWg.Wait()
		C.rac_stt_destroy(s.handle)
		s.handle = nil
		decrementHandleCount()
	})
	return nil
}
