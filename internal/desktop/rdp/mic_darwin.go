//go:build darwin && !ios

package rdp

/*
#cgo darwin LDFLAGS: -framework AudioToolbox -framework CoreAudio
#include <stdint.h>

typedef struct motty_mic motty_mic;

motty_mic *motty_mic_open(double rate, uint32_t channels, int32_t *status);
int64_t motty_mic_read(motty_mic *m, void *pcm, uintptr_t len);
void motty_mic_stop(motty_mic *m);
void motty_mic_close(motty_mic *m);
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

type darwinMicDevice struct {
	handle *C.motty_mic
}

func platformMicDevice(format micFormat) (micDevice, error) {
	var status C.int32_t
	handle := C.motty_mic_open(C.double(format.SampleRate), C.uint32_t(format.Channels), &status)
	if handle == nil {
		if status == -1 {
			return nil, fmt.Errorf("%w: default input device is missing", errMicUnsupported)
		}
		return nil, fmt.Errorf("rdp: open default input unit: OSStatus %d", int32(status))
	}
	return &darwinMicDevice{handle: handle}, nil
}

func (d *darwinMicDevice) read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	n := C.motty_mic_read(d.handle, unsafe.Pointer(&dst[0]), C.uintptr_t(len(dst)))
	runtime.KeepAlive(dst)
	if n == 0 {
		return 0, errMicStopped
	}
	if n < 0 {
		return 0, fmt.Errorf("rdp: read from input unit: OSStatus %d", int64(n))
	}
	return int(n), nil
}

func (d *darwinMicDevice) stop() {
	if d.handle != nil {
		C.motty_mic_stop(d.handle)
	}
}

func (d *darwinMicDevice) close() {
	if d.handle != nil {
		C.motty_mic_close(d.handle)
		d.handle = nil
	}
}
