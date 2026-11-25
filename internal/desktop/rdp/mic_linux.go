//go:build linux && (amd64 || arm64)

package rdp

import (
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	alsaCapture        = 1 // SND_PCM_STREAM_CAPTURE
	alsaAccessRWInter  = 3 // SND_PCM_ACCESS_RW_INTERLEAVED
	alsaFormatS16LE    = 2 // SND_PCM_FORMAT_S16_LE
	alsaRecoverRetries = 4
)

type alsaMicDevice struct {
	library         uintptr
	pcm             uintptr
	frame           int
	framesPerPacket int
	api             *alsaMicAPI
	stopped         atomic.Bool
}

type alsaMicAPI struct {
	open        func(pcm *uintptr, name *byte, stream int32, mode int32) int32
	close       func(pcm uintptr) int32
	drop        func(pcm uintptr) int32
	prepare     func(pcm uintptr) int32
	recover     func(pcm uintptr, err int32, silent int32) int32
	wait        func(pcm uintptr, timeout int32) int32
	readi       func(pcm uintptr, buf unsafe.Pointer, frames uintptr) int64
	strerror    func(err int32) *byte
	paramsAlloc func(params *uintptr) int32
	paramsFree  func(params uintptr)
	paramsAny   func(pcm uintptr, params uintptr) int32
	setAccess   func(pcm uintptr, params uintptr, access int32) int32
	setFormat   func(pcm uintptr, params uintptr, format int32) int32
	setChannels func(pcm uintptr, params uintptr, channels uint32) int32
	setRate     func(pcm uintptr, params uintptr, rate uint32, dir int32) int32
	setRateNear func(pcm uintptr, params uintptr, rate *uint32, dir *int32) int32
	params      func(pcm uintptr, params uintptr) int32
}

func openALSAMic() (*alsaMicAPI, uintptr, error) {
	var library uintptr
	var err error
	for _, name := range []string{"libasound.so.2", "libasound.so"} {
		library = 0
		library, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err == nil {
			break
		}
	}
	if library == 0 {
		return nil, 0, fmt.Errorf("rdp: load libasound: %w", errMicUnsupported)
	}
	api := &alsaMicAPI{}
	purego.RegisterLibFunc(&api.open, library, "snd_pcm_open")
	purego.RegisterLibFunc(&api.close, library, "snd_pcm_close")
	purego.RegisterLibFunc(&api.drop, library, "snd_pcm_drop")
	purego.RegisterLibFunc(&api.prepare, library, "snd_pcm_prepare")
	purego.RegisterLibFunc(&api.recover, library, "snd_pcm_recover")
	purego.RegisterLibFunc(&api.wait, library, "snd_pcm_wait")
	purego.RegisterLibFunc(&api.readi, library, "snd_pcm_readi")
	purego.RegisterLibFunc(&api.strerror, library, "snd_strerror")
	purego.RegisterLibFunc(&api.paramsAlloc, library, "snd_pcm_hw_params_malloc")
	purego.RegisterLibFunc(&api.paramsFree, library, "snd_pcm_hw_params_free")
	purego.RegisterLibFunc(&api.paramsAny, library, "snd_pcm_hw_params_any")
	purego.RegisterLibFunc(&api.setAccess, library, "snd_pcm_hw_params_set_access")
	purego.RegisterLibFunc(&api.setFormat, library, "snd_pcm_hw_params_set_format")
	purego.RegisterLibFunc(&api.setChannels, library, "snd_pcm_hw_params_set_channels")
	purego.RegisterLibFunc(&api.setRate, library, "snd_pcm_hw_params_set_rate")
	purego.RegisterLibFunc(&api.setRateNear, library, "snd_pcm_hw_params_set_rate_near")
	purego.RegisterLibFunc(&api.params, library, "snd_pcm_hw_params")
	return api, library, nil
}

func platformMicDevice(format micFormat) (micDevice, error) {
	api, library, err := openALSAMic()
	if err != nil {
		return nil, err
	}
	name := []byte("default\x00")
	var pcm uintptr
	if status := api.open(&pcm, &name[0], alsaCapture, 0); status < 0 {
		message := alsaMicError(api, status)
		purego.Dlclose(library)
		return nil, fmt.Errorf("rdp: open ALSA default capture device: %s: %w", message, errMicUnsupported)
	}
	if err := configureALSAMic(api, pcm, format); err != nil {
		api.close(pcm)
		purego.Dlclose(library)
		return nil, err
	}
	fpp := format.FramesPerPacket
	if fpp <= 0 {
		fpp = max(format.SampleRate/100, 1)
	}
	return &alsaMicDevice{library: library, pcm: pcm, frame: format.Channels * 2, framesPerPacket: fpp, api: api}, nil
}

