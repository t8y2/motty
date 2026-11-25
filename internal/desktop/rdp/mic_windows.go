//go:build windows

package rdp

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wasapiShareModeShared     = 0
	wasapiEventCallback       = 0x00040000
	wasapiAutoConvertPCM      = 0x80000000
	wasapiSRCDefaultQuality   = 0x08000000
	wasapiCapture             = 1 // eCapture
	wasapiConsole             = 0 // eConsole
	wasapiBufferFlagSilent    = 0x2
	coinitMultithreaded       = 0x0
	coinitSFalse              = 0x1
	hresultChangedMode        = 0x80010106
	clsctxAll                 = 0x17
	wasapiBufferDuration100ns = 100 * 10000 // 100 ms
	waveFormatPCM             = 1
)

type comGUID struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}

var (
	clsidMMDeviceEnumerator = comGUID{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidMMDeviceEnumerator   = comGUID{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidAudioClient          = comGUID{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidAudioCaptureClient   = comGUID{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

var (
	ole32            = windows.NewLazySystemDLL("ole32.dll")
	coInitializeEx   = ole32.NewProc("CoInitializeEx")
	coUninitialize   = ole32.NewProc("CoUninitialize")
	coCreateInstance = ole32.NewProc("CoCreateInstance")
)

func comPtr(address uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&address)) }

func comVtable(object uintptr, index int) uintptr {
	return *(*uintptr)(unsafe.Add(comPtr(*(*uintptr)(comPtr(object))), index*int(unsafe.Sizeof(uintptr(0)))))
}

//go:uintptrescapes
func comCall(object uintptr, index int, arguments ...uintptr) uintptr {
	var values [16]uintptr
	if len(arguments) >= len(values) {
		panic("rdp: COM call exceeds argument limit")
	}
	values[0] = object
	copy(values[1:], arguments)
	result, _, _ := syscall.SyscallN(comVtable(object, index), values[:len(arguments)+1]...)
	return result
}

func comRelease(object *uintptr) {
	if *object != 0 {
		comCall(*object, 2) // IUnknown::Release
		*object = 0
	}
}

func comHRESULT(step string, result uintptr) error {
	if int32(uint32(result)) < 0 {
		return fmt.Errorf("rdp: %s: HRESULT %#x", step, uint32(result))
	}
	return nil
}

type wasapiMicDevice struct {
	enumerator uintptr
	endpoint   uintptr
	client     uintptr
	capture    uintptr
	audioEvent windows.Handle
	stopMu     sync.Mutex
	stopEvent  windows.Handle
	stopped    bool
	started    bool
	frameBytes int
}

func platformMicDevice(format micFormat) (micDevice, error) {
	result, _, _ := coInitializeEx.Call(0, coinitMultithreaded)
	comInit := false
	switch uint32(result) {
	case 0, coinitSFalse:
		comInit = true
	case hresultChangedMode:
		return nil, fmt.Errorf("rdp: audio thread COM apartment mismatch: %w", errMicUnsupported)
	default:
		return nil, comHRESULT("initialize COM", result)
	}
	d := &wasapiMicDevice{frameBytes: format.Channels * 2}
	if err := d.open(format); err != nil {
		d.close()
		if !comInit {
			return nil, err
		}
		return nil, err
	}
	return d, nil
}

func (d *wasapiMicDevice) open(format micFormat) error {
	r, _, _ := coCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&d.enumerator)),
	)
	runtime.KeepAlive(&clsidMMDeviceEnumerator)
	runtime.KeepAlive(&iidMMDeviceEnumerator)
	if err := comHRESULT("create audio enumerator", r); err != nil {
		return fmt.Errorf("%w: %v", errMicUnsupported, err)
	}
	// IMMDeviceEnumerator::GetDefaultAudioEndpoint(eCapture, eConsole, &endpoint).
	if r := comCall(d.enumerator, 4, wasapiCapture, wasapiConsole, uintptr(unsafe.Pointer(&d.endpoint))); int32(uint32(r)) < 0 {
		return fmt.Errorf("%w: no default input device", errMicUnsupported)
	}
	// IMMDevice::Activate(IID_IAudioClient, ...).
	if r := comCall(d.endpoint, 3, uintptr(unsafe.Pointer(&iidAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&d.client))); int32(uint32(r)) < 0 {
		return comHRESULT("activate audio capture client", r)
	}

	wfx := waveFormatEx{
		FormatTag:     waveFormatPCM,
		Channels:      uint16(format.Channels),
		SamplesPerSec: uint32(format.SampleRate),
		BlockAlign:    uint16(format.Channels * 2),
		BitsPerSample: 16,
	}
	wfx.AvgBytesPerSec = wfx.SamplesPerSec * uint32(wfx.BlockAlign)
	const flags = wasapiEventCallback | wasapiAutoConvertPCM | wasapiSRCDefaultQuality
	if r := comCall(d.client, 3,
		wasapiShareModeShared,
		flags,
		wasapiBufferDuration100ns,
		0,
		uintptr(unsafe.Pointer(&wfx)),
		0,
	); int32(uint32(r)) < 0 {
		return comHRESULT("initialize audio capture client", r)
	}
	runtime.KeepAlive(&wfx)

	var frames uint32
	if r := comCall(d.client, 4, uintptr(unsafe.Pointer(&frames))); int32(uint32(r)) < 0 {
		return comHRESULT("read capture buffer size", r)
	}
	if frames == 0 {
		return fmt.Errorf("rdp: audio capture client reported empty buffer")
	}

	// IAudioClient::GetService(IID_IAudioCaptureClient).
	if r := comCall(d.client, 14, uintptr(unsafe.Pointer(&iidAudioCaptureClient)), uintptr(unsafe.Pointer(&d.capture))); int32(uint32(r)) < 0 {
		return comHRESULT("get audio capture service", r)
	}

	audioEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return fmt.Errorf("rdp: create audio capture event: %w", err)
	}
	d.audioEvent = audioEvent

	stopEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("rdp: create audio stop event: %w", err)
	}
	d.stopEvent = stopEvent

	// IAudioClient::SetEventHandle.
	if r := comCall(d.client, 13, uintptr(d.audioEvent)); int32(uint32(r)) < 0 {
		return comHRESULT("set capture event handle", r)
	}
	// IAudioClient::Start.
	if r := comCall(d.client, 10); int32(uint32(r)) < 0 {
		return comHRESULT("start audio capture", r)
	}
	d.started = true
	return nil
}

