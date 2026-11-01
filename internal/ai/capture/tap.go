package capture

import (
	"bytes"
	"strconv"
)

// Result is what a Tap learned about the command so far.
type Result struct {
	// Output is the ANSI-stripped plain text the command printed, bounded to the
	// tap's maxOutput (head and tail kept, the middle replaced by an
	// "[... N bytes omitted ...]" marker).
	Output    string
	Truncated bool
	// ExitCode is the command's status, or -1 until Done.
	ExitCode int
	Done     bool
}

// Tap watches raw pty output for the marker printed by [Wrap]'s command.
// A Tap is not safe for concurrent use.
type Tap struct {
	nlT   []byte // "\n" + marker prefix + nonce
	crnlT []byte // "\r\n" + the same

	hold    []byte // bytes withheld because they might start the marker
	buf     []byte
	out     []byte
	rec     *recorder
	done    bool
	aborted bool
	code    int

	// The wrapper's echo is hidden from the terminal: the user sees the
	// logical command once instead. All raw bytes stay in hidden so a mismatch
	// can fall back to showing exactly what the shell sent.
	displayOn  bool
	displayCmd []byte
	echoLines  [][]byte
	echoIndex  int
	echoScan   int
	echoDone   bool
	hidden     []byte
}

// NewTap returns a Tap for one command wrapped with the same nonce. maxOutput
// bounds the recorded output text (default 64 KiB when <= 0).
func NewTap(nonce string, maxOutput int) *Tap {
	tok := markerPrefix + cleanNonce(nonce)
	return &Tap{
		nlT:   []byte("\n" + tok),
		crnlT: []byte("\r\n" + tok),
		rec:   newRecorder(maxOutput),
		code:  -1,
	}
}

// SetEcho tells the tap the exact bytes that were typed (the result of Wrap),
// so that their terminal echo at the start of the output is left out of
// Result.Output. It is best effort: the first echoed line that does not match
// stops the matching.
func (t *Tap) SetEcho(sent []byte) {
	t.rec.setEcho(sent)
	t.echoLines = t.echoLines[:0]
	for _, line := range bytes.Split(bytes.TrimRight(sent, "\n"), []byte{'\n'}) {
		t.echoLines = append(t.echoLines, bytes.TrimSpace(line))
	}
}

// SetDisplay hides the wrapper's terminal echo and shows command instead, as
// the terminal's visible command line. It is best effort: if the shell echoes
// something the wrapper did not predict (a wrapped line, a coloured prompt),
// the raw bytes are shown unchanged rather than guessing. SetEcho must be
// called first. The capture recorder still receives the raw echo, so its echo
// matching and output result are unchanged.
func (t *Tap) SetDisplay(command string) {
	t.displayCmd = append(t.displayCmd[:0], command...)
	t.displayOn = len(t.echoLines) > 0
	t.echoIndex = 0
	t.echoScan = 0
	t.echoDone = false
	t.hidden = t.hidden[:0]
}

// Feed takes the next chunk of raw pty output. It returns the bytes that should
// still be shown to the user and whether the command has completed. The marker
// line, including the newline that precedes it, is removed; at most the minimal
// suffix that could still become the marker is held back until the next call.
// The returned slice is valid only until the next call of Feed or Abort.
// After completion or Abort everything passes through unchanged.
func (t *Tap) Feed(p []byte) (display []byte, done bool) {
	t.out = t.out[:0]
	if t.done || t.aborted {
		t.out = append(t.out, p...)
		return t.out, t.done
	}
	buf := p
	if len(t.hold) > 0 {
		t.buf = append(append(t.buf[:0], t.hold...), p...)
		buf = t.buf
		t.hold = t.hold[:0]
	}
	pos := 0
	for {
		rest := buf[pos:]
		i := bytes.Index(rest, t.nlT)
		if i < 0 {
			k := t.holdLen(rest)
			t.emit(rest[:len(rest)-k])
			t.hold = append(t.hold[:0], rest[len(rest)-k:]...)
			return t.out, false
		}
		start := i
		if i > 0 && rest[i-1] == '\r' {
			start = i - 1
		}
		j := i + len(t.nlT)
		code, end, st := parseTail(rest, j)
		switch st {
		case tailMore:
			t.emit(rest[:start])
			t.hold = append(t.hold[:0], rest[start:]...)
			return t.out, false
		case tailNo:
			t.emit(rest[:j])
			pos += j
		default:
			t.emit(rest[:start])
			t.out = append(t.out, t.flushDisplay()...)
			t.rec.finish()
			t.done = true
			t.code = code
			t.out = append(t.out, rest[end:]...)
			return t.out, true
		}
	}
}

