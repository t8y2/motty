package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
)

func TestCommandPaletteIdentitiesAreItsOwn(t *testing.T) {
	// The palette's controls and its entries are names in a place of its own, so
	// nothing else answers to them, and an entry the palette cannot list has none.
	for _, id := range []gfx.ID{hitCommandPalette, hitCommandPaletteDismiss, hitCommandPaletteClose, hitCommandPaletteSearch, hitCommandPaletteItems.At(0), hitCommandPaletteItems.At(commandPaletteVisibleRows - 1)} {
		if !tree.Within(id.Scope(), commandPaletteHits) {
			t.Fatalf("identity %s is outside the palette's place", tree.Text(id))
		}
	}
}

func TestCommandPaletteFiltersAndExecutesCommands(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitCommandPalette)
	if state.modal != modalCommandPalette || state.EditorFor(hitCommandPaletteSearch) == nil {
		t.Fatal("command palette did not open with a native text editor")
	}
	app.TextEvent{Kind: app.TextInsert, Text: "settings"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if len(state.commandPalette.visible) != 1 {
		t.Fatalf("visible commands = %d", len(state.commandPalette.visible))
	}
	if hit, ok := hitByID(&canvas, hitCommandPaletteItems.At(0)); !ok || hit.Label != state.messages.NavSettings {
		t.Fatalf("settings command hit = %+v, ok=%v", hit, ok)
	}
	result := state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	if !result.Handled || state.modal != modalNone || state.activeSection != sectionSettings {
		t.Fatalf("execute result=%+v modal=%d section=%d", result, state.modal, state.activeSection)
	}
}

func TestCommandPaletteKeyboardNavigationAndSessionSwitch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions,
		Session{ID: 1, Title: "Alpha", restored: true},
		Session{ID: 2, Title: "Beta", restored: true},
	)
	state.activeSessionID = 1
	state.nextSessionID = 3
	state.Click(hitCommandPalette)
	app.TextEvent{Kind: app.TextInsert, Text: "Beta"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	state.refreshCommandPalette()
	if len(state.commandPalette.visible) != 1 {
		t.Fatalf("visible session commands = %d", len(state.commandPalette.visible))
	}
	result := state.Key(gfx.KeyEvent{Target: hitCommandPaletteItems.At(0), Key: gfx.KeyEnter})
	if !result.Handled || state.activeSessionID != 2 || state.activeSection != sectionTerminal || state.modal != modalNone {
		t.Fatalf("session switch result=%+v active=%d section=%d modal=%d", result, state.activeSessionID, state.activeSection, state.modal)
	}
}

func TestCommandPaletteShortcutOpensFromTerminal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	result := state.Shortcut(gfx.KeyEvent{Text: "p", Modifiers: keymap.Primary | gfx.ModifierShift})
	if !result.Handled || !result.Repaint || result.Focus != hitCommandPaletteSearch || state.modal != modalCommandPalette {
		t.Fatalf("shortcut result=%+v modal=%d", result, state.modal)
	}
}

func TestNewWindowCommandAndShortcutUseTheHostLifecycle(t *testing.T) {
	opened := 0
	options := testOptions(t)
	options.NewWindow = func() error { opened++; return nil }
	state := newTestState(t, options)
	state.Click(hitCommandPalette)
	app.TextEvent{Kind: app.TextInsert, Text: "new window"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	state.refreshCommandPalette()
	if len(state.commandPalette.visible) != 1 {
		t.Fatalf("visible commands = %d", len(state.commandPalette.visible))
	}
	state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	if opened != 1 {
		t.Fatalf("palette opened %d windows", opened)
	}
	result := state.Shortcut(gfx.KeyEvent{Text: "n", Modifiers: keymap.Primary | gfx.ModifierShift})
	if !result.Handled || opened != 2 {
		t.Fatalf("shortcut result=%+v opened=%d", result, opened)
	}
}

func TestCommandPalettePaintDoesNotAllocateOnceWarm(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitCommandPalette)
	app.TextEvent{Kind: app.TextInsert, Text: "SET"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("command palette paint allocations = %v", allocations)
	}
	if _, scheduled := canvas.NextFrame(); scheduled {
		t.Fatal("idle command palette scheduled another frame")
	}
}

