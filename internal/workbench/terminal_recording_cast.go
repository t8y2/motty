package workbench

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

// Terminal recordings are exchanged as asciicast files (https://docs.asciinema.org/manual/asciicast/v2/):
// the first line is a JSON header, every following line is one
// [seconds, code, data] event. Motty writes version 2, which every player and
// converter reads, and plays versions 2 and 3 as well as the binary .mottyrec
// files earlier releases exported.

const (
	// recordingFileMaxBytes bounds a file opened for playback. Motty's own
	// recordings stay far below it; a longer file from another recorder is
	// refused instead of being held in memory.
	recordingFileMaxBytes  = 64 << 20
	recordingFileMaxEvents = 1 << 20
	// recordingMaxDuration keeps an absurd timestamp from overflowing the
	// duration arithmetic of the playhead.
	recordingMaxDuration = 24 * 365 * time.Hour

	castDefaultCols, castDefaultRows = 80, 24
	castMaxCols, castMaxRows         = 400, 200
)

// castHeader is the first line of an asciicast file, for both versions. A
// field another recorder adds is ignored; MottyTruncated is ours and says the
// recording stopped at its size limit.
type castHeader struct {
	Version        int       `json:"version"`
	Width          int       `json:"width,omitempty"`
	Height         int       `json:"height,omitempty"`
	Term           *castTerm `json:"term,omitempty"`
	Timestamp      int64     `json:"timestamp,omitempty"`
	Title          string    `json:"title,omitempty"`
	Env            *castEnv  `json:"env,omitempty"`
	MottyTruncated bool      `json:"motty_truncated,omitempty"`
}

type castTerm struct {
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Type string `json:"type,omitempty"`
}

type castEnv struct {
	Term string `json:"TERM,omitempty"`
}

// recordingFile is a recording read back for playback.
type recordingFile struct {
	cols, rows int
	title      string
	started    time.Time // zero when the file does not say
	truncated  bool
	events     []terminalRecordingEvent
}

func (file *recordingFile) duration() time.Duration {
	if len(file.events) == 0 {
		return 0
	}
	return file.events[len(file.events)-1].at
}

// encodeAsciicast writes the recording as an asciicast v2 file.
func encodeAsciicast(recording *terminalRecording, title string) []byte {
	header := castHeader{
		Version: 2, Width: recording.cols, Height: recording.rows,
		Title: title, Env: &castEnv{Term: "xterm-256color"}, MottyTruncated: recording.truncated,
	}
	if header.Width <= 0 || header.Height <= 0 {
		header.Width, header.Height = castDefaultCols, castDefaultRows
	}
	if !recording.wall.IsZero() {
		header.Timestamp = recording.wall.Unix()
	}
	var out bytes.Buffer
	out.Grow(recording.bytes + len(recording.events)*24 + 128)
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(header) // Encode ends the line

	// An event is split at 64 KiB, which can cut a character in two. A split
	// character is held for the next event of the same direction so each line
	// holds valid text, as a recorder that reads a stream does.
	var held [2][]byte
	var scratch []byte
	for _, event := range recording.events {
		slot := 0
		if event.direction == terminalRecordingOutput {
			slot = 1
		}
		data := event.data
		if len(held[slot]) > 0 {
			scratch = append(append(scratch[:0], held[slot]...), data...)
			data = scratch
			held[slot] = held[slot][:0]
		}
		if cut := incompleteTail(data); cut > 0 {
			held[slot] = append(held[slot], data[len(data)-cut:]...)
			data = data[:len(data)-cut]
		}
		if len(data) == 0 {
			continue
		}
		writeCastEvent(&out, encoder, event.at, event.direction, data)
	}
	// A character that never completed becomes U+FFFD, as the encoder does for
	// any invalid byte.
	for slot, rest := range held {
		if len(rest) == 0 {
			continue
		}
		direction := terminalRecordingInput
		if slot == 1 {
			direction = terminalRecordingOutput
		}
		var at time.Duration
		if count := len(recording.events); count > 0 {
			at = recording.events[count-1].at
		}
		writeCastEvent(&out, encoder, at, direction, rest)
	}
	return out.Bytes()
}

