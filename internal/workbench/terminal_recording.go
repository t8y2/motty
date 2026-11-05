package workbench

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/vivaui/viva/app"
)

const (
	terminalRecordingVersion   = uint16(1)
	terminalRecordingMaxBytes  = 8 << 20
	terminalRecordingMaxEvents = 1 << 16
	terminalRecordingMaxChunk  = 64 << 10
)

var terminalRecordingMagic = [8]byte{'G', 'O', 'P', 'R', 'E', 'C', '\r', '\n'}

type terminalRecordingDirection uint8

const (
	terminalRecordingInput terminalRecordingDirection = iota + 1
	terminalRecordingOutput
)

type terminalRecordingEvent struct {
	at        time.Duration
	direction terminalRecordingDirection
	data      []byte
}

type terminalRecording struct {
	started   time.Duration
	events    []terminalRecordingEvent
	bytes     int
	active    bool
	truncated bool
}

func (recording *terminalRecording) start(now time.Duration) {
	*recording = terminalRecording{started: now, active: true}
}

func (recording *terminalRecording) stop() { recording.active = false }

func (recording *terminalRecording) append(now time.Duration, direction terminalRecordingDirection, data []byte) {
	if !recording.active || len(data) == 0 {
		return
	}
	at := max(now-recording.started, 0)
	for len(data) > 0 {
		if len(recording.events) >= terminalRecordingMaxEvents || recording.bytes >= terminalRecordingMaxBytes {
			recording.active = false
			recording.truncated = true
			return
		}
		length := min(len(data), terminalRecordingMaxChunk, terminalRecordingMaxBytes-recording.bytes)
		if length == 0 {
			recording.active = false
			recording.truncated = true
			return
		}
		chunk := append([]byte(nil), data[:length]...)
		recording.events = append(recording.events, terminalRecordingEvent{at: at, direction: direction, data: chunk})
		recording.bytes += length
		data = data[length:]
	}
}

func encodeTerminalRecording(recording *terminalRecording) []byte {
	flags := uint16(0)
	if recording.truncated {
		flags = 1
	}
	data := make([]byte, 16, 16+recording.bytes+len(recording.events)*16)
	copy(data, terminalRecordingMagic[:])
	binary.LittleEndian.PutUint16(data[8:10], terminalRecordingVersion)
	binary.LittleEndian.PutUint16(data[10:12], flags)
	binary.LittleEndian.PutUint32(data[12:16], uint32(len(recording.events)))
	var header [16]byte
	for _, event := range recording.events {
		binary.LittleEndian.PutUint64(header[0:8], uint64(max(event.at, 0)/time.Microsecond))
		header[8] = byte(event.direction)
		header[9], header[10], header[11] = 0, 0, 0
		binary.LittleEndian.PutUint32(header[12:16], uint32(len(event.data)))
		data = append(data, header[:]...)
		data = append(data, event.data...)
	}
	return data
}

func replayTerminalRecording(data []byte, apply func(time.Duration, terminalRecordingDirection, []byte) error) error {
	if len(data) < 16 || string(data[:8]) != string(terminalRecordingMagic[:]) {
		return errors.New("terminal recording: invalid header")
	}
	if version := binary.LittleEndian.Uint16(data[8:10]); version != terminalRecordingVersion {
		return fmt.Errorf("terminal recording: unsupported version %d", version)
	}
	if flags := binary.LittleEndian.Uint16(data[10:12]); flags&^uint16(1) != 0 {
		return fmt.Errorf("terminal recording: unsupported flags %#x", flags)
	}
	count := int(binary.LittleEndian.Uint32(data[12:16]))
	if count > terminalRecordingMaxEvents {
		return errors.New("terminal recording: too many events")
	}
	offset := 16
	payload := 0
	previous := time.Duration(0)
	for index := 0; index < count; index++ {
		if len(data)-offset < 16 {
			return errors.New("terminal recording: truncated event")
		}
		header := data[offset : offset+16]
		offset += 16
		if header[9] != 0 || header[10] != 0 || header[11] != 0 {
			return errors.New("terminal recording: nonzero reserved bytes")
		}
		atMicros := binary.LittleEndian.Uint64(header[0:8])
		if atMicros > uint64(^uint64(0)>>1)/uint64(time.Microsecond) {
			return errors.New("terminal recording: timestamp overflow")
		}
		at := time.Duration(atMicros) * time.Microsecond
		if at < previous {
			return errors.New("terminal recording: timestamps are not monotonic")
		}
		previous = at
		direction := terminalRecordingDirection(header[8])
		if direction != terminalRecordingInput && direction != terminalRecordingOutput {
			return errors.New("terminal recording: invalid direction")
		}
		length := int(binary.LittleEndian.Uint32(header[12:16]))
		if length > terminalRecordingMaxChunk || length > len(data)-offset || payload+length > terminalRecordingMaxBytes {
			return errors.New("terminal recording: invalid payload length")
		}
		if err := apply(at, direction, data[offset:offset+length]); err != nil {
			return err
		}
		offset += length
		payload += length
	}
	if offset != len(data) {
		return errors.New("terminal recording: trailing data")
	}
	return nil
}

func (state *State) toggleTerminalRecording(session *Session) {
	if session == nil || session.term == nil || session.term.shell == nil {
		return
	}
	recording := &session.term.recording
	if recording.active {
		recording.stop()
		state.status = state.messages.RecordingStopped
		return
	}
	recording.start(state.now())
	state.status = state.messages.RecordingStarted
}

func (state *State) exportTerminalRecording(session *Session) {
	if session == nil || session.term == nil || len(session.term.recording.events) == 0 {
		return
	}
	path, err := state.options.SaveFile(app.SaveFileOptions{
		Title:            state.messages.RecordingExport,
		SuggestedName:    recordingName(session.Title, time.Now()),
		DefaultExtension: "mottyrec",
		Filters:          []app.FileFilter{{Name: "Motty terminal recording", Pattern: "*.mottyrec"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err != nil {
		state.showActionFailure(state.messages.RecordingExport, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	if err = writePrivateAtomic(path, encodeTerminalRecording(&session.term.recording)); err != nil {
		state.showFileSaveFailure(state.messages.RecordingExport, err)
		return
	}
	state.status = fmt.Sprintf(state.messages.RecordingExportedFmt, path)
}

func recordingName(title string, at time.Time) string {
	return exportFileName(title, "terminal", "mottyrec", at)
}
