package rdp

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type fakeMicDevice struct {
	mu     sync.Mutex
	chunks chan []byte
	closed bool
	fail   error
}

func newFakeMicDevice() *fakeMicDevice {
	return &fakeMicDevice{chunks: make(chan []byte, 16)}
}

func (d *fakeMicDevice) push(pcm []byte) {
	d.chunks <- append([]byte(nil), pcm...)
}

func (d *fakeMicDevice) read(dst []byte) (int, error) {
	chunk, ok := <-d.chunks
	if !ok {
		if d.fail != nil {
			return 0, d.fail
		}
		return 0, errMicStopped
	}
	n := copy(dst, chunk)
	return n, nil
}

func (d *fakeMicDevice) stop() {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.chunks)
	}
	d.mu.Unlock()
}

func (d *fakeMicDevice) close() {
	d.stop()
}

func withFakeMicDevice(t *testing.T, dev micDevice, err error) {
	t.Helper()
	prev := openMicDevice
	openMicDevice = func(micFormat) (micDevice, error) { return dev, err }
	t.Cleanup(func() { openMicDevice = prev })
}

func TestOpenMicrophoneRejectsBadFormats(t *testing.T) {
	for _, tc := range []struct {
		rate     uint32
		channels uint16
	}{
		{0, 1},
		{7999, 2},
		{192001, 1},
		{44100, 0},
		{44100, 3},
	} {
		if _, err := openMicrophone(tc.rate, tc.channels, 441); err == nil {
			t.Errorf("openMicrophone(%d, %d) succeeded, want error", tc.rate, tc.channels)
		}
	}
}

func TestOpenMicrophonePropagatesDeviceError(t *testing.T) {
	withFakeMicDevice(t, nil, errMicUnsupported)
	if _, err := openMicrophone(44100, 2, 441); !errors.Is(err, errMicUnsupported) {
		t.Fatalf("error = %v, want errMicUnsupported", err)
	}
}

func TestMicrophoneStreamsInOrderAndCloseUnblocksRead(t *testing.T) {
	dev := newFakeMicDevice()
	withFakeMicDevice(t, dev, nil)

	r, err := openMicrophone(16000, 1, 160)
	if err != nil {
		t.Fatal(err)
	}

	want := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	dev.push(want[:4])
	dev.push(want[4:])

	got := make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("read %v, want %v", got, want)
	}

	readErr := make(chan error, 1)
	go func() {
		var buf [16]byte
		_, err := r.Read(buf[:])
		readErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-readErr:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Read after Close = %v, want io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not unblock after Close")
	}
	// Closing twice is safe.
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
