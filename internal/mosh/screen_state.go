package mosh

import (
	"encoding/base64"
	"maps"
	"slices"

	"motty/internal/term"
)

// screenState is one complete host-side terminal state: the screen a host diff
// applies to, the bell count and clipboard writes it carried, and the newest
// echo ack it named. The transport keeps one for every received state, as the C
// client keeps a Terminal::Complete per received state, because a diff is
// relative to the particular state its old_num names and not to whichever state
// arrived last.
//
// A state is never changed once it is in the queue: every diff is applied to a
// clone. That makes the queue safe to reference from more than one place (the
// presented state and the throwaway floor can name the same state).
type screenState struct {
	screen    *term.Terminal
	bell      uint64
	clipboard map[string]string // last text per OSC 52 selection
	echoAck   uint64
}

// newScreenState is the blank state a session starts from, at the size the
// remote is told about first.
func newScreenState(cols, rows int) *screenState {
	state := &screenState{screen: term.New(cols, rows, 0)}
	state.install()
	return state
}

// install points the screen's owner hooks at this state. A cloned terminal has
// no hooks of its own, so every state installs them after cloning.
func (state *screenState) install() {
	state.screen.SetBell(func() { state.bell++ })
	state.screen.SetClipboardWrite(func(write term.ClipboardWrite) {
		if state.clipboard == nil {
			state.clipboard = make(map[string]string)
		}
		state.clipboard[write.Selection] = write.Text
	})
}

func (state *screenState) clone() hostState {
	next := &screenState{
		screen:  state.screen.Clone(),
		bell:    state.bell,
		echoAck: state.echoAck,
	}
	if state.clipboard != nil {
		next.clipboard = maps.Clone(state.clipboard)
	}
	next.install()
	return next
}

// apply applies one host message and returns the echo ack it named. The
// instructions are applied in order, as the C receiver does: text at the size
// the state has when it arrives, then a resize for the instructions after it.
func (state *screenState) apply(diff []byte) (uint64, bool) {
	instructions, err := UnmarshalHostMessage(diff)
	if err != nil {
		return 0, false
	}
	for i := range instructions {
		instruction := &instructions[i]
		switch {
		case len(instruction.Hoststring) > 0:
			state.screen.Write(instruction.Hoststring)
		case instruction.Width > 0 || instruction.Height > 0:
			// The remote emulator owns the size; a resize it sends changes the
			// state the next diff will be applied to.
			cols, rows := state.screen.Size()
			if instruction.Width > 0 {
				cols = int(instruction.Width)
			}
			if instruction.Height > 0 {
				rows = int(instruction.Height)
			}
			state.screen.Resize(cols, rows)
		}
		if instruction.EchoAckNum >= 0 && uint64(instruction.EchoAckNum) > state.echoAck {
			state.echoAck = uint64(instruction.EchoAckNum)
		}
	}
	return state.echoAck, true
}

// diffFrom returns what turns the older state into this one: bells and
// clipboard writes first, then the terminal update. older is nil for the first
// state the owner sees, which repaints the screen in full.
func (state *screenState) diffFrom(older hostState) []byte {
	var previous *screenState
	if older != nil {
		previous, _ = older.(*screenState)
	}
	var out []byte
	if previous == nil {
		if state.bell > 0 {
			out = append(out, '\a')
		}
	} else if state.bell != previous.bell {
		out = append(out, '\a')
	}
	for _, selection := range slices.Sorted(maps.Keys(state.clipboard)) {
		text := state.clipboard[selection]
		if previous != nil && previous.clipboard[selection] == text {
			continue
		}
		out = appendClipboard(out, selection, text)
	}
	var screen *term.Terminal
	if previous != nil {
		screen = previous.screen
	}
	return append(out, term.ScreenDiff(screen, state.screen)...)
}

// appendClipboard writes one OSC 52 clipboard replacement. The text was
// validated by the emulator's parser; base64 keeps the control bytes out of the
// stream.
func appendClipboard(out []byte, selection, text string) []byte {
	out = append(out, "\x1b]52;"...)
	out = append(out, selection...)
	out = append(out, ';')
	out = base64.StdEncoding.AppendEncode(out, []byte(text))
	return append(out, '\a')
}
