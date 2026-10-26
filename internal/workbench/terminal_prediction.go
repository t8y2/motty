package workbench

import (
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

const (
	maxTerminalPredictionBytes = 256
	terminalPredictionConfirm  = 3
	terminalPredictionTrust    = 2 * time.Second
)

type terminalPredictionPhase uint8

const (
	terminalPredictionIdle terminalPredictionPhase = iota
	terminalPredictionProbe
	terminalPredictionActive
)

// terminalPrediction keeps a reversible display-only copy of recently sent
// printable input. It never writes predicted text into the emulator. A remote
// context must first echo three consecutive runes byte-for-byte, and any other
// output or control input drops that trust before more text can be shown.
type terminalPrediction struct {
	phase        terminalPredictionPhase
	data         [maxTerminalPredictionBytes]byte
	length       int
	matched      int
	cells        int
	confirmed    int
	trustedUntil time.Duration
	text         string
	runs         [maxTerminalPredictionBytes]ui.TextRun
	runCount     int
}

func (prediction *terminalPrediction) reset() bool {
	changed := prediction.visible()
	prediction.phase = terminalPredictionIdle
	prediction.length = 0
	prediction.matched = 0
	prediction.cells = 0
	prediction.confirmed = 0
	prediction.trustedUntil = 0
	prediction.text = ""
	prediction.runCount = 0
	return changed
}

func (prediction *terminalPrediction) visible() bool {
	return prediction.phase == terminalPredictionActive && prediction.length > 0
}

func (prediction *terminalPrediction) overlay() (string, []ui.TextRun, int, bool) {
	if !prediction.visible() {
		return "", nil, 0, false
	}
	return prediction.text, prediction.runs[:prediction.runCount], prediction.cells, true
}

// input records accepted printable input. Probes are deliberately invisible;
// only an already verified, recent echo context may produce an overlay.
func (prediction *terminalPrediction) input(screen *term.Terminal, input []byte, now time.Duration) bool {
	if len(input) == 0 {
		return false
	}
	if prediction.phase == terminalPredictionActive && now > prediction.trustedUntil {
		prediction.reset()
	}
	if !predictionContextSafe(screen) {
		return prediction.reset()
	}
	cells, _, ok := predictionMeasure(input)
	if !ok || prediction.length+len(input) > len(prediction.data) {
		return prediction.reset()
	}
	cols, _ := screen.Size()
	cursor := screen.Cursor()
	if cursor.X+prediction.cells+cells >= cols {
		return prediction.reset()
	}
	if prediction.phase == terminalPredictionIdle {
		prediction.phase = terminalPredictionProbe
	}
	wasVisible := prediction.visible()
	copy(prediction.data[prediction.length:], input)
	prediction.length += len(input)
	prediction.cells += cells
	if prediction.phase == terminalPredictionActive {
		prediction.rebuild()
	}
	return wasVisible || prediction.visible()
}

// observe reconciles raw remote output with pending input before the output is
// committed to the emulator. Exact prefixes confirm predictions; anything else
// invalidates them without changing what the remote terminal actually draws.
func (prediction *terminalPrediction) observe(output []byte, now time.Duration) bool {
	if len(output) == 0 {
		return false
	}
	wasVisible := prediction.visible()
	if prediction.length == 0 {
		prediction.reset()
		return wasVisible
	}

	remaining := prediction.data[prediction.matched:prediction.length]
	matched := 0
	for matched < len(output) && matched < len(remaining) && output[matched] == remaining[matched] {
		matched++
	}
	if matched < len(output) && matched < len(remaining) {
		prediction.reset()
		return wasVisible
	}
	prediction.matched += matched
	prediction.consumeCompleteRunes(now)

	// Output beyond the exact pending echo means the remote context changed.
	if matched < len(output) {
		prediction.reset()
		return wasVisible
	}
	if prediction.length == 0 && prediction.phase == terminalPredictionProbe && prediction.confirmed >= terminalPredictionConfirm {
		prediction.phase = terminalPredictionActive
		prediction.trustedUntil = now + terminalPredictionTrust
	}
	return wasVisible || prediction.visible()
}

func (prediction *terminalPrediction) consumeCompleteRunes(now time.Duration) {
	consumed := 0
	cells := 0
	runes := 0
	for consumed < prediction.matched {
		input := prediction.data[consumed:prediction.matched]
		if !utf8.FullRune(input) {
			break
		}
		r, size := utf8.DecodeRune(input)
		if r == utf8.RuneError && size == 1 {
			break
		}
		consumed += size
		cells += term.Width(r)
		runes++
	}
	if consumed == 0 {
		return
	}
	copy(prediction.data[:], prediction.data[consumed:prediction.length])
	prediction.length -= consumed
	prediction.matched -= consumed
	prediction.cells -= cells
	prediction.confirmed += runes
	prediction.trustedUntil = now + terminalPredictionTrust
	if prediction.phase == terminalPredictionActive {
		prediction.rebuild()
	}
}

func (prediction *terminalPrediction) rebuild() {
	prediction.text = string(prediction.data[:prediction.length])
	prediction.runCount = 0
	column := 0
	for offset := 0; offset < prediction.length; {
		r, size := utf8.DecodeRune(prediction.data[offset:prediction.length])
		width := term.Width(r)
		prediction.runs[prediction.runCount] = ui.TextRun{
			Column: int32(column), Bytes: int32(size), Cells: int32(width), Underline: true,
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