func (d *wasapiMicDevice) read(dst []byte) (int, error) {
	written := 0
	for written == 0 {
		var packetFrames uint32
		if r := comCall(d.capture, 5, uintptr(unsafe.Pointer(&packetFrames))); int32(uint32(r)) < 0 {
			return 0, comHRESULT("read next capture packet size", r)
		}
		if packetFrames == 0 {
			handles := [2]windows.Handle{d.audioEvent, d.stopEvent}
			ev, err := windows.WaitForMultipleObjects(handles[:], false, 200)
			if err != nil {
				return 0, fmt.Errorf("rdp: wait for capture event: %w", err)
			}
			if ev == windows.WAIT_OBJECT_0+1 {
				return 0, errMicStopped
			}
			continue
		}

		for packetFrames > 0 {
			var data unsafe.Pointer
			var numFrames uint32
			var flags uint32
			if r := comCall(d.capture, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&numFrames)), uintptr(unsafe.Pointer(&flags)), 0, 0); int32(uint32(r)) < 0 {
				return written, comHRESULT("get capture buffer", r)
			}
			bytes := int(numFrames) * d.frameBytes
			if bytes > 0 && written+bytes <= len(dst) {
				if flags&wasapiBufferFlagSilent != 0 || data == nil {
					clear(dst[written : written+bytes])
				} else {
					copy(dst[written:written+bytes], unsafe.Slice((*byte)(data), bytes))
				}
				written += bytes
			}
			if r := comCall(d.capture, 4, uintptr(numFrames)); int32(uint32(r)) < 0 {
				return written, comHRESULT("release capture buffer", r)
			}
			if written >= len(dst) {
				break
			}
			if r := comCall(d.capture, 5, uintptr(unsafe.Pointer(&packetFrames))); int32(uint32(r)) < 0 {
				return written, comHRESULT("read next capture packet size", r)
			}
		}
	}
	return written, nil
}

func (d *wasapiMicDevice) stop() {
	d.stopMu.Lock()
	d.stopped = true
	if d.stopEvent != 0 {
		_ = windows.SetEvent(d.stopEvent)
	}
	d.stopMu.Unlock()
}

func (d *wasapiMicDevice) close() {
	if d.client != 0 && d.started {
		comCall(d.client, 11) // IAudioClient::Stop
		d.started = false
	}
	comRelease(&d.capture)
	comRelease(&d.client)
	comRelease(&d.endpoint)
	comRelease(&d.enumerator)
	if d.audioEvent != 0 {
		_ = windows.CloseHandle(d.audioEvent)
		d.audioEvent = 0
	}
	d.stopMu.Lock()
	if d.stopEvent != 0 {
		_ = windows.CloseHandle(d.stopEvent)
		d.stopEvent = 0
	}
	d.stopMu.Unlock()
	coUninitialize.Call()
}
