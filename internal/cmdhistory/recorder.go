package cmdhistory

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// QuietBefore is how long the terminal must have been silent when the first
	// key of a line is pressed. A prompt is the last thing a shell prints before
	// it waits; a key pressed while output is still arriving is typed ahead of a
	// prompt that is not there yet, and the line cannot be read back.
	QuietBefore = 40 * time.Millisecond
	// maxRows bounds how many wrapped rows make one command line.
	maxRows = 8
	// maxTyped bounds the keys remembered for one line.
	maxTyped = 4 * MaxCommand
)

// Recorder watches the keys a user sends to one terminal and returns each
// command line that is safe to remember. It owns no clock and no goroutine: the
// caller tells it about output and gives it the screen when keys are sent.
type Recorder struct {
	lastOutput time.Duration
	heard      bool

	armed bool // the first key of the line has been seen
	skip  bool // the line cannot be recorded, whatever is typed in it
	multi bool // a newline was pasted into the line

	promptX    int    // cursor column at the first key: the end of the prompt
	promptRows int    // wrapped rows above the cursor's row at that moment
	right      string // text right of the cursor then: a right-hand prompt
	rightShown string // the same, grey text included, to tell a shell's suggestion from it

	typed   []byte // keys that echo, since the last editing key
	tainted bool   // an editing key was used: only the screen knows the line
	inPaste bool
	last    string
}

// Output tells the recorder the terminal has just received output.
func (r *Recorder) Output(now time.Duration) { r.lastOutput, r.heard = now, true }

// Reset forgets the line being typed. Call it when recording is turned off or
// the terminal changes hands, so a half-seen line is not finished later.
func (r *Recorder) Reset() {
	r.armed, r.skip, r.multi, r.tainted, r.inPaste = false, false, false, false, false
	r.typed = r.typed[:0]
}

// Typed notes keys sent to the shell. It returns the command line they ended,
// when there is one that may be remembered.
func (r *Recorder) Typed(p []byte, screen Screen, now time.Duration) (command string, ok bool) {
	for index := 0; index < len(p); index++ {
		if !r.armed && !r.skip {
			r.begin(screen, now)
		}
		b := p[index]
		switch {
		case b == 0x1b:
			if rest := p[index:]; hasPrefix(rest, "\x1b[200~") {
				r.inPaste = true
				index += len("\x1b[200~") - 1
			} else if hasPrefix(rest, "\x1b[201~") {
				r.inPaste = false
				index += len("\x1b[201~") - 1
			} else {
				index += escapeLength(rest) - 1
			}
			r.edit()
		case b == '\r' || b == '\n':
			if b == '\n' && index > 0 && p[index-1] == '\r' {
				continue
			}
			if r.inPaste {
				r.multi = true
				continue
			}
			if index != len(p)-1 && !(b == '\r' && index == len(p)-2 && p[index+1] == '\n') {
				// More than one line arrived at once: a paste, or a script sent
				// whole. The echo of the first cannot be told from the rest.
				r.Reset()
				r.skip = true
				continue
			}
			command, ok = r.finish(screen)
			r.Reset()
			if ok && command == r.last {
				return "", false
			}
			if ok {
				r.last = command
			}
			return command, ok
		case b == 0x03 || b == 0x04:
			r.Reset() // abandoned; the next prompt starts a new line
		case b == 0x7f || b == 0x08:
			if n := len(r.typed); n > 0 {
				_, size := utf8.DecodeLastRune(r.typed)
				r.typed = r.typed[:n-size]
			} else {
				r.tainted = true
			}
		case b < 0x20:
			r.edit()
		default:
			if len(r.typed) >= maxTyped {
				r.skip = true
				continue
			}
			r.typed = append(r.typed, b)
		}
	}
	return "", false
}

// edit notes a key that changes the line in a way the recorder does not follow
// (Tab, an arrow, a control key): from here on the screen is the only source.
func (r *Recorder) edit() {
	r.tainted = true
	r.typed = r.typed[:0]
}

// begin looks at the screen when the first key of a line is pressed.
func (r *Recorder) begin(screen Screen, now time.Duration) {
	if screen.Alt() || screen.Scrolled() || r.heard && now-r.lastOutput < QuietBefore {
		r.skip = true
		return
	}
	x, y := screen.Cursor()
	r.promptX = x
	r.promptRows = 0
	for row := y; row > 0 && screen.Wrapped(row-1) && r.promptRows < maxRows; row-- {
		r.promptRows++
	}
	r.right = strings.TrimSpace(screen.Span(y, x, -1, true))
	r.rightShown = strings.TrimSpace(screen.Span(y, x, -1, false))
	r.armed = true
}

