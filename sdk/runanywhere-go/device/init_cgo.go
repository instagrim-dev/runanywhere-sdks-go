//go:build cgo

package device

/*
#cgo CFLAGS: -I${SRCDIR}/../../runanywhere-commons/include
#cgo LDFLAGS: -lrac_commons -lrac_backend_llamacpp
// Optional: add -lrac_backend_onnx for STT/TTS. User may need CGO_LDFLAGS=-L<path> to find libs.

#include <stdlib.h>
#include <stdint.h>
#include "rac/core/rac_core.h"
#include "rac/core/rac_platform_adapter.h"
#include "rac/core/rac_error.h"

extern rac_result_t rac_backend_llamacpp_register(void);
extern rac_result_t rac_backend_onnx_register(void);

// Declare Go exports so adapter_log/adapter_now_ms see them. Signatures must match
// cgo's generated export header exactly (int32_t, char*, int64_t).
extern void go_rac_log(int32_t level, char* category, char* message, void* user_data);
extern int64_t go_rac_now_ms(void* user_data);

// Wrappers with exact adapter callback types.
static void adapter_log(rac_log_level_t level, const char* category, const char* message, void* user_data) {
	go_rac_log((int32_t)level, (char*)category, (char*)message, user_data);
}
static int64_t adapter_now_ms(void* user_data) {
	return go_rac_now_ms(user_data);
}

static void set_adapter_log_and_now_ms(rac_platform_adapter_t* a, void* user_data) {
	a->log = adapter_log;
	a->now_ms = adapter_now_ms;
	a->user_data = user_data;
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"runtime/cgo"
)

//export go_rac_log
func go_rac_log(level C.int32_t, category, message *C.char, user_data unsafe.Pointer) {
	if user_data == nil {
		return
	}
	id := *(*uintptr)(user_data)
	v, ok := cgo.Handle(id).Value().(*initState)
	if !ok || v == nil {
		return
	}
	// Forward only when message level is at or above configured threshold (higher = less verbose).
	if int(level) < v.logLevel {
		return
	}
	cat := C.GoString(category)
	msg := C.GoString(message)
	meta := map[string]string{"cgo_category": cat}
	switch LogLevel(level) {
	case LogLevelTrace:
		LogCore.Trace(msg, meta)
	case LogLevelDebug:
		LogCore.Debug(msg, meta)
	case LogLevelInfo:
		LogCore.Info(msg, meta)
	case LogLevelWarning:
		LogCore.Warn(msg, meta)
	case LogLevelError:
		LogCore.Error(msg, nil, meta)
	case LogLevelFault:
		LogCore.Fault(msg, nil, meta)
	default:
		LogCore.Debug(msg, meta)
	}
}

//export go_rac_now_ms
func go_rac_now_ms(user_data unsafe.Pointer) C.int64_t {
	// user_data is unused for now_ms; we keep the signature for adapter compatibility
	return C.int64_t(time.Now().UnixMilli())
}

type initState struct {
	handle   cgo.Handle
	handleID uintptr // passed to C as void*; callback reads *uintptr to get handle (avoids unsafe.Pointer(uintptr) misuse)
	logLevel int
	logTag   *C.char
	adapter  C.rac_platform_adapter_t
}

var (
	initMu       sync.Mutex
	initialized  bool
	initStateVal *initState
	handleCount  int
)


func isInitialized() bool {
	initMu.Lock()
	defer initMu.Unlock()
	return initialized
}

// incrementHandleCount is called when a device handle (LLM, STT, etc.) is created.
func incrementHandleCount() {
	initMu.Lock()
	defer initMu.Unlock()
	handleCount++
}

// decrementHandleCount is called when a device handle is closed.
func decrementHandleCount() {
	initMu.Lock()
	defer initMu.Unlock()
	handleCount--
}

func Init(ctx context.Context) error {
	return InitWithConfig(ctx, nil)
}

func InitWithConfig(ctx context.Context, cfg *Config) error {
	initMu.Lock()
	defer initMu.Unlock()
	if initialized {
		return nil
	}
	logLevel := 2 // RAC_LOG_INFO default
	logTag := "GoDevice"
	if cfg != nil {
		if cfg.LogLevel != 0 {
			logLevel = cfg.LogLevel
		}
		if cfg.LogTag != "" {
			logTag = cfg.LogTag
		}
	}
	state := &initState{
		logLevel: logLevel,
		logTag:   C.CString(logTag),
	}
	state.handle = cgo.NewHandle(state)
	state.handleID = uintptr(state.handle)
	C.set_adapter_log_and_now_ms(&state.adapter, unsafe.Pointer(&state.handleID))
	config := C.rac_config_t{
		platform_adapter: &state.adapter,
		log_level:        C.rac_log_level_t(logLevel),
		log_tag:          state.logTag,
		reserved:         nil,
	}
	res := C.rac_init(&config)
	if res != C.RAC_SUCCESS {
		state.handle.Delete()
		C.free(unsafe.Pointer(state.logTag))
		return fmt.Errorf("%w", newCGOError("rac_init", int(res)))
	}
	initStateVal = state
	initialized = true
	res = C.rac_backend_llamacpp_register()
	if res != C.RAC_SUCCESS && res != C.RAC_ERROR_MODULE_ALREADY_REGISTERED {
		C.rac_shutdown()
		initialized = false
		initStateVal.handle.Delete()
		C.free(unsafe.Pointer(initStateVal.logTag))
		initStateVal = nil
		return fmt.Errorf("%w", newCGOError("rac_backend_llamacpp_register", int(res)))
	}
	if cfg != nil && cfg.RegisterONNX {
		res = C.rac_backend_onnx_register()
		if res != C.RAC_SUCCESS && res != C.RAC_ERROR_MODULE_ALREADY_REGISTERED {
			C.rac_shutdown()
			initialized = false
			initStateVal.handle.Delete()
			C.free(unsafe.Pointer(initStateVal.logTag))
			initStateVal = nil
			return fmt.Errorf("%w", newCGOError("rac_backend_onnx_register", int(res)))
		}
	}
	Publish(NewLifecycleEvent("initialized"))
	return nil
}

func Shutdown() error {
	initMu.Lock()
	defer initMu.Unlock()
	if !initialized {
		return nil
	}
	if handleCount > 0 {
		return ErrHandlesStillOpen
	}
	C.rac_shutdown()
	if initStateVal != nil {
		initStateVal.handle.Delete()
		C.free(unsafe.Pointer(initStateVal.logTag))
		initStateVal = nil
	}
	initialized = false
	Publish(NewLifecycleEvent("shutdown"))
	return nil
}
