package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/keymap"
)

// ghostHintID is the badge part of a session's terminal control.
func ghostHintID(session *Session) gfx.ID {
	return terminalControlParts(session.ID).At(terminalPartGhostHint)
}

// paintGhostGrid paints one terminal grid as a focused control, which is what
// the ghost suggestion and its badge need.
func paintGhostGrid(t *testing.T, state *State, session *Session) (*gfx.Canvas, gfx.Hit, bool) {
	t.Helper()
	var canvas gfx.Canvas
	control := terminalTestHit(state, session)
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, control, true)
	state.paintGrid(&canvas, session, state.layout())
	hit, ok := hitByID(&canvas, ghostHintID(session))
	return &canvas, hit, ok
}

// ghostHintWorld is a history world with a suggestion showing: the user typed
// the start of a remembered command and the shell is waiting.
func ghostHintWorld(t *testing.T) *historyWorld {
	t.Helper()
	h := ghostWorld(t, "git checkout main")
	h.prompt("$ ")
	h.type_("git ch")
	if rest, ok := h.state.commandGhost(&h.state.sessions[0]); !ok || rest == "" {
		t.Fatal("no suggestion to point at")
	}
	return h
}

func TestGhostHintBadgeIsDrawnAfterTheSuggestion(t *testing.T) {
	h := ghostHintWorld(t)
	session := &h.state.sessions[0]
	canvas, hit, ok := paintGhostGrid(t, h.state, session)
	if !ok {
		t.Fatal("the badge was not painted beside the suggestion")
	}
	if hit.Role != gfx.RoleButton || hit.Value == "" || !hit.Expandable {
		t.Fatalf("badge hit = %+v", hit)
	}
	geometry := h.state.layout()
	cursor := session.term.screen.Cursor()
	left := geometry.left + float32(cursor.X)*h.state.cellWidth
	if hit.Bounds.X <= left {
		t.Fatalf("badge starts at %.1f, not after the cursor cell at %.1f", hit.Bounds.X, left)
	}
	if right := geometry.area.X + geometry.area.Width; hit.Bounds.X+hit.Bounds.Width > right {
		t.Fatalf("badge ends at %.1f, past the terminal's %.1f", hit.Bounds.X+hit.Bounds.Width, right)
	}
	// The badge's region is declared after the terminal's own, so a press on it
	// reaches the part rather than the grid.
	control := terminalTestHit(h.state, session)
	controlAt, badgeAt := -1, -1
	for index, region := range canvas.Hits() {
		switch region.ID {
		case control:
			controlAt = index
		case ghostHintID(session):
			badgeAt = index
		}
	}
	if controlAt < 0 || badgeAt <= controlAt {
		t.Fatalf("control hit at %d, badge hit at %d: the grid would take the press", controlAt, badgeAt)
	}
}

func TestGhostHintBadgeHidesWhenItIsOffOrUnbound(t *testing.T) {
	h := ghostHintWorld(t)
	session := &h.state.sessions[0]
	h.state.history.acceptHintOff = true
	if _, _, ok := paintGhostGrid(t, h.state, session); ok {
		t.Fatal("the badge is painted while its setting is off")
	}
	h.state.history.acceptHintOff = false
	h.state.shortcuts.unbind(shortcutAcceptSuggestion)
	if _, _, ok := paintGhostGrid(t, h.state, session); ok {
		t.Fatal("the badge is painted without a key to name")
	}
}