// finish decides on the line the Enter key ends.
func (r *Recorder) finish(screen Screen) (string, bool) {
	if !r.armed || r.skip || r.multi || screen.Alt() || screen.Scrolled() {
		return "", false
	}
	_, y := screen.Cursor()
	first := y
	for first > 0 && screen.Wrapped(first-1) && y-first < maxRows {
		first--
	}
	promptRow := first + r.promptRows
	if y-first >= maxRows || promptRow > y {
		return "", false
	}

	// What was on the screen before the cursor must not read like a request for
	// a secret, and must be a prompt at all: a line that starts in column zero
	// (cat > file, a program reading lines) is not a command line.
	prompt := screen.Span(promptRow, 0, r.promptX, true)
	if strings.TrimSpace(prompt) == "" {
		return "", false
	}
	context := prompt
	for row := first; row < promptRow; row++ {
		context = screen.Span(row, 0, -1, false) + " " + context
	}
	if first > 0 {
		context = screen.Span(first-1, 0, -1, true) + " " + context
	}
	if PasswordPrompt(context) {
		return "", false
	}

	// The line as the screen shows it, joined across wrapped rows.
	var shown strings.Builder
	for row := promptRow; row <= y; row++ {
		from := 0
		if row == promptRow {
			from = r.promptX
		}
		segment := screen.Span(row, from, -1, row == y)
		if row == y && r.right != "" && strings.HasSuffix(segment, r.right) {
			segment = strings.TrimRight(strings.TrimSuffix(segment, r.right), " ")
		}
		shown.WriteString(segment)
	}
	text := shown.String()

	var line string
	if r.tainted {
		// Completed, recalled or edited: only the screen knows the result, and
		// whatever was typed on top of it must be visible in it.
		if strings.TrimSpace(text) == "" || !inOrder(string(r.typed), text) {
			return "", false
		}
		line = text
	} else {
		typed := strings.TrimRight(string(r.typed), " ")
		shownText := strings.TrimRight(text, " ")
		// The text must be echoed. If the echo has not all arrived the part that
		// has is proof enough; if none of it has, or it differs, there is none.
		if shownText == "" || !strings.HasPrefix(typed, shownText) {
			return "", false
		}
		line = string(r.typed)
	}
	return clean(line)
}

// inOrder reports whether the characters of needle appear in order in text.
func inOrder(needle, text string) bool {
	rest := text
	for _, r := range needle {
		at := strings.IndexRune(rest, r)
		if at < 0 {
			return false
		}
		rest = rest[at+utf8.RuneLen(r):]
	}
	return true
}

func hasPrefix(p []byte, prefix string) bool {
	return len(p) >= len(prefix) && string(p[:len(prefix)]) == prefix
}

// escapeLength is how many bytes the escape sequence at the start of p takes:
// the ESC and what makes the key (a CSI or SS3 sequence, or one meta key).
func escapeLength(p []byte) int {
	if len(p) < 2 {
		return len(p)
	}
	switch p[1] {
	case '[':
		for index := 2; index < len(p); index++ {
			if p[index] >= 0x40 && p[index] <= 0x7e {
				return index + 1
			}
		}
		return len(p)
	case 'O':
		return min(3, len(p))
	}
	return 2
}

// Line is the command line being typed, as far as it can be suggested for: the
// text between the end of the prompt and the cursor, when the cursor is at the
// end of a line that began after a prompt that does not ask for a secret. It
// reads the screen, so a line recalled or completed counts too. Nothing is
// returned for a line that starts with a blank (the usual way to keep one out of
// history), one that has wrapped, text after the cursor that is not the
// right-hand prompt seen at the first key (a shell's own suggestion), or the
// alternate screen.
func (r *Recorder) Line(screen Screen) (string, bool) {
	line, _, ok := r.lineAt(screen)
	return line, ok
}

// LinePrompt is Line together with the text the line was typed after: the
// prompt, joined across the rows it wraps onto and with its trailing blanks
// dropped. Callers that must tell a shell's prompt from a program's are the
// ones that need it; it reports false exactly when Line does.
func (r *Recorder) LinePrompt(screen Screen) (line, prompt string, ok bool) {
	return r.lineAt(screen)
}

// lineAt reads the current line and its prompt in one pass; see Line.
func (r *Recorder) lineAt(screen Screen) (line, promptText string, ok bool) {
	if !r.armed || r.skip || r.multi || r.inPaste || screen.Alt() || screen.Scrolled() {
		return "", "", false
	}
	x, y := screen.Cursor()
	first := y
	for first > 0 && screen.Wrapped(first-1) && y-first < maxRows {
		first--
	}
	if first+r.promptRows != y || x < r.promptX || screen.Wrapped(y) {
		return "", "", false
	}
	if strings.TrimSpace(screen.Span(y, x, -1, false)) != r.rightShown {
		return "", "", false
	}
	prompt := screen.Span(y, 0, r.promptX, true)
	if strings.TrimSpace(prompt) == "" {
		return "", "", false
	}
	context := prompt
	var full strings.Builder
	for row := first; row < y; row++ {
		part := screen.Span(row, 0, -1, false)
		full.WriteString(part)
		context = part + " " + context
	}
	full.WriteString(prompt)
	if first > 0 {
		context = screen.Span(first-1, 0, -1, true) + " " + context
	}
	if PasswordPrompt(context) {
		return "", "", false
	}
	line = screen.Span(y, r.promptX, x, false)
	if strings.TrimSpace(line) == "" || line[0] == ' ' || line[0] == '\t' {
		return "", "", false
	}
	return line, full.String(), true
}
