package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// The badge beside an inline suggestion names the key that accepts it, as
// Warp's autosuggestion hint does, and opens a menu that changes the binding.
// The badge is a part of the terminal control: the grid paints it, and the
// control's part routing turns presses on it into the menu.
//
// Ownership: the suggestion and the keys sent to the shell are the terminal
// application's; the menu, the hit model and the keybinding table are Viva's
// components, used as the terminal's other overlays use them.

const (
	ghostHintGap     = float32(8)  // between the suggestion and the badge
	ghostHintPad     = float32(6)  // inside the badge, before the text
	ghostHintAfter   = float32(5)  // between the text and the chevron
	ghostHintChevron = float32(9)  // the dropdown chevron
	ghostHintMin     = float32(40) // the narrowest a keycap and the chevron read well
	ghostHintMinCell = float32(14) // a shorter row cannot hold a readable badge
)

// paintGhostHint draws the badge after a painted suggestion and returns its
// rectangle. It paints nothing when the hint is off, the binding is gone, the
// program owns the keyboard, or the row has no room before its right prompt.
func (state *State) paintGhostHint(canvas *gfx.Canvas, session *Session, geometry terminalGeometry, cursor term.Cursor, used, limit int) (gfx.Rect, bool) {
	if state.history.acceptHintOff || state.cellHeight < ghostHintMinCell {
		return gfx.Rect{}, false
	}
	// A program that asked for win32 input records still leaves the terminal
	// free to accept its own suggestion: the mode is how a local console shell
	// receives keys as console records, not a claim on this key.
	if session.term == nil || session.term.screen.KeyboardFlags() != 0 {
		return gfx.Rect{}, false
	}
	// Embedded terminals, such as the CLI sign-in view, have no control part
	// routing; a badge there would be a button that does nothing.
	if _, ok := state.sessionByIDPointer(session.ID); !ok {
		return gfx.Rect{}, false
	}
	session.term.pageIdentity(session.ID)
	label := state.ghostHintLabel(session.term)
	if label == "" {
		return gfx.Rect{}, false
	}
	height := min(state.cellHeight, 18)
	if height < 12 {
		return gfx.Rect{}, false
	}
	size := min(float32(12), height-3)
	textWidth := canvas.TextWidthWeight(label, size, gfx.WeightMedium)
	width := max(ghostHintPad+textWidth+ghostHintAfter+ghostHintChevron+ghostHintPad, ghostHintMin)
	x := geometry.left + float32(cursor.X+used)*state.cellWidth + ghostHintGap
	right := geometry.left + float32(limit)*state.cellWidth
	if x+width > right {
		return gfx.Rect{}, false
	}
	y := geometry.top + float32(cursor.Y)*state.cellHeight + (state.cellHeight-height)/2
	pill := gfx.Rect{X: x, Y: y, Width: width, Height: height}

	id := session.term.parts.At(terminalPartGhostHint)
	lit := canvas.Hovered() == id || state.ghostMenu.open && state.ghostMenu.session == session.ID
	// The badge is as quiet as the suggestion it belongs to: the same dim
	// foreground, with no fill of its own. Pointer or an open menu brightens it.
	ink := withAlpha(state.termPal.termFG, 0.42)
	line := withAlpha(state.termPal.termFG, 0.32)
	fill := gfx.Color{}
	if lit {
		ink = withAlpha(state.termPal.termFG, 0.85)
		line = withAlpha(state.termPal.termFG, 0.55)
		fill = withAlpha(state.termPal.termFG, 0.08)
	}
	radius := min(height/2, float32(4))
	if fill.Alpha > 0 {
		canvas.Rounded(pill, radius, fill)
	}
	canvas.RoundedBorder(pill, radius, 1, line, gfx.Color{})
	chevron := gfx.Rect{
		X: pill.X + pill.Width - ghostHintPad - ghostHintChevron, Y: pill.Y + (height-ghostHintChevron)/2,
		Width: ghostHintChevron, Height: ghostHintChevron,
	}
	text := gfx.Rect{X: pill.X + ghostHintPad, Y: pill.Y, Width: max(chevron.X-ghostHintAfter-(pill.X+ghostHintPad), 0), Height: height}
	canvas.LabelCenter(label, text, gfx.TextStyle{Size: size, Weight: gfx.WeightMedium, Color: ink})
	// The reference's control ends in a small solid caret, not a stroked chevron.
	cx := chevron.X + chevron.Width/2
	top := chevron.Y + (chevron.Height-5)/2
	canvas.AppendTriangle(gfx.Triangle{Points: [3]gfx.Point{
		{X: cx - 3.5, Y: top}, {X: cx + 3.5, Y: top}, {X: cx, Y: top + 4.5},
	}, Color: ink})
	session.term.hintRect = pill
	return pill, true
}

// ghostHintLabel is the badge text for a terminal: the bound key's keycaps,
// built again only when the binding changes. An unbound key has no badge.
func (state *State) ghostHintLabel(t *terminal) string {
	chord := state.shortcuts.chords.Chord(shortcutAcceptSuggestion)
	if !chord.Valid() {
		return ""
	}
	if t.hintChord != chord {
		var scratch [5]string
		t.hintChord = chord
		t.hintLabel = strings.Join(chord.Keycaps(scratch[:0]), "")
	}
	return t.hintLabel
}

// declareGhostHintHit declares the badge part after the terminal's own region,
// so a press on it reaches the control's part routing instead of the grid.
func (state *State) declareGhostHintHit(canvas *gfx.Canvas, session *Session, rect gfx.Rect) {
	t := session.term
	if t == nil || rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	label := state.messages.Completion.AcceptHintBadge
	canvas.HitWithOptions(t.parts.At(terminalPartGhostHint), rect, label, gfx.HitOptions{
		Role: gfx.RoleButton, SkipTab: true, Cursor: gfx.PointerCursorHand,
		Value: t.hintLabel, Hint: label, Expandable: true,
		Expanded: state.ghostMenu.open && state.ghostMenu.session == session.ID,
	})
}

// terminalGhostHintPointer handles presses on the badge: a primary press opens
// the key menu when it is released (a button activates on release, and opening
// on the press would put the new backdrop under the release), and a secondary
// one is the terminal's own right-click, since the badge sits over the grid.
func (state *State) terminalGhostHintPointer(session *Session, event gfx.PointerEvent) gfx.EventResult {
	switch event.Kind {
	case gfx.PointerDown:
		switch event.Button {
		case gfx.PointerButtonPrimary, gfx.PointerButtonNone:
			return gfx.EventResult{Handled: true}
		case gfx.PointerButtonSecondary:
			return state.terminalPointer(session, event)
		}
		return gfx.EventResult{Handled: true}
	case gfx.PointerUp:
		if event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone {
			state.openGhostMenu(session)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}
