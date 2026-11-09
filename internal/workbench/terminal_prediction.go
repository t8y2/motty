package workbench

import (
	"bytes"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

const (
	maxTerminalPredictionBytes  = 256
	maxTerminalPredictionChunks = 64
	terminalPredictionConfirm   = 3
	terminalPredictionTrust     = 2 * time.Second
	// The link decides whether a guess is shown, with the thresholds and the
	// hysteresis of mosh's prediction engine so that a delay hovering at an edge
	// does not flicker. A guess is drawn once echoes take longer than ShowAbove,
	// and withdrawn at or under HideAt: on a link faster than that the server's
	// answer is on screen before the guess would be noticed. The text keeps its
	// own color and carries no mark, so the echo that replaces it changes nothing.
	terminalPredictionShowAbove = 30 * time.Millisecond
	terminalPredictionHideAt    = 20 * time.Millisecond
	// A guess nobody confirmed is taken back after four echo delays, clamped to
	// this range, or after terminalPredictionUnknownStale before any echo was
	// timed. A server that never echoes (a vi-mode prompt in command mode) must
	// not leave text on screen.
	terminalPredictionStaleMin     = 500 * time.Millisecond
	terminalPredictionStaleMax     = 3 * time.Second
	terminalPredictionUnknownStale = time.Second
)

type terminalPredictionPhase uint8

const (
	terminalPredictionIdle terminalPredictionPhase = iota
	terminalPredictionProbe
	terminalPredictionActive
)

// terminalPrediction keeps a reversible display-only copy of recently sent
// printable input. It never writes predicted text into the emulator.
//
// Two things can make a guess visible. A prompt proof: the shell's own marker
// (OSC 133 B with this shell's token) says the cursor is on its input line and
// no control key has been typed since. Or echo trust: three runes the server
// echoed back, each checked against the emulator's screen after the output was
// applied, so a shell that colors or redraws the line still counts as long as
// the cells hold what was typed. Output that does not leave exactly the typed
// text before the cursor, or that moves the cursor elsewhere, takes the guess
// back; so does a control key, a stale guess, and any output when nothing is
// pending.
type terminalPrediction struct {
	phase terminalPredictionPhase
	// proven is set while the guess rests on a prompt proof rather than echoes.
	proven bool
	// blocked withdraws the prompt proof until the shell draws its next prompt:
	// a proven guess failed on this line, so the line is not what it seemed.
	blocked bool

	data   [maxTerminalPredictionBytes]byte
	length int
	cells  int
	// chunkEnd and chunkAt say, for each input event still pending, where it ends
	// in data and when it was sent, to time its echo.
	chunkEnd [maxTerminalPredictionChunks]uint16
	chunkAt  [maxTerminalPredictionChunks]time.Duration
	chunks   int

	// anchorX and anchorY are where the emulator's cursor stood, and prefix is
	// the hash of the row's cells before it, when the pending text was last
	// known to be what the server had: output is judged against them.
	anchorX, anchorY int
	prefix           uint64

	confirmed    int
	trustedUntil time.Duration
	// echo is the smoothed delay between a key and its echo, zero until one was
	// timed. It describes the link, so it outlives a reset, and so does shown,
	// the display decision drawn from it (see terminalPredictionShowAbove).
	echo  time.Duration
	shown bool

	text     string
	runs     [maxTerminalPredictionBytes]gfx.TextRun
	runCount int
}

func (prediction *terminalPrediction) reset() bool {
	changed := prediction.visible()
	prediction.phase = terminalPredictionIdle
	prediction.proven = false
	prediction.length = 0
	prediction.cells = 0
	prediction.chunks = 0
	prediction.confirmed = 0
	prediction.trustedUntil = 0
	prediction.text = ""
	prediction.runCount = 0
	return changed
}

// fail takes a guess back because the server did not do what it predicted. A
// guess made on a prompt proof also withdraws the proof for this line.
func (prediction *terminalPrediction) fail() {
	if prediction.proven {
		prediction.blocked = true
	}
	prediction.reset()
}

// newPrompt is the shell drawing its prompt again: a proof withdrawn on the
// last line is good for this one.
func (prediction *terminalPrediction) newPrompt() { prediction.blocked = false }

// visible reports whether text is pending in a state that may be shown, before
// the echo delay and age of the guess are considered.
func (prediction *terminalPrediction) visible() bool {
	return prediction.phase == terminalPredictionActive && prediction.length > 0
}

func (prediction *terminalPrediction) staleAfter() time.Duration {
	if prediction.echo == 0 {
		return terminalPredictionUnknownStale
	}
	return min(max(4*prediction.echo, terminalPredictionStaleMin), terminalPredictionStaleMax)
}

func (prediction *terminalPrediction) stale(now time.Duration) bool {
	return prediction.chunks > 0 && now-prediction.chunkAt[0] > prediction.staleAfter()
}

// visibleAt is whether the guess is on screen at now: pending in an active
// phase, not so old that the server evidently is not echoing it, and worth
// drawing at all on a link whose echoes do not come back at once. Until one echo
// was timed the link is unknown and the guess is drawn.
func (prediction *terminalPrediction) visibleAt(now time.Duration) bool {
	if !prediction.visible() || prediction.stale(now) {
		return false
	}
	return prediction.echo == 0 || prediction.shown
}

// expiry is how long a shown guess may still go unconfirmed, so that the owner
// can ask for the frame that takes it back.
func (prediction *terminalPrediction) expiry(now time.Duration) (time.Duration, bool) {
	if !prediction.visibleAt(now) {
		return 0, false
	}
	return prediction.staleAfter() - (now - prediction.chunkAt[0]), true
}

func (prediction *terminalPrediction) overlay(now time.Duration) (string, []gfx.TextRun, int, bool) {
	if !prediction.visibleAt(now) {
		return "", nil, 0, false
	}
	return prediction.text, prediction.runs[:prediction.runCount], prediction.cells, true
}

// input records accepted printable input; proof says the shell's prompt proof
// holds. It reports whether the overlay changed or may have.
func (prediction *terminalPrediction) input(screen *term.Terminal, input []byte, now time.Duration, proof bool) bool {
	if len(input) == 0 {
		return false
	}
	wasVisible := prediction.visibleAt(now)
	switch {
	case prediction.stale(now):
		prediction.fail()
	case prediction.phase == terminalPredictionActive && !prediction.proven && now > prediction.trustedUntil:
		prediction.reset()
	case prediction.proven && !proof:
		prediction.reset() // the line was left
	}
	if !predictionContextSafe(screen) {
		prediction.reset()
		return wasVisible
	}
	cells, _, ok := predictionMeasure(input)
	if !ok || prediction.length+len(input) > len(prediction.data) || prediction.chunks == len(prediction.chunkEnd) {
		prediction.reset()
		return wasVisible
	}
	cols, _ := screen.Size()
	cursor := screen.Cursor()
	if prediction.length > 0 && (cursor.X != prediction.anchorX || cursor.Y != prediction.anchorY) {
		prediction.reset() // something moved the cursor since the last output was judged
	}
	if cursor.X+prediction.cells+cells >= cols {
		prediction.reset()
		return wasVisible
	}
	if prediction.length == 0 {
		prediction.anchorX, prediction.anchorY = cursor.X, cursor.Y
		prediction.prefix = predictionRowHash(screen, cursor.Y, cursor.X)
	}
	switch {
	case proof && !prediction.blocked:
		prediction.phase, prediction.proven = terminalPredictionActive, true
	case prediction.phase == terminalPredictionIdle:
		prediction.phase = terminalPredictionProbe
	}
	copy(prediction.data[prediction.length:], input)
	prediction.length += len(input)
	prediction.cells += cells
	prediction.chunkEnd[prediction.chunks] = uint16(prediction.length)
	prediction.chunkAt[prediction.chunks] = now
	prediction.chunks++
	if prediction.phase == terminalPredictionActive {
		prediction.rebuild()
	}
	return wasVisible || prediction.visibleAt(now)
}

// reconcile judges remote output after the emulator applied it. The pending text
// is confirmed by the cells between where the cursor stood and where it is now:
// they must hold exactly a prefix of it, on the same row, with the row before
// the cursor unchanged. Output that left the cursor where it was (a title, a
// bell) says nothing and the guess waits; anything else takes it back.
func (prediction *terminalPrediction) reconcile(screen *term.Terminal, now time.Duration) bool {
	wasVisible := prediction.visibleAt(now)
	if prediction.length == 0 {
		prediction.reset()
		return wasVisible
	}
	cursor := screen.Cursor()
	if !cursor.Visible || cursor.Y != prediction.anchorY || cursor.X < prediction.anchorX ||
		predictionRowHash(screen, cursor.Y, prediction.anchorX) != prediction.prefix {
		prediction.fail()
		return wasVisible
	}
	if cursor.X == prediction.anchorX {
		return wasVisible
	}
	var scratch [maxTerminalPredictionBytes + utf8.UTFMax]byte
	echoed, ok := screen.AppendCellText(scratch[:0], cursor.Y, prediction.anchorX, cursor.X)
	if !ok || len(echoed) > prediction.length || !bytes.Equal(echoed, prediction.data[:len(echoed)]) {
		prediction.fail()
		return wasVisible
	}
	prediction.consume(len(echoed), now)
	prediction.anchorX = cursor.X
	prediction.prefix = predictionRowHash(screen, cursor.Y, cursor.X)
	prediction.confirmed += utf8.RuneCount(echoed)
	prediction.trustedUntil = now + terminalPredictionTrust
	if prediction.phase == terminalPredictionProbe && prediction.confirmed >= terminalPredictionConfirm {
		prediction.phase = terminalPredictionActive
	}
	if prediction.phase == terminalPredictionActive {
		prediction.rebuild()
	}
	return wasVisible || prediction.visibleAt(now)
}

// consume drops the first size bytes, now confirmed, and times the echo of the
// input that held the first.
func (prediction *terminalPrediction) consume(size int, now time.Duration) {
	if sample := now - prediction.chunkAt[0]; sample > 0 {
		if prediction.echo == 0 {
			prediction.echo = sample
		} else {
			prediction.echo += (sample - prediction.echo) / 4
		}
	}
	copy(prediction.data[:], prediction.data[size:prediction.length])
	prediction.length -= size
	for prediction.chunks > 0 && int(prediction.chunkEnd[0]) <= size {
		copy(prediction.chunkEnd[:], prediction.chunkEnd[1:prediction.chunks])
		copy(prediction.chunkAt[:], prediction.chunkAt[1:prediction.chunks])
		prediction.chunks--
	}
	for index := 0; index < prediction.chunks; index++ {
		prediction.chunkEnd[index] -= uint16(size)
	}
	cells := 0
	for offset := 0; offset < prediction.length; {
		r, width := utf8.DecodeRune(prediction.data[offset:prediction.length])
		cells += term.Width(r)
		offset += width
	}
	prediction.cells = cells
	prediction.judgeLink()
}

// judgeLink moves the display decision with the smoothed echo delay. A shown
// guess is not withdrawn while text is pending, so it never vanishes under the
// typist's fingers; the next consume after the last echo takes it back.
func (prediction *terminalPrediction) judgeLink() {
	switch {
	case prediction.echo > terminalPredictionShowAbove:
		prediction.shown = true
	case prediction.echo <= terminalPredictionHideAt && prediction.length == 0:
		prediction.shown = false
	}
}

func (prediction *terminalPrediction) rebuild() {
	prediction.text = string(prediction.data[:prediction.length])
	prediction.runCount = 0
	column := 0
	for offset := 0; offset < prediction.length; {
		r, size := utf8.DecodeRune(prediction.data[offset:prediction.length])
		width := term.Width(r)
		prediction.runs[prediction.runCount] = gfx.TextRun{
			Column: int32(column), Bytes: int32(size), Cells: int32(width),
		}
		prediction.runCount++
		offset += size
		column += width
	}
}

func predictionContextSafe(screen *term.Terminal) bool {
	cursor := screen.Cursor()
	return cursor.Visible && screen.ViewOffset() == 0 && !screen.Modes().AltScreen
}

// predictionRowHash identifies the text of the cells of row y before column to.
func predictionRowHash(screen *term.Terminal, y, to int) uint64 {
	var scratch [512]byte
	text, _ := screen.AppendCellText(scratch[:0], y, 0, to)
	hash := uint64(14695981039346656037)
	for _, b := range text {
		hash = (hash ^ uint64(b)) * 1099511628211
	}
	return hash
}

func predictionMeasure(input []byte) (cells, runes int, ok bool) {
	if !utf8.Valid(input) {
		return 0, 0, false
	}
	for len(input) > 0 {
		r, size := utf8.DecodeRune(input)
		width := term.Width(r)
		if r < 0x20 || r == 0x7f || width <= 0 {
			return 0, 0, false
		}
		cells += width
		runes++
		input = input[size:]
	}
	return cells, runes, true
}