func TestGhostHintMenuOffersTheKeysAndBindsOne(t *testing.T) {
	h := ghostHintWorld(t)
	session := &h.state.sessions[0]
	canvas, badge, ok := paintGhostGrid(t, h.state, session)
	if !ok {
		t.Fatal("the badge was not painted")
	}
	x := badge.Bounds.X + badge.Bounds.Width/2
	y := badge.Bounds.Y + badge.Bounds.Height/2
	down := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: ghostHintID(session),
		X: x, Y: y, Viewport: h.state.viewport,
	}
	up := down
	up.Kind = gfx.PointerUp
	if result := h.state.Pointer(down); !result.Handled {
		t.Fatalf("pressing the badge was not handled: %+v", result)
	}
	if result := h.state.Pointer(up); !result.Handled || !h.state.ghostMenu.open {
		t.Fatalf("releasing the badge did not open the menu: %+v", result)
	}
	canvas.Reset()
	control := terminalTestHit(h.state, session)
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, control, true)
	h.state.paintGrid(canvas, session, h.state.layout())
	h.state.paintGhostMenu(canvas, h.state.windowRect())
	current := h.state.shortcuts.chords.Chord(shortcutAcceptSuggestion)
	right, ok := hitByID(canvas, hitGhostMenuRight)
	if !ok || !right.Selected || current != (keymap.Chord{Key: gfx.KeyRight}) {
		t.Fatalf("right row = %+v, %v; current = %v", right, ok, current)
	}
	if _, ok := hitByID(canvas, hitGhostMenuTab); !ok {
		t.Fatal("the menu does not offer Tab")
	}
	if _, ok := hitByID(canvas, hitGhostMenuCustom); !ok {
		t.Fatal("the menu does not offer a custom key")
	}
	if _, ok := hitByID(canvas, hitGhostMenuTurnOff); !ok {
		t.Fatal("the menu does not offer to turn the hint off")
	}
	h.state.Click(hitGhostMenuTab)
	if h.state.ghostMenu.open {
		t.Fatal("the menu stayed open after a choice")
	}
	if got := h.state.shortcuts.chords.Chord(shortcutAcceptSuggestion); got != (keymap.Chord{Key: gfx.KeyTab}) {
		t.Fatalf("binding = %v, want Tab", got)
	}
}

func TestGhostHintMenuTurnsTheHintOffButKeepsTheKey(t *testing.T) {
	h := ghostHintWorld(t)
	session := &h.state.sessions[0]
	if _, _, ok := paintGhostGrid(t, h.state, session); !ok {
		t.Fatal("the badge was not painted")
	}
	h.state.openGhostMenu(session)
	h.state.Click(hitGhostMenuTurnOff)
	if !h.state.history.acceptHintOff {
		t.Fatal("the menu did not turn the hint off")
	}
	if h.state.ghostMenu.open {
		t.Fatal("the menu stayed open after turning the hint off")
	}
	if _, _, ok := paintGhostGrid(t, h.state, session); ok {
		t.Fatal("the badge is still painted after it was turned off")
	}
	if !h.state.snapshot().CommandAcceptHintOff {
		t.Fatal("turning the hint off was not saved")
	}
	// The key still accepts the suggestion; only the badge is gone.
	before := len(h.shell.input)
	h.key(gfx.KeyEvent{Key: gfx.KeyRight})
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("the shell got %q, want the suggestion", got)
	}
}

func TestGhostHintMenuCustomOpensTheShortcutDialog(t *testing.T) {
	h := ghostHintWorld(t)
	session := &h.state.sessions[0]
	if _, _, ok := paintGhostGrid(t, h.state, session); !ok {
		t.Fatal("the badge was not painted")
	}
	h.state.openGhostMenu(session)
	h.state.Click(hitGhostMenuCustom)
	if h.state.modal != modalShortcutCapture || h.state.shortcutCapture != shortcutAcceptSuggestion {
		t.Fatalf("modal = %v, capture = %v", h.state.modal, h.state.shortcutCapture)
	}
}

func TestGhostHintMenuClosesOnEscapeAndOutsidePress(t *testing.T) {
	h := ghostHintWorld(t)
	session := &h.state.sessions[0]
	if _, _, ok := paintGhostGrid(t, h.state, session); !ok {
		t.Fatal("the badge was not painted")
	}
	h.state.openGhostMenu(session)
	h.state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if h.state.ghostMenu.open {
		t.Fatal("Escape left the menu open")
	}
	h.state.openGhostMenu(session)
	h.state.Click(hitGhostMenuDismiss)
	if h.state.ghostMenu.open {
		t.Fatal("a press outside left the menu open")
	}
}