func writeCastEvent(out *bytes.Buffer, encoder *json.Encoder, at time.Duration, direction terminalRecordingDirection, data []byte) {
	micros := int64(max(at, 0) / time.Microsecond)
	var stamp [24]byte
	out.WriteByte('[')
	out.Write(strconv.AppendInt(stamp[:0], micros/1_000_000, 10))
	out.WriteByte('.')
	fraction := strconv.AppendInt(stamp[:0], micros%1_000_000, 10)
	for pad := len(fraction); pad < 6; pad++ {
		out.WriteByte('0')
	}
	out.Write(fraction)
	if direction == terminalRecordingInput {
		out.WriteString(`,"i",`)
	} else {
		out.WriteString(`,"o",`)
	}
	_ = encoder.Encode(string(data)) // ends with a newline, which closes the array below
	out.Truncate(out.Len() - 1)
	out.WriteString("]\n")
}

// incompleteTail is the length of a partial UTF-8 character that ends data, or
// zero when data ends on a character boundary or in bytes no character can
// complete.
func incompleteTail(data []byte) int {
	for back := 1; back <= min(utf8.UTFMax-1, len(data)); back++ {
		b := data[len(data)-back]
		if b < utf8.RuneSelf { // ASCII ends the search: nothing before it can be pending
			return 0
		}
		if b >= 0xC0 { // a lead byte
			if !utf8.FullRune(data[len(data)-back:]) {
				return back
			}
			return 0
		}
	}
	return 0
}

// decodeRecordingFile reads a recording in any format Motty has exported or
// that an asciicast recorder writes.
func decodeRecordingFile(data []byte) (*recordingFile, error) {
	switch {
	case len(data) > recordingFileMaxBytes:
		return nil, errors.New("recording is too large")
	case bytes.HasPrefix(data, terminalRecordingMagic[:]):
		return decodeLegacyRecording(data)
	default:
		return decodeAsciicast(data)
	}
}

func decodeLegacyRecording(data []byte) (*recordingFile, error) {
	file := &recordingFile{cols: castDefaultCols, rows: castDefaultRows}
	err := replayTerminalRecording(data, func(at time.Duration, direction terminalRecordingDirection, chunk []byte) error {
		file.events = append(file.events, terminalRecordingEvent{at: at, direction: direction, data: chunk})
		return nil
	})
	if err != nil {
		return nil, err
	}
	file.truncated = binary.LittleEndian.Uint16(data[10:12])&1 != 0
	return file, nil
}

func decodeAsciicast(data []byte) (*recordingFile, error) {
	line, rest, _ := bytes.Cut(data, []byte{'\n'})
	var header castHeader
	if err := json.Unmarshal(bytes.TrimSpace(line), &header); err != nil || header.Version != 2 && header.Version != 3 {
		return nil, errors.New("not a terminal recording")
	}
	file := &recordingFile{cols: castDefaultCols, rows: castDefaultRows, title: header.Title, truncated: header.MottyTruncated}
	if header.Timestamp > 0 {
		file.started = time.Unix(header.Timestamp, 0)
	}
	cols, rows := header.Width, header.Height
	if header.Term != nil {
		cols, rows = header.Term.Cols, header.Term.Rows
	}
	if cols > 0 {
		file.cols = min(cols, castMaxCols)
	}
	if rows > 0 {
		file.rows = min(rows, castMaxRows)
	}
	file.cols, file.rows = max(file.cols, minTerminalCols), max(file.rows, minTerminalRows)

	var at time.Duration
	for len(rest) > 0 {
		line, rest, _ = bytes.Cut(rest, []byte{'\n'})
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		var fields []json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil || len(fields) < 3 {
			return nil, fmt.Errorf("invalid event %d", len(file.events)+1)
		}
		var seconds float64
		var code string
		if json.Unmarshal(fields[0], &seconds) != nil || json.Unmarshal(fields[1], &code) != nil ||
			math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > recordingMaxDuration.Seconds() {
			return nil, fmt.Errorf("invalid event %d", len(file.events)+1)
		}
		stamp := time.Duration(math.Round(seconds*1e6)) * time.Microsecond
		if header.Version == 3 { // version 3 gives the time since the previous event
			stamp += at
		}
		at = max(at, min(stamp, recordingMaxDuration)) // a foreign file may step back a little: keep the order
		var direction terminalRecordingDirection
		switch code {
		case "o":
			direction = terminalRecordingOutput
		case "i":
			direction = terminalRecordingInput
		default: // markers, resizes and the exit status are not shown
			continue
		}
		var text string
		if err := json.Unmarshal(fields[2], &text); err != nil {
			return nil, fmt.Errorf("invalid event %d", len(file.events)+1)
		}
		if len(file.events) >= recordingFileMaxEvents {
			return nil, errors.New("recording has too many events")
		}
		file.events = append(file.events, terminalRecordingEvent{at: at, direction: direction, data: []byte(text)})
	}
	return file, nil
}
