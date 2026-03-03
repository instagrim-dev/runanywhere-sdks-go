package device

// Tests in this package that do not need CGO (e.g. TestErrorSentinels) run with
// CGO_ENABLED=0. When CGO_ENABLED=1, the package links against rac_commons and
// backend libs; go test ./device/... then requires those shared libraries to be
// on the library path (e.g. LD_LIBRARY_PATH or dyld path).

import (
	"errors"
	"testing"
)

func TestErrorSentinels(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("ErrUnsupported should not be nil")
	}
	if ErrNotInitialized == nil {
		t.Fatal("ErrNotInitialized should not be nil")
	}
	if ErrHandlesStillOpen == nil {
		t.Fatal("ErrHandlesStillOpen should not be nil")
	}
	if ErrCancelled == nil {
		t.Fatal("ErrCancelled should not be nil")
	}
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("errors.Is(ErrUnsupported, ErrUnsupported) should be true")
	}
}