func (t *Tap) emit(b []byte) {
	if len(b) == 0 {
		return
	}
	t.rec.write(b)
	t.display(b)
}

// display appends b to the terminal's output, replacing the wrapper's echo with
// the logical command once all of that echo has been seen.
func (t *Tap) display(b []byte) {
	if !t.displayOn || t.echoDone {
		t.out = append(t.out, b...)
		return
	}
	t.hidden = append(t.hidden, b...)
	for !t.echoDone {
		rest := t.hidden[t.echoScan:]
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			return
		}
		line := rest[:i]
		if t.echoIndex >= len(t.echoLines) || !matchEcho(line, t.echoLines[t.echoIndex]) {
			t.out = append(t.out, t.flushDisplay()...)
			return
		}
		t.echoIndex++
		t.echoScan += i + 1
		if t.echoIndex == len(t.echoLines) {
			t.echoDone = true
			command := bytes.TrimRight(t.displayCmd, "\r\n")
			if len(command) == 0 {
				command = []byte(":")
			}
			// A bare line feed keeps the column, so every line of a multi-line
			// command would start where the one above it ended.
			for line := range bytes.SplitSeq(command, []byte{'\n'}) {
				t.out = append(t.out, line...)
				t.out = append(t.out, '\r', '\n')
			}
			t.out = append(t.out, t.hidden[t.echoScan:]...)
			t.hidden = t.hidden[:0]
			t.echoScan = 0
			return
		}
	}
}

// flushDisplay returns everything still held for the display filter unchanged.
// The terminal then looks exactly as it did before SetDisplay; the AI card
// remains the place where the command is summarized.
func (t *Tap) flushDisplay() []byte {
	if t.echoDone {
		return nil
	}
	t.echoDone = true
	out := append([]byte(nil), t.hidden...)
	t.hidden = t.hidden[:0]
	t.echoScan = 0
	return out
}

// holdLen returns the length of the longest suffix of rest that is a proper
// prefix of "\n"+marker or "\r\n"+marker.
func (t *Tap) holdLen(rest []byte) int {
	lo := len(rest) - len(t.crnlT) + 1
	if lo < 0 {
		lo = 0
	}
	for q := lo; q < len(rest); q++ {
		if c := rest[q]; c != '\r' && c != '\n' {
			continue
		}
		s := rest[q:]
		if bytes.HasPrefix(t.crnlT, s) || bytes.HasPrefix(t.nlT, s) {
			return len(s)
		}
	}
	return 0
}

type tailState uint8

const (
	tailMore tailState = iota // need more input
	tailNo                    // not a marker
	tailOK
)

const maxCodeDigits = 12

// parseTail parses "<digits>(\r)?\n" starting at rest[j:]. end is the index
// after the consumed marker line.
func parseTail(rest []byte, j int) (code, end int, st tailState) {
	k := j
	for k < len(rest) && rest[k] >= '0' && rest[k] <= '9' {
		k++
	}
	nd := k - j
	if nd > maxCodeDigits {
		return 0, 0, tailNo
	}
	if k == len(rest) {
		return 0, 0, tailMore
	}
	if nd == 0 {
		return 0, 0, tailNo
	}
	n, _ := strconv.Atoi(string(rest[j:k]))
	switch rest[k] {
	case '\r':
		if k+1 == len(rest) {
			return 0, 0, tailMore
		}
		if rest[k+1] == '\n' {
			return n, k + 2, tailOK
		}
		return n, k + 1, tailOK
	case '\n':
		return n, k + 1, tailOK
	}
	return n, k, tailOK
}

// Abort stops the capture (for instance after a timeout). Held-back bytes are
// returned so that the caller can still display them; afterwards every Feed
// passes through unchanged and nothing more is recorded.
func (t *Tap) Abort() (display []byte) {
	if t.done || t.aborted {
		return nil
	}
	t.aborted = true
	held := t.hold
	display = append(t.flushDisplay(), held...)
	t.hold = t.hold[:0]
	t.rec.write(held) // hidden bytes were recorded when emitted
	return display
}

// Result reports the output captured so far and, once done, the exit status.
func (t *Tap) Result() Result {
	out, trunc := t.rec.snapshot(t.done)
	return Result{Output: out, Truncated: trunc, ExitCode: t.code, Done: t.done}
}
