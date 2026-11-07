package capture

import (
	"bytes"
	"strconv"
)

const maxLineBytes = 64 << 10

type ansiState uint8

const (
	stGround ansiState = iota
	stEsc
	stEscInter
	stCSI
	stOSC
	stOSCEsc
)

// recorder turns raw pty bytes into bounded plain text. It is a byte-wise state
// machine, so the result does not depend on how the input is chunked.
type recorder struct {
	state  ansiState
	params []byte // CSI parameter bytes (bounded)

	cur      []byte // current line
	col      int    // cursor column within cur
	erased   bool   // a backspace happened on this line
	lineOver bool   // current line exceeded maxLineBytes

	echo        [][]byte // expected echoed lines, consumed from the start
	echoIdx     int
	echoPending []byte

	head       []byte
	headCap    int
	tail       []byte
	tailCap    int
	total      int // bytes committed to head/tail combined
	truncated  bool
	retained   []byte
	retainCap  int
	retainLost bool
	// retainWindow keeps the newest output instead of giving up when the cap is
	// exceeded. retained then holds the bytes from retainBase on, and the
	// offsets reported to a reader stay absolute.
	retainWindow bool
	retainBase   int
}

func newRecorder(maxOutput int) *recorder {
	if maxOutput <= 0 {
		maxOutput = 64 << 10
	}
	if maxOutput < 64 {
		maxOutput = 64
	}
	r := &recorder{headCap: maxOutput / 2}
	r.tailCap = maxOutput - r.headCap
	return r
}

func (r *recorder) setEcho(sent []byte) {
	r.echo = r.echo[:0]
	for _, l := range splitEcho(sent) {
		r.echo = append(r.echo, bytes.TrimSpace(l))
	}
	r.echoIdx = 0
	r.echoPending = r.echoPending[:0]
}

func (r *recorder) write(p []byte) {
	for _, b := range p {
		r.step(b)
	}
}

func (r *recorder) step(b byte) {
	switch r.state {
	case stGround:
		r.ground(b)
	case stEsc:
		switch {
		case b == '[':
			r.state = stCSI
			r.params = r.params[:0]
		case b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_':
			r.state = stOSC
		case b >= 0x20 && b <= 0x2f:
			r.state = stEscInter
		case b == 0x1b:
			// stay
		default:
			r.state = stGround
		}
	case stEscInter:
		if b < 0x20 || b > 0x2f {
			r.state = stGround
		}
	case stCSI:
		switch {
		case b >= 0x40 && b <= 0x7e:
			r.csi(b)
			r.state = stGround
		case b == 0x1b:
			r.state = stEsc
		case b == 0x18 || b == 0x1a:
			r.state = stGround
		default:
			if len(r.params) < 16 {
				r.params = append(r.params, b)
			}
		}
	case stOSC:
		switch b {
		case 0x07, 0x18, 0x1a:
			r.state = stGround
		case 0x1b:
			r.state = stOSCEsc
		}
	case stOSCEsc:
		if b == '\\' {
			r.state = stGround
		} else {
			r.state = stEsc
			r.step(b)
		}
	}
}

func (r *recorder) csi(final byte) {
	if final != 'K' {
		return
	}
	switch string(r.params) {
	case "", "0":
		if r.col < len(r.cur) {
			r.cur = r.cur[:r.col]
		}
	case "2":
		r.cur = r.cur[:0]
		r.col = 0
	}
}

func (r *recorder) ground(b byte) {
	lineLimit := maxLineBytes
	if r.retainCap > lineLimit && !r.retainLost {
		lineLimit = r.retainCap
	}
	switch {
	case b == 0x1b:
		r.state = stEsc
	case b == '\n':
		r.commitLine()
	case b == '\r':
		r.col = 0
	case b == '\b':
		if r.col > 0 {
			r.col--
		}
		r.erased = true
	case b == '\t' || b >= 0x20 && b != 0x7f:
		switch {
		case r.col < len(r.cur):
			r.cur[r.col] = b
			r.col++
		case len(r.cur) >= lineLimit:
			r.lineOver = true
			r.truncated = true
		default:
			r.cur = append(r.cur, b)
			r.col++
		}
	}
}

func (r *recorder) commitLine() {
	line := r.cur
	if r.erased {
		line = bytes.TrimRight(line, " ")
	}
	r.emitLine(line, true)
	r.cur = r.cur[:0]
	r.col = 0
	r.erased = false
	r.lineOver = false
}