func BenchmarkPaintCommandPalette(b *testing.B) {
	state := newTestState(b, testOptions(b))
	state.Click(hitCommandPalette)
	app.TextEvent{Kind: app.TextInsert, Text: "set"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func TestCommandPaletteReachesEntriesBelowTheTenRowsItShows(t *testing.T) {
	state := newTestState(t, testOptions(t))
	// With a session open the palette lists its commands too: more than ten.
	for id := uint32(1); id <= 3; id++ {
		state.sessions = append(state.sessions, Session{ID: id, Title: "Box", restored: true})
	}
	state.activeSessionID, state.nextSessionID = 1, 4
	state.Click(hitCommandPalette)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	total := len(state.commandPalette.visible)
	if total <= commandPaletteVisibleRows {
		t.Fatalf("only %d entries; the test needs more than the %d rows shown", total, commandPaletteVisibleRows)
	}
	last := hitCommandPaletteItems.At(total - 1)
	if _, ok := hitByID(&canvas, hitCommandPaletteItems.At(commandPaletteVisibleRows)); ok {
		t.Fatal("an eleventh row is painted before anything scrolled")
	}
	result := state.Key(gfx.KeyEvent{Target: hitCommandPaletteItems.At(0), Key: gfx.KeyEnd})
	if !result.Handled || result.Focus != last {
		t.Fatalf("End: %+v, want focus on the last visible row %v", result, last)
	}
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, last)
	if !ok || state.commandPalette.selected != total-1 {
		t.Fatalf("after End: last row painted %v, selected %d of %d", ok, state.commandPalette.selected, total)
	}
	if want := state.commandPalette.entries[state.commandPalette.visible[total-1]].label; hit.Label != want {
		t.Fatalf("the last row is %q, want the last entry %q", hit.Label, want)
	}
	// The wheel moves the list back, and a click on a row runs the entry that row shows.
	if r := state.Scroll(gfx.ScrollEvent{DeltaY: -1000, Target: hitCommandPaletteItems.At(0)}); !r.Repaint || state.commandPalette.scroll != 0 {
		t.Fatalf("wheel up: %+v scroll=%g", r, state.commandPalette.scroll)
	}
	state.Scroll(gfx.ScrollEvent{DeltaY: 1000})
	state.Click(last)
	if state.modal != modalNone || state.activeSessionID != 3 {
		t.Fatalf("clicking the last row: modal=%d active session %d, want the last session", state.modal, state.activeSessionID)
	}
}

func TestCommandPalettePointerHoverAndLeaveRetainsSelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitCommandPalette)
	if state.commandPalette.selected != 0 {
		t.Fatalf("initial selected = %d, want 0", state.commandPalette.selected)
	}
	settingsRow := commandPaletteRow(t, state, hitSettings)

	// Moving pointer over Settings.
	settingsHit := hitCommandPaletteItems.At(settingsRow)
	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: settingsHit})
	if !result.Handled || !result.Repaint || state.commandPalette.selected != settingsRow {
		t.Fatalf("hover over settings: result=%+v selected=%d row=%d", result, state.commandPalette.selected, settingsRow)
	}

	// Moving pointer away (e.g. back to search field or outside the list).
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hitCommandPaletteSearch})
	if state.commandPalette.selected != settingsRow {
		t.Fatalf("after pointer leaves, selected = %d, want it to stay on %d", state.commandPalette.selected, settingsRow)
	}

	// Pressing Enter from the search input should execute the selected item (Settings), not item 0 (Hosts).
	res := state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	if !res.Handled || state.modal != modalNone || state.activeSection != sectionSettings {
		t.Fatalf("enter after hover: res=%+v modal=%d section=%d (want settings)", res, state.modal, state.activeSection)
	}
}

