package rdp

import "testing"

// A capture run: one key event at a time, in the order a hand would type them.
type chordStep struct {
	vk   uint32
	down bool
}

func downUp(vk uint32) []chordStep { return []chordStep{{vk, true}, {vk, false}} }

// chord builds the steps of a held modifier with keys typed inside it.
func chord(mod uint32, keys ...uint32) []chordStep {
	var steps []chordStep
	steps = append(steps, chordStep{mod, true})
	for _, key := range keys {
		steps = append(steps, chordStep{key, true}, chordStep{key, false})
	}
	return append(steps, chordStep{mod, false})
}

// takeFeed returns which of the steps the capture takes for the remote.
func takeFeed(steps []chordStep) (taken []bool, final chordState) {
	var state chordState
	for _, step := range steps {
		taken = append(taken, state.capture(step.vk, step.down))
	}
	return taken, state
}

func TestChordCaptureTakesTheSystemChords(t *testing.T) {
	// taken is what the hook forwards and hides from the host: the chord's
	// keys, and the Win key itself; the modifier travels as its own key event
	// through the window, as it does today.
	tests := []struct {
		name  string
		steps []chordStep
		want  []bool
	}{
		{"win alone", downUp(vkLWin), []bool{true, true}},
		{"right win alone", downUp(vkRWin), []bool{true, true}},
		{"win+r", chord(vkLWin, 'R'), []bool{true, true, true, true}},
		{"win+l", chord(vkRWin, 'L'), []bool{true, true, true, true}},
		{"alt+tab", chord(0xA4, vkTab), []bool{false, true, true, false}},
		{"alt+esc", chord(0xA4, vkEscape), []bool{false, true, true, false}},
		{"ctrl+esc", chord(0xA2, vkEscape), []bool{false, true, true, false}},
		{"ctrl+shift+esc", []chordStep{
			{0xA2, true}, {0xA0, true}, {vkEscape, true}, {vkEscape, false}, {0xA0, false}, {0xA2, false},
		}, []bool{false, false, true, true, false, false}},
		// Plain typing and the terminal's own chords stay with the host.
		{"plain a", downUp('A'), []bool{false, false}},
		{"ctrl+c", chord(0xA2, 'C'), []bool{false, false, false, false}},
		{"alt+f4 closes the window", chord(0xA4, 0x73), []bool{false, false, false, false}},
		{"tab alone", downUp(vkTab), []bool{false, false}},
		{"esc alone", downUp(vkEscape), []bool{false, false}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, state := takeFeed(test.steps)
			if len(got) != len(test.want) {
				t.Fatalf("%d steps produced %d results", len(test.steps), len(got))
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("step %d (vk 0x%02X): taken %v, want %v",
						index, test.steps[index].vk, got[index], test.want[index])
				}
			}
			if state.win != 0 || state.alt != 0 || state.ctrl != 0 {
				t.Fatalf("modifiers left held: %+v", state)
			}
		})
	}
}

// A lost key-up (the session hid the window mid-chord) must not leave the
// state wedged: the counts never go negative and the chord keeps naming
// itself.
func TestChordStateSurvivesALostKeyUp(t *testing.T) {
	var state chordState
	if !state.capture(vkLWin, true) {
		t.Fatal("win down was not taken")
	}
	// No win up: the next win down joins the count, and one release still
	// leaves it held, so the chord keeps working.
	if !state.capture('D', true) {
		t.Fatal("win+d was not taken")
	}
	if !state.capture('D', false) {
		t.Fatal("d up was not taken")
	}
	if !state.capture(vkLWin, true) {
		t.Fatal("second win down was not taken")
	}
	state.capture(vkLWin, false)
	if state.win != 1 {
		t.Fatalf("held win = %d after two downs and one up", state.win)
	}
}

// Both of a modifier's keys count once each; releasing one leaves the other.
func TestChordStateCountsBothSides(t *testing.T) {
	var state chordState
	state.capture(0xA2, true) // left control
	state.capture(0xA3, true) // right control
	if state.ctrl != 2 {
		t.Fatalf("held control = %d, want 2", state.ctrl)
	}
	state.capture(0xA2, false)
	if state.ctrl != 1 {
		t.Fatalf("held control = %d after one release, want 1", state.ctrl)
	}
	if !state.capture(vkEscape, true) {
		t.Fatal("ctrl+esc was not taken with the right control held")
	}
}