func TestAcceptSuggestionBindingDrivesTheGhostKey(t *testing.T) {
	h := ghostWorld(t, "git checkout main")
	h.prompt("$ ")
	h.type_("git ch")
	before := len(h.shell.input)
	h.key(gfx.KeyEvent{Key: gfx.KeyRight})
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("the default key typed %q, want the suggestion", got)
	}

	// Another key takes over, and the arrow is the shell's again.
	h = ghostWorld(t, "git checkout main")
	h.prompt("$ ")
	if _, bound := h.state.shortcuts.bind(shortcutAcceptSuggestion, keymap.Chord{Key: gfx.KeyTab}); !bound {
		t.Fatal("Tab could not be bound")
	}
	h.type_("git ch")
	before = len(h.shell.input)
	h.key(gfx.KeyEvent{Key: gfx.KeyTab})
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("Tab typed %q, want the suggestion", got)
	}

	h = ghostWorld(t, "git checkout main")
	h.prompt("$ ")
	h.state.shortcuts.bind(shortcutAcceptSuggestion, keymap.Chord{Key: gfx.KeyTab})
	h.type_("git ch")
	before = len(h.shell.input)
	h.key(gfx.KeyEvent{Key: gfx.KeyRight})
	if got := string(h.shell.input[before:]); got != "\x1b[C" {
		t.Fatalf("the shell got %q, want the arrow key it now owns", got)
	}
}

func TestUnbindingTheAcceptKeyLeavesTheArrowToTheShell(t *testing.T) {
	h := ghostWorld(t, "kubectl get pods")
	h.prompt("$ ")
	h.state.shortcuts.unbind(shortcutAcceptSuggestion)
	h.type_("kubectl ")
	before := len(h.shell.input)
	h.key(gfx.KeyEvent{Key: gfx.KeyRight})
	if got := string(h.shell.input[before:]); got != "\x1b[C" {
		t.Fatalf("the shell got %q, want the arrow key", got)
	}

	// Word acceptance is its own fixed key, whatever the binding is.
	h = ghostWorld(t, "kubectl get pods")
	h.prompt("$ ")
	h.state.shortcuts.unbind(shortcutAcceptSuggestion)
	h.type_("kubectl ")
	before = len(h.shell.input)
	h.key(gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt})
	if got := string(h.shell.input[before:]); got != "get" {
		t.Fatalf("the shell got %q, want one word", got)
	}
}

func TestAcceptSuggestionKeyIgnoresReleases(t *testing.T) {
	h := ghostWorld(t, "git checkout main")
	h.prompt("$ ")
	h.type_("git ch")
	before := len(h.shell.input)
	h.key(gfx.KeyEvent{Key: gfx.KeyRight, Release: true})
	if got := string(h.shell.input[before:]); got != "" {
		t.Fatalf("a release typed %q", got)
	}
	if _, ok := h.state.commandGhost(&h.state.sessions[0]); !ok {
		t.Fatal("a release took the suggestion")
	}
}

func TestAcceptSuggestionCaptureRejectsPlainCharacters(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openShortcutCapture(shortcutAcceptSuggestion)
	state.Key(gfx.KeyEvent{Text: "j", Key: gfx.KeyUnknown})
	if state.shortcutError != state.messages.ShortcutsAcceptInvalid {
		t.Fatalf("error = %q", state.shortcutError)
	}
	if state.modal != modalShortcutCapture {
		t.Fatal("the dialog closed on a rejected key")
	}
	if chord := state.shortcuts.chords.Chord(shortcutAcceptSuggestion); chord != (keymap.Chord{Key: gfx.KeyRight}) {
		t.Fatalf("the rejected key changed the binding to %v", chord)
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyTab})
	if state.modal != modalNone || state.shortcuts.chords.Chord(shortcutAcceptSuggestion) != (keymap.Chord{Key: gfx.KeyTab}) {
		t.Fatalf("modal = %v, binding = %v", state.modal, state.shortcuts.chords.Chord(shortcutAcceptSuggestion))
	}
}

