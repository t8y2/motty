//go:build windows

package localshell

import (
	"bytes"
	"io"

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

var (
	legacyPSReadLineDefaultColor = []byte("\x1b[37m\x1b[40m")
	fixedPSReadLineDefaultColor  = []byte("\x1b[39;49m")
)

func windowsBuildNumber() uint32 {
	if version := windows.RtlGetVersion(); version != nil {
		return version.BuildNumber
	}
	return 0
}

func needsLegacyPSReadLineColorFix(passthrough, isWSL bool, build uint32) bool {
	return passthrough && !isWSL && build > 0 && build < legacyPSReadLineBuild
}

// psReadLineColorFilter rewrites Windows 10 PSReadLine 2.0.0-beta2's split
// default-color pair ("\x1b[37m\x1b[40m") into default foreground/background
// ("\x1b[39;49m") across arbitrary Read chunk boundaries.
type psReadLineColorFilter struct {
	src      io.Reader
	raw      []byte
	work     []byte
	pending  []byte
	ready    []byte
	readyOff int
	err      error
}

func newPSReadLineColorFilter(src io.Reader) *psReadLineColorFilter {
	return &psReadLineColorFilter{src: src}
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
		if len(f.raw) < len(p) {
			f.raw = make([]byte, len(p))
		}
		n, err := f.src.Read(f.raw[:len(p)])
		if n > 0 {
			f.process(f.raw[:n], err != nil)
		} else if err != nil {
			f.flushPending()
		}
		if err != nil {
			f.err = err
			if f.readyOff == len(f.ready) {
				f.err = nil
				return 0, err
			}
		}
	}
	copied := copy(p, f.ready[f.readyOff:])
	f.readyOff += copied
	if f.readyOff == len(f.ready) {
		f.ready = f.ready[:0]
		f.readyOff = 0
	}
	return copied, nil
}

func (f *psReadLineColorFilter) process(chunk []byte, eof bool) {
	f.work = append(f.work[:0], f.pending...)
	f.work = append(f.work, chunk...)
	f.pending = f.pending[:0]

	rest := f.work
	for {
		index := bytes.Index(rest, legacyPSReadLineDefaultColor)
		if index < 0 {
			break
		}
		f.ready = append(f.ready, rest[:index]...)
		f.ready = append(f.ready, fixedPSReadLineDefaultColor...)
		rest = rest[index+len(legacyPSReadLineDefaultColor):]
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
