//go:build cgo

package device

/*
#cgo CFLAGS: -I${SRCDIR}/../../runanywhere-commons/include
#cgo LDFLAGS: -lrac_commons -lrac_backend_llamacpp -lrac_backend_onnx

#include <stdlib.h>
#include "rac/features/tts/rac_tts_service.h"
#include "rac/features/tts/rac_tts_types.h"
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"
)

// TTS is an on-device text-to-speech handle (CGO implementation).
// SynthesizeStream is not implemented (commons TTS stream path has a known leak; use Synthesize only).
type TTS struct {
	handle    C.rac_handle_t
	closeOnce sync.Once
}

// NewTTS creates a TTS handle for the given voice/model path. Call device.Init() first with RegisterONNX: true.
// opts is reserved for future use.
func NewTTS(ctx context.Context, voicePath string, opts *TTSOptions) (*TTS, error) {
	if !isInitialized() {
		return nil, ErrNotInitialized
	}
	cPath := C.CString(voicePath)
	defer C.free(unsafe.Pointer(cPath))
	var outHandle C.rac_handle_t
	res := C.rac_tts_create(cPath, &outHandle)
	if res != C.RAC_SUCCESS {
		return nil, fmt.Errorf("%w", newCGOError("rac_tts_create", int(res)))
	}
	incrementHandleCount()
	return &TTS{handle: outHandle}, nil
}

// Synthesize runs non-streaming synthesis and returns raw audio bytes.
func (t *TTS) Synthesize(ctx context.Context, text string, opts *TTSOptions) ([]byte, error) {
	if t.handle == nil {
		return nil, fmt.Errorf("TTS handle closed")
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	var cOpts *C.rac_tts_options_t
	if opts != nil {
		cOpts = &C.rac_tts_options_t{
			voice:       nil,
			language:    nil,
			rate:        C.float(opts.Rate),
			sample_rate: C.int32_t(22050),
		}
		if opts.Voice != "" {
			cOpts.voice = C.CString(opts.Voice)
			defer C.free(unsafe.Pointer(cOpts.voice))
		}
	}
	var result C.rac_tts_result_t
	res := C.rac_tts_synthesize(t.handle, cText, cOpts, &result)
	if res != C.RAC_SUCCESS {
		return nil, fmt.Errorf("%w", newCGOError("rac_tts_synthesize", int(res)))
	}
	defer C.rac_tts_result_free(&result)
	if result.audio_data == nil || result.audio_size == 0 {
		return nil, nil
	}
	out := C.GoBytes(result.audio_data, C.int(result.audio_size))
	return out, nil
}

// SynthesizeStream returns (nil, ErrUnsupported). TTS streaming is gated until the commons ONNX stream path is fixed.
func (t *TTS) SynthesizeStream(ctx context.Context, text string, opts *TTSOptions) (TTSStreamIterator, error) {
	return nil, ErrUnsupported
}

// Close releases the handle. Idempotent and safe for concurrent use.
func (t *TTS) Close() error {
	t.closeOnce.Do(func() {
		if t.handle == nil {
			return
		}
		C.rac_tts_destroy(t.handle)
		t.handle = nil
		decrementHandleCount()
	})
	return nil
}