func TestAcceptSuggestionBindingPersists(t *testing.T) {
	restored := restoreShortcuts([]savedShortcut{{Action: "accept_suggestion", Key: "tab"}})
	if chord := restored.chords.Chord(shortcutAcceptSuggestion); chord != (keymap.Chord{Key: gfx.KeyTab}) {
		t.Fatalf("restored binding = %v, want Tab", chord)
	}
}

func TestAcceptSuggestionSettingsRowAndHintToggle(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.shortcuts.actionFor(keymap.Chord{Key: gfx.KeyRight}) != shortcutAcceptSuggestion {
		t.Fatal("the right arrow is not the accept-suggestion binding")
	}
	state.Click(hitSettingsCompletionHint)
	if !state.history.acceptHintOff {
		t.Fatal("the hint toggle did not turn the badge off")
	}
	state.Click(hitSettingsCompletionHint)
	if state.history.acceptHintOff {
		t.Fatal("the hint toggle did not turn the badge back on")
	}
	var saved bool
	for _, entry := range state.snapshot().Shortcuts {
		if entry.Action != "accept_suggestion" {
			continue
		}
		saved = true
		if chord, ok := keymap.Parse(gfx.Modifiers(entry.Modifiers), entry.Key); !ok || chord != (keymap.Chord{Key: gfx.KeyRight}) {
			t.Fatalf("the accept key saves as %v, %v", chord, ok)
		}
	}
	if !saved {
		t.Fatal("the accept-suggestion binding is missing from the saved table")
	}
}

func TestHeadlessGhostHintBadgeOpensItsMenuAndBindsATab(t *testing.T) {
	h := ghostHintWorld(t)
	var invalidator app.Invalidator
	h.state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Invalidator: &invalidator, OnWake: h.state.Wake,
		Paint:     func(canvas *gfx.Canvas, size gfx.Size) { h.state.Paint(canvas, size) },
		TextInput: h.state.TerminalInput,
		OnKey:     h.state.Key, OnPointer: h.state.Pointer, OnResize: h.state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	// Focus the terminal; the input-click setting would turn the press into
	// cursor keys, which is neither what this test is about nor harmless here.
	h.state.terminalInputClickOff = true
	if err := view.Click(400, 300); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}

	badge := h.state.messages.Completion.AcceptHintBadge
	deadline := time.Now().Add(5 * time.Second)
	var pill gfx.Hit
	for {
		if hit, ok := view.Hit(badge); ok {
			pill = hit
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no reachable badge named %q was painted", badge)
		}
		if err := view.Wait(20 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.Click(pill.Bounds.X+pill.Bounds.Width/2, pill.Bounds.Y+pill.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !h.state.ghostMenu.open {
		t.Fatal("clicking the badge did not open its menu")
	}
	if err := view.Wait(20 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	tab, ok := view.Hit("Tab")
	if !ok {
		t.Fatal("the menu does not show the Tab row")
	}
	if err := view.Click(tab.Bounds.X+tab.Bounds.Width/2, tab.Bounds.Y+tab.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if h.state.ghostMenu.open {
		t.Fatal("the menu stayed open after the click")
	}
	if got := h.state.shortcuts.chords.Chord(shortcutAcceptSuggestion); got != (keymap.Chord{Key: gfx.KeyTab}) {
		t.Fatalf("binding = %v, want Tab", got)
	}
	// Closing the menu gives the terminal (and its suggestion) back.
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, ok := view.Hit(badge); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the terminal did not get its suggestion back after the menu closed")
		}
		invalidator.Invalidate()
		if err := view.Wait(20 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
}