func TestCommandPaletteArrowKeysFromSearchInput(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitCommandPalette)
	settingsRow := commandPaletteRow(t, state, hitSettings)

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	searchHit, ok := hitByID(&canvas, hitCommandPaletteSearch)
	if !ok || !searchHit.NavigationKeys || searchHit.Placement != nil {
		t.Fatalf("search field hit = %+v, want NavigationKeys=true and Placement=nil", searchHit)
	}

	// Pressing Down from the search box navigates to the next item while keeping focus on the search field.
	result := state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyDown})
	if !result.Handled || !result.Repaint || result.Focus != hitCommandPaletteSearch || state.commandPalette.selected != 1 {
		t.Fatalf("down from search: result=%+v selected=%d", result, state.commandPalette.selected)
	}

	// Pressing Up navigates back to item 0.
	result = state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyUp})
	if !result.Handled || !result.Repaint || result.Focus != hitCommandPaletteSearch || state.commandPalette.selected != 0 {
		t.Fatalf("up from search: result=%+v selected=%d", result, state.commandPalette.selected)
	}

	// Navigate to Settings using down arrows.
	for i := 0; i < settingsRow; i++ {
		state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyDown})
	}
	if state.commandPalette.selected != settingsRow {
		t.Fatalf("after %d down arrows: selected=%d, want %d", settingsRow, state.commandPalette.selected, settingsRow)
	}

	// Press Enter to execute Settings.
	res := state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	if !res.Handled || state.modal != modalNone || state.activeSection != sectionSettings {
		t.Fatalf("enter after arrow navigation: res=%+v modal=%d section=%d (want settings)", res, state.modal, state.activeSection)
	}
}

func TestCommandPalettePointerDownDoesNotConsumePress(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitCommandPalette)
	settingsRow := commandPaletteRow(t, state, hitSettings)

	// Pointer down on Settings must not be consumed as handled, so click can fire.
	down := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitCommandPaletteItems.At(settingsRow)})
	if down.Handled {
		t.Fatalf("pointer down handled=%v, want false so click can be delivered", down.Handled)
	}

	// Click on Settings executes it.
	state.Click(hitCommandPaletteItems.At(settingsRow))
	if state.modal != modalNone || state.activeSection != sectionSettings {
		t.Fatalf("click modal=%d section=%d, want sectionSettings", state.modal, state.activeSection)
	}
}

