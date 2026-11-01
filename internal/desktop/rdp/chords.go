package rdp

// The system chords a host keeps for itself, and the state that names one from
// the keys seen so far. This file is plain logic so every platform builds and
// tests it; the Windows hook in hook_windows.go feeds it, the other platforms
// have no hook to feed.

// Virtual-key codes of the keys the chord rules name. Set-1 scancodes travel
// with the events; these identify the keys.
const (
	vkTab    = 0x09
	vkEscape = 0x1B
	vkLWin   = 0x5B
	vkRWin   = 0x5C
)

// chordState tracks which modifiers are held, from the key events seen, so a
// chord can be named the moment its last key arrives. Counts rather than
// booleans, because a keyboard has two of each modifier and either may be the
// one held. Only the hook's own thread touches it.
type chordState struct {
	win, alt, ctrl int
}

// capture says whether this event belongs to the host's system chord and is
// taken for the remote, and records the modifier state either way, so the
// state follows what the user is really holding even for keys the hook eats.
func (s *chordState) capture(vk uint32, down bool) bool {
	take := false
	switch vk {
	case vkLWin, vkRWin:
		take = true // the Win key alone opens the host's menu; taken either way
	default:
		switch {
		case s.win > 0: // every Win chord: Win+R, Win+L, Win+D, ...
			take = true
		case s.alt > 0 && (vk == vkTab || vk == vkEscape):
			take = true // Alt+Tab switches host tasks, Alt+Esc cycles them
		case s.ctrl > 0 && vk == vkEscape:
			take = true // Ctrl+Esc opens the host menu, Ctrl+Shift+Esc its manager
		}
	}
	delta := -1
	if down {
		delta = 1
	}
	switch vk {
	case vkLWin, vkRWin:
		s.win = max(s.win+delta, 0)
	case 0xA0, 0xA1: // Shift, not part of any chord above
	case 0xA2, 0xA3: // Control, left and right
		s.ctrl = max(s.ctrl+delta, 0)
	case 0xA4, 0xA5: // Alt (Menu), left and right
		s.alt = max(s.alt+delta, 0)
	}
	return take
}
