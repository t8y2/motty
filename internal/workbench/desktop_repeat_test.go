package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
)

// sentKeys is what the fake remote was given, in order.
func (s *fileDesktop) sentKeys() []desktop.KeyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]desktop.KeyEvent(nil), s.keys...)
}

// TestAHeldKeyReachesTheDesktopAsARepeat pins the press, the repeat and the
// release of a key held down. An RDP server repeats only a key the client says
// is already down, so the repeat is a press of its own, not silence.
func TestAHeldKeyReachesTheDesktopAsARepeat(t *testing.T) {
	remote := newFileDesktop()
	state, session := fileDesktopState(t, remote, false, Options{})
	hit := hitTerminalList.At(int(session.ID))
	send := func(event gfx.KeyEvent) {
		t.Helper()
		event.Target = hit
		if result := state.Key(event); !result.Handled {
			t.Fatalf("key %+v was not handled", event)
		}
	}
	send(gfx.KeyEvent{Text: "a"})
	send(gfx.KeyEvent{Text: "a", Repeat: true})
	send(gfx.KeyEvent{Text: "a", Repeat: true})
	send(gfx.KeyEvent{Text: "a", Release: true})

	want := []desktop.KeyEvent{
		{Rune: 'a', Down: true},
		{Rune: 'a', Down: true, Repeat: true},
		{Rune: 'a', Down: true, Repeat: true},
		{Rune: 'a'},
	}
	if got := remote.sentKeys(); !slices.Equal(got, want) {
		t.Fatalf("remote keys = %+v, want %+v", got, want)
	}
}