func TestCommandPaletteDriverRetainsNativeTextEditing(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitCommandPalette)

	var nativeInput app.TextInput
	driver, err := app.NewDriver(app.Config{
		Title: "cmd palette driver", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(context.ID); editor != nil {
				nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
				return &nativeInput
			}
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	// Type abcdef into the search field.
	if err := driver.Type("abcdef"); err != nil {
		t.Fatal(err)
	}
	if got := state.EditorFor(hitCommandPaletteSearch).String(); got != "abcdef" {
		t.Fatalf("search value = %q, want abcdef", got)
	}

	// Press Home: native editor moves caret to 0.
	if err := driver.Key(gfx.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	if caret := state.EditorFor(hitCommandPaletteSearch).Selection(); caret != (text.Range{Start: 0, End: 0}) {
		t.Fatalf("after Home: caret = %+v, want 0", caret)
	}

	// Select all and delete to restore full list.
	if _, err := driver.Shortcut('a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.EditorFor(hitCommandPaletteSearch).String(); got != "" {
		t.Fatalf("search value after delete = %q, want empty", got)
	}

	// Press Down: reaches the application to navigate command palette items.
	if err := driver.Key(gfx.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if state.commandPalette.selected != 1 {
		t.Fatalf("after Down: selected = %d, want 1", state.commandPalette.selected)
	}
}

func TestCommandPaletteTabFocusAndHoverEnterExecutesHoveredChoice(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitCommandPalette)
	if state.commandPalette.selected != 0 {
		t.Fatalf("initial selected = %d, want 0", state.commandPalette.selected)
	}

	firstItem := hitCommandPaletteItems.At(0)
	settingsRow := commandPaletteRow(t, state, hitSettings)
	settingsHit := hitCommandPaletteItems.At(settingsRow)

	// Hover pointer over Settings.
	hoverRes := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: settingsHit})
	if !hoverRes.Handled || state.commandPalette.selected != settingsRow {
		t.Fatalf("hover over settings: result=%+v selected=%d row=%d", hoverRes, state.commandPalette.selected, settingsRow)
	}

	// Paint canvas with focus still on item 0 (as when Tab focused it).
	var canvas gfx.Canvas
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, firstItem, true)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	// Enter with target=firstItem must execute the hovered item (Settings, 5), not item 0.
	enterRes := state.Key(gfx.KeyEvent{Target: firstItem, Key: gfx.KeyEnter})
	if !enterRes.Handled || state.modal != modalNone || state.activeSection != sectionSettings {
		t.Fatalf("enter after hover: enterRes=%+v modal=%d section=%d (want settings)", enterRes, state.modal, state.activeSection)
	}
}

func TestCommandPaletteMoveWheelRepaintEnterOnLongList(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for id := uint32(1); id <= 5; id++ {
		state.sessions = append(state.sessions, Session{ID: id, Title: fmt.Sprintf("Server %d", id), restored: true})
	}
	state.activeSessionID, state.nextSessionID = 1, 6
	state.Click(hitCommandPalette)
	settingsRow := commandPaletteRow(t, state, hitSettings)
	scrollRows := settingsRow - 2

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if len(state.commandPalette.visible) <= commandPaletteVisibleRows {
		t.Fatalf("entries = %d, want > %d", len(state.commandPalette.visible), commandPaletteVisibleRows)
	}

	// 1. Move: pointer hovers over slot 2
	slot2 := hitCommandPaletteItems.At(2)
	moveRes := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: slot2})
	if !moveRes.Handled || state.commandPalette.selected != 2 {
		t.Fatalf("move: result=%+v selected=%d, want 2", moveRes, state.commandPalette.selected)
	}

	// 2. Scroll Settings under the pointer, which stays over slot 2.
	scrollRes := state.Scroll(gfx.ScrollEvent{DeltaY: float32(34 * scrollRows), Target: slot2, X: state.commandPalette.listRect.X + 10, Y: state.commandPalette.listRect.Y + 2*34 + 15})
	if !scrollRes.Handled || !scrollRes.Repaint {
		t.Fatalf("scroll: result=%+v", scrollRes)
	}
	if state.commandPalette.scroll != float32(34*scrollRows) {
		t.Fatalf("after scroll: scroll=%g, want %d", state.commandPalette.scroll, 34*scrollRows)
	}
	wantSelected := settingsRow
	if state.commandPalette.selected != wantSelected {
		t.Fatalf("after wheel: selected=%d, want %d", state.commandPalette.selected, wantSelected)
	}

	// 3. Repaint
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	// 4. Enter executes Settings, the row under the pointer.
	keyRes := state.Key(gfx.KeyEvent{Target: slot2, Key: gfx.KeyEnter})
	if !keyRes.Handled || state.modal != modalNone || state.activeSection != sectionSettings {
		t.Fatalf("enter: res=%+v modal=%d section=%d", keyRes, state.modal, state.activeSection)
	}
}

func commandPaletteRow(t testing.TB, state *State, target gfx.ID) int {
	t.Helper()
	for row, entry := range state.commandPalette.entries {
		if entry.target == target {
			return row
		}
	}
	t.Fatalf("command palette target %d is not listed", target)
	return -1
}
