//go:build windows

package localshell

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// legacyPSReadLineBuild is the first Windows build (Windows Server 2022 /
// early Windows 11) whose inbox Windows PowerShell ships PSReadLine with
// PowerShell/PSReadLine#1626, emitting default colors ("\x1b[39;49m") instead
// of explicit ANSI gray-on-black ("\x1b[37m\x1b[40m") when painting spaces and
// erasing edited text. Older builds (Windows 10) relied on the inbox ConPTY's
// VT color quirk that microsoft/terminal#17666 removed from the modern
// passthrough ConPTY we bundle, leaving permanent ANSI-black blocks on spaces
// and deleted text in light themes, non-black color schemes, and transparent
// terminals (issue #1737).
const legacyPSReadLineBuild = 20348

// defaultPSReadLineFlushTimeout is how long a trailing prefix of
// legacyPSReadLineDefaultColor (such as a bare ESC or "\x1b[37m") waits for the
// rest of a split sequence before being released untouched.
const defaultPSReadLineFlushTimeout = 10 * time.Millisecond

var (
	legacyPSReadLineDefaultColor = []byte("\x1b[37m\x1b[40m")
	fixedPSReadLineDefaultColor  = []byte("\x1b[39;49m")
	// windowsBuildNumberFn is overridden by tests so the filter activation path
	// can be exercised on newer Windows builds (such as Windows 11 CI runners).
	windowsBuildNumberFn = windowsBuildNumber
)

func windowsBuildNumber() uint32 {
	if version := windows.RtlGetVersion(); version != nil {
		return version.BuildNumber
	}
	return 0
}

func isWindowsPowerShell(program string) bool {
	return strings.EqualFold(filepath.Base(program), "powershell.exe")
}

func needsLegacyPSReadLineColorFix(passthrough bool, program string, build uint32) bool {
	return passthrough && isWindowsPowerShell(program) && build > 0 && build < legacyPSReadLineBuild
}

type filterChunk struct {
	data []byte
	err  error
}

// psReadLineColorFilter rewrites Windows 10 PSReadLine 2.0.0-beta2's split
// default-color pair ("\x1b[37m\x1b[40m") into default foreground/background
// ("\x1b[39;49m") across arbitrary Read chunk boundaries.
//
// When a chunk ends with a partial prefix of legacyPSReadLineDefaultColor, it
// waits at most flushTimeout for the next chunk before releasing those bytes
// unchanged, so a trailing ESC or "\x1b[37m" is never held indefinitely when
// the program goes idle.
type psReadLineColorFilter struct {
	chunks        chan filterChunk
	done          chan struct{}
	closeOnce     sync.Once
	flushTimeout  time.Duration
	shouldRewrite func() bool

	work     []byte
	pending  []byte
	ready    []byte
	readyOff int
	err      error
}

func newPSReadLineColorFilter(src io.Reader, shouldRewrite func() bool) *psReadLineColorFilter {
	return newPSReadLineColorFilterWithTimeout(src, shouldRewrite, defaultPSReadLineFlushTimeout)
}

func newPSReadLineColorFilterWithTimeout(src io.Reader, shouldRewrite func() bool, flushTimeout time.Duration) *psReadLineColorFilter {
	if flushTimeout <= 0 {
		flushTimeout = defaultPSReadLineFlushTimeout
	}
	f := &psReadLineColorFilter{
		chunks:        make(chan filterChunk, 1),
		done:          make(chan struct{}),
		flushTimeout:  flushTimeout,
		shouldRewrite: shouldRewrite,
	}
	go f.pump(src)
	return f
}

func (f *psReadLineColorFilter) Close() {
	f.closeOnce.Do(func() { close(f.done) })
}

func (f *psReadLineColorFilter) pump(src io.Reader) {
	defer close(f.chunks)
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			chunk := filterChunk{data: append([]byte(nil), buf[:n]...), err: err}
			select {
			case f.chunks <- chunk:
			case <-f.done:
				return
			}
			if err != nil {
				return
			}
			continue
		}
		if err != nil {
			select {
			case f.chunks <- filterChunk{err: err}:
			case <-f.done:
			}
			return
		}
	}
}

func (f *psReadLineColorFilter) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for f.readyOff == len(f.ready) {
		f.ready = f.ready[:0]
		f.readyOff = 0
		if f.err != nil {
			err := f.err
			f.err = nil
			return 0, err
		}
		if len(f.pending) > 0 {
			timer := time.NewTimer(f.flushTimeout)
			select {
			case chunk, ok := <-f.chunks:
				timer.Stop()
				if !ok {
					f.flushPending()
					if f.readyOff == len(f.ready) {
						return 0, io.EOF
					}
					break
				}
				f.consume(chunk)
			case <-timer.C:
				f.flushPending()
			}
			continue
		}
		chunk, ok := <-f.chunks
		if !ok {
			return 0, io.EOF
		}
		f.consume(chunk)
	}
	copied := copy(p, f.ready[f.readyOff:])
	f.readyOff += copied
	if f.readyOff == len(f.ready) {
		f.ready = f.ready[:0]
		f.readyOff = 0
	}
	return copied, nil
}

func (f *psReadLineColorFilter) consume(chunk filterChunk) {
	if len(chunk.data) > 0 {
		f.process(chunk.data, chunk.err != nil)
	} else if chunk.err != nil {
		f.flushPending()
	}
	if chunk.err != nil {
		f.err = chunk.err
	}
}

func (f *psReadLineColorFilter) process(chunk []byte, eof bool) {
	f.work = append(f.work[:0], f.pending...)
	f.work = append(f.work, chunk...)
	f.pending = f.pending[:0]

	rest := f.work
	if bytes.Contains(rest, legacyPSReadLineDefaultColor) {
		if f.shouldRewrite == nil || f.shouldRewrite() {
			for {
				index := bytes.Index(rest, legacyPSReadLineDefaultColor)
				if index < 0 {
					break
				}
				f.ready = append(f.ready, rest[:index]...)
				f.ready = append(f.ready, fixedPSReadLineDefaultColor...)
				rest = rest[index+len(legacyPSReadLineDefaultColor):]
			}
		} else {
			f.ready = append(f.ready, rest...)
			return
		}
	}
	if eof {
		f.ready = append(f.ready, rest...)
		return
	}
	keep := legacyColorTailPrefix(rest)
	f.ready = append(f.ready, rest[:len(rest)-keep]...)
	f.pending = append(f.pending[:0], rest[len(rest)-keep:]...)
}

func (f *psReadLineColorFilter) flushPending() {
	if len(f.pending) > 0 {
		f.ready = append(f.ready, f.pending...)
		f.pending = f.pending[:0]
	}
}

// legacyColorTailPrefix returns the length of the longest suffix of data that
// is a proper prefix of legacyPSReadLineDefaultColor.
func legacyColorTailPrefix(data []byte) int {
	limit := min(len(data), len(legacyPSReadLineDefaultColor)-1)
	for length := limit; length > 0; length-- {
		if bytes.Equal(data[len(data)-length:], legacyPSReadLineDefaultColor[:length]) {
			return length
		}
	}
	return 0
}
