package rdp

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
)

var (
	errMicUnsupported = errors.New("rdp: no microphone input device is available")
	errMicStopped     = errors.New("rdp: microphone capture stopped")
)

type micFormat struct {
	SampleRate      int
	Channels        int
	FramesPerPacket int
}

// micDevice is one platform's capture stream. All methods except stop run on
// the capture goroutine locked to an OS thread.
type micDevice interface {
	read(dst []byte) (int, error)
	stop()
	close()
}

var openMicDevice = platformMicDevice

// openMicrophone opens the default audio input device for the requested 16-bit
// signed little-endian PCM format.
func openMicrophone(sampleRate uint32, channels uint16, framesPerPacket uint32) (io.ReadCloser, error) {
	if sampleRate < 8000 || sampleRate > 192000 || channels < 1 || channels > 2 {
		return nil, fmt.Errorf("rdp: unsupported microphone format %d Hz %d ch", sampleRate, channels)
	}
	frameBytes := int(channels) * 2
	// Keep up to 250 ms of captured audio in the ring so brief network backpressure
	// drops old frames rather than stalling the OS audio callback.
	ringBytes := max(int(sampleRate)/4*frameBytes, 4096)
	ringBytes -= ringBytes % frameBytes
	if ringBytes < frameBytes {
		ringBytes = frameBytes
	}

	m := &micStream{
		format: micFormat{
			SampleRate:      int(sampleRate),
			Channels:        int(channels),
			FramesPerPacket: int(framesPerPacket),
		},
		ring:  make([]byte, ringBytes),
		done:  make(chan struct{}),
		ready: make(chan struct{}),
	}
	m.cond = sync.NewCond(&m.mu)
	go m.run()
	<-m.ready
	if m.openErr != nil {
		<-m.done
		return nil, m.openErr
	}
	return m, nil
}

type micStream struct {
	format micFormat

	mu      sync.Mutex
	cond    *sync.Cond
	ring    []byte
	head    int // read offset
	used    int
	closed  bool
	failed  error
	dev     micDevice
	openErr error
	done    chan struct{}
	ready   chan struct{}
}

func (m *micStream) run() {
	runtime.LockOSThread()
	defer close(m.done)

	dev, err := loadMicDevice(m.format)
	m.mu.Lock()
	m.openErr = err
	if err != nil {
		m.failed = err
	} else {
		m.dev = dev
	}
	m.mu.Unlock()
	close(m.ready)
	if err != nil {
		return
	}

	frameBytes := m.format.Channels * 2
	scratch := make([]byte, max(m.format.SampleRate/4, 4096)*frameBytes)
	for {
		n, err := dev.read(scratch)
		if n > 0 {
			m.push(scratch[:n])
		}
		if err != nil {
			m.mu.Lock()
			if !m.closed {
				m.failed = err
			}
			m.cond.Broadcast()
			m.mu.Unlock()
			break
		}
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()
		if closed {
			break
		}
	}
	dev.close()
}

func (m *micStream) push(pcm []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	capRing := len(m.ring)
	if len(pcm) >= capRing {
		pcm = pcm[len(pcm)-capRing:]
		m.head = 0
		m.used = 0
	} else if overflow := m.used + len(pcm) - capRing; overflow > 0 {
		m.head = (m.head + overflow) % capRing
		m.used -= overflow
	}
	write := (m.head + m.used) % capRing
	first := min(len(pcm), capRing-write)
	copy(m.ring[write:], pcm[:first])
	if first < len(pcm) {
		copy(m.ring, pcm[first:])
	}
	m.used += len(pcm)
	m.cond.Broadcast()
}

func (m *micStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.used == 0 && !m.closed && m.failed == nil {
		m.cond.Wait()
	}
	if m.closed {
		return 0, io.EOF
	}
	if m.used == 0 && m.failed != nil {
		return 0, m.failed
	}
	take := min(len(p), m.used)
	capRing := len(m.ring)
	first := min(take, capRing-m.head)
	copy(p[:first], m.ring[m.head:m.head+first])
	if first < take {
		copy(p[first:take], m.ring[:take-first])
	}
	m.head = (m.head + take) % capRing
	m.used -= take
	return take, nil
}

func (m *micStream) Close() error {
	m.mu.Lock()
	already := m.closed
	m.closed = true
	dev := m.dev
	m.cond.Broadcast()
	m.mu.Unlock()
	if !already && dev != nil {
		dev.stop()
	}
	<-m.done
	return nil
}

func loadMicDevice(format micFormat) (dev micDevice, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			dev, err = nil, fmt.Errorf("rdp: microphone panic: %v", recovered)
		}
	}()
	return openMicDevice(format)
}