func (r *recorder) emitLine(line []byte, nl bool) {
	if r.echoIdx < len(r.echo) {
		if len(r.echoPending) == 0 && matchEcho(line, r.echo[r.echoIdx]) {
			r.echoIdx++
			return
		}
		if len(r.echo) > 1 && matchEcho(line, r.echo[len(r.echo)-1]) {
			r.echoIdx = len(r.echo)
			r.echoPending = nil
			return
		}
		if len(r.echoPending)+len(line)+1 <= 16<<10 {
			r.echoPending = append(r.echoPending, line...)
			if nl {
				r.echoPending = append(r.echoPending, '\n')
			}
			return
		}
		r.echoIdx = len(r.echo)
		r.put(r.echoPending)
		r.echoPending = nil
	}
	r.put(line)
	if nl {
		r.put([]byte{'\n'})
	}
}

// matchEcho reports whether line is the terminal echo of the expected line,
// tolerating a shell continuation prompt ("> ", "cursh> ") in front of it.
func matchEcho(line, exp []byte) bool {
	line = bytes.TrimSpace(line)
	if len(exp) == 0 {
		if len(line) > 12 {
			return false
		}
		for _, c := range line {
			if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				return false
			}
		}
		return true
	}
	return bytes.Equal(line, exp) || bytes.HasSuffix(line, exp)
}

// put appends committed text to the bounded head/tail store.
func (r *recorder) put(b []byte) {
	r.total += len(b)
	if r.retainWindow {
		r.retained = append(r.retained, b...)
		if len(r.retained) > 2*r.retainCap {
			drop := len(r.retained) - r.retainCap
			n := copy(r.retained, r.retained[drop:])
			r.retained = r.retained[:n]
			r.retainBase += drop
		}
	} else if r.retainCap > 0 && !r.retainLost {
		if len(b) > r.retainCap-len(r.retained) {
			r.retained = nil
			r.retainLost = true
		} else {
			r.retained = append(r.retained, b...)
		}
	}
	if len(r.head) < r.headCap {
		n := min(r.headCap-len(r.head), len(b))
		r.head = append(r.head, b[:n]...)
		b = b[n:]
	}
	if len(b) == 0 {
		return
	}
	if len(b) >= r.tailCap {
		r.tail = append(r.tail[:0], b[len(b)-r.tailCap:]...)
		return
	}
	r.tail = append(r.tail, b...)
	if len(r.tail) >= 2*r.tailCap {
		n := copy(r.tail, r.tail[len(r.tail)-r.tailCap:])
		r.tail = r.tail[:n]
	}
}

// finish flushes the unterminated current line (used when the command is done).
func (r *recorder) finish() {
	if len(r.cur) > 0 {
		line := r.cur
		if r.erased {
			line = bytes.TrimRight(line, " ")
		}
		r.emitLine(line, false)
		r.cur = r.cur[:0]
		r.col = 0
	}
	if len(r.echoPending) > 0 {
		r.put(r.echoPending)
		r.echoPending = nil
	}
}

// dropPendingLine removes the current unfinished line when it is prompt. A
// device leaves its prompt as the last, unterminated line, and that line is the
// signal the command has returned rather than part of its output.
func (r *recorder) dropPendingLine(prompt string) {
	if prompt == "" || !bytes.Equal(bytes.TrimRight(r.cur, " "), []byte(prompt)) {
		return
	}
	r.cur = r.cur[:0]
	r.col = 0
	r.erased = false
	r.lineOver = false
}

// snapshot builds the output text without mutating the recorder.
func (r *recorder) snapshot(final bool) (string, bool) {
	head, tail, total := r.head, r.tail, r.total
	if len(tail) > r.tailCap {
		tail = tail[len(tail)-r.tailCap:]
	}
	var pend []byte
	if !final && len(r.cur) > 0 && r.echoIdx >= len(r.echo) {
		pend = r.cur
	}
	omitted := total - len(head) - len(tail)
	var out []byte
	out = append(out, head...)
	if omitted > 0 {
		out = append(out, "\n[... "...)
		out = strconv.AppendInt(out, int64(omitted), 10)
		out = append(out, " bytes omitted ...]\n"...)
	}
	out = append(out, tail...)
	out = append(out, pend...)
	out = bytes.TrimSuffix(out, []byte{'\n'})
	return string(out), omitted > 0 || r.truncated
}