func configureALSAMic(api *alsaMicAPI, pcm uintptr, format micFormat) error {
	var params uintptr
	if status := api.paramsAlloc(&params); status < 0 {
		return fmt.Errorf("rdp: allocate ALSA capture parameters: %s", alsaMicError(api, status))
	}
	defer api.paramsFree(params)
	fail := func(step string, status int32) error {
		return fmt.Errorf("rdp: %s: %s", step, alsaMicError(api, status))
	}
	if status := api.paramsAny(pcm, params); status < 0 {
		return fail("read ALSA capture parameters", status)
	}
	if status := api.setAccess(pcm, params, alsaAccessRWInter); status < 0 {
		return fail("set interleaved capture access", status)
	}
	if status := api.setFormat(pcm, params, alsaFormatS16LE); status < 0 {
		return fail("set 16-bit capture PCM", status)
	}
	if status := api.setChannels(pcm, params, uint32(format.Channels)); status < 0 {
		return fail("set capture channels", status)
	}
	if status := api.setRate(pcm, params, uint32(format.SampleRate), 0); status < 0 {
		rate := uint32(format.SampleRate)
		var dir int32
		if near := api.setRateNear(pcm, params, &rate, &dir); near < 0 || rate != uint32(format.SampleRate) {
			return fail("set capture sample rate", status)
		}
	}
	if status := api.params(pcm, params); status < 0 {
		return fail("apply ALSA capture parameters", status)
	}
	if status := api.prepare(pcm); status < 0 {
		return fail("prepare ALSA capture device", status)
	}
	return nil
}

func (d *alsaMicDevice) read(dst []byte) (int, error) {
	wantFrames := len(dst) / d.frame
	if d.formatFrames() > 0 && d.formatFrames() < wantFrames {
		wantFrames = d.formatFrames()
	}
	if wantFrames < 1 {
		wantFrames = 1
	}
	for {
		if d.stopped.Load() {
			return 0, errMicStopped
		}
		ready := d.api.wait(d.pcm, 50)
		if d.stopped.Load() {
			return 0, errMicStopped
		}
		if ready == 0 {
			continue
		}
		if ready < 0 {
			if status := d.api.recover(d.pcm, ready, 1); status < 0 {
				return 0, fmt.Errorf("rdp: wait ALSA capture: %s", alsaMicError(d.api, status))
			}
			continue
		}
		got := d.api.readi(d.pcm, unsafe.Pointer(&dst[0]), uintptr(wantFrames))
		if got > 0 {
			return int(got) * d.frame, nil
		}
		if got == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		recovered := false
		for range alsaRecoverRetries {
			if d.stopped.Load() {
				return 0, errMicStopped
			}
			if status := d.api.recover(d.pcm, int32(got), 1); status < 0 {
				return 0, fmt.Errorf("rdp: capture on ALSA device: %s", alsaMicError(d.api, status))
			}
			got = d.api.readi(d.pcm, unsafe.Pointer(&dst[0]), uintptr(wantFrames))
			if got > 0 {
				return int(got) * d.frame, nil
			}
			if got == 0 {
				recovered = true
				break
			}
		}
		if !recovered {
			return 0, fmt.Errorf("rdp: ALSA capture device stopped: %s", alsaMicError(d.api, int32(got)))
		}
	}
}

func (d *alsaMicDevice) formatFrames() int { return d.framesPerPacket }

func (d *alsaMicDevice) stop() {
	d.stopped.Store(true)
}

func (d *alsaMicDevice) close() {
	if d.pcm != 0 {
		d.api.drop(d.pcm)
		d.api.close(d.pcm)
		d.pcm = 0
	}
	if d.library != 0 {
		purego.Dlclose(d.library)
		d.library = 0
	}
}

func alsaMicError(api *alsaMicAPI, status int32) string {
	if message := api.strerror(status); message != nil {
		return alsaGoString(message)
	}
	return fmt.Sprintf("status %d", status)
}

func alsaGoString(text *byte) string {
	if text == nil {
		return ""
	}
	var length int
	for ptr := unsafe.Pointer(text); *(*byte)(unsafe.Add(ptr, length)) != 0; length++ {
	}
	return string(unsafe.Slice(text, length))
}
