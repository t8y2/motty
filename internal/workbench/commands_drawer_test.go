package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// The drawer only routes on the terminal page, like the snippet picker it wraps.
func openCommandsDrawerTest(t *testing.T) *State {
	state := newTestState(t, freshOptions(t))
	// A real terminal page needs an active workspace deck, not just the section flag.
	state.deck = layoutcore.Deck{}
	if _, _, err := state.deck.Create("test"); err != nil {
		t.Fatal(err)
	}
	state.activeSection = sectionTerminal
	state.modal = modalNone
	state.snippets.list = []snippet{
		{Name: "ls", Command: "ls", Run: true},
		{Name: "pwd", Command: "pwd", Group: "shell"},
	}
	// The bar hides until the user opts in; these tests exercise the open bar.
	state.commands.shown = true
	return state
}

// Clicking a command tag routes through the central handler to sendSnippet, so a
// tag is a repeatable action and not a one-shot popup.
func TestCommandTagClickRoutes(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.Click(hitCommandsTag.At(0)) // a user click goes through the public Click
	if !state.clickCommandsDrawer(hitCommandsTag.At(1)) {
		t.Fatal("command tag scope was not recognized by the drawer")
	}
	if state.clickCommandsDrawer(hitSnippetAdd) {
		t.Fatal("an unrelated hit was claimed by the drawer")
	}
}

// Sending a quick command from the bar must hand the keyboard back to the
// terminal, or the next Enter types wherever the pointer happens to be.
func TestClickingACommandChipReturnsTheKeyboardToTheTerminal(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionTerminal
	state.snippets.list = []snippet{{Name: "ls", Command: "ls", Run: true}}
	state.commands.shown = true
	state.Click(hitCommandsTag.At(0))
	want := state.activeTerminalFocusHit()
	if want.IsZero() {
		t.Fatal("no terminal focus target")
	}
	if state.pendingFocus != want {
		t.Fatalf("pending focus = %v, want the terminal %v", state.pendingFocus, want)
	}
	canvas, _ := paintAt(state, gfx.Size{Width: 1200, Height: 800})
	if got := canvas.FocusRequest(); got != want {
		t.Fatalf("paint asked for focus %v, want the terminal %v", got, want)
	}
	if !state.pendingFocus.IsZero() {
		t.Fatal("the focus request was not consumed")
	}
}

// The focus a chip asked for must not be taken while a dialog is up (the
// dialog keeps its own first field), and must still be cleared so it cannot
// steal the keyboard later.
func TestAQuickCommandKeepsADialogsKeyboardAndDropsItsFocus(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionTerminal
	state.modal = modalSnippetVariables
	state.pendingFocus = state.activeTerminalFocusHit()
	canvas, _ := paintAt(state, gfx.Size{Width: 1200, Height: 800})
	if got := canvas.FocusRequest(); !got.IsZero() {
		t.Fatalf("paint asked for %v while a dialog was up", got)
	}
	if !state.pendingFocus.IsZero() {
		t.Fatal("the focus request was not dropped with the dialog up")
	}
}

// A page that cannot take the keyboard (the snippets page) must drop the
// request too, not hold it for a later paint to grab the keyboard.
func TestAQuickCommandDropsFocusOnANonTerminalPage(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionSnippets
	state.pendingFocus = state.activeTerminalFocusHit()
	canvas, _ := paintAt(state, gfx.Size{Width: 1200, Height: 800})
	if got := canvas.FocusRequest(); !got.IsZero() {
		t.Fatalf("paint asked for %v on the snippets page", got)
	}
	if !state.pendingFocus.IsZero() {
		t.Fatal("the focus request was not dropped on a non-terminal page")
	}
}

// Cancelling or closing the variables dialog opened from the command bar must
// also bring the keyboard back to the terminal.
func TestCancellingTheVariablesDialogReturnsTheKeyboardToTheTerminal(t *testing.T) {
	for _, id := range []gfx.ID{hitSnippetVarCancel, hitSnippetVarClose} {
		world, options := newSharingWorld(t)
		state := newTestState(t, options)
		world.connectFirst(t, state)
		state.activeSection = sectionTerminal
		state.modal = modalSnippetVariables
		state.snippetPrompt.fromPage = false
		state.pendingFocus = gfx.ID{}
		state.clickSnippetVariablesDialog(id)
		want := state.activeTerminalFocusHit()
		if want.IsZero() {
			t.Fatal("no terminal focus target")
		}
		if state.pendingFocus != want {
			t.Fatalf("%v: pending focus = %v, want the terminal %v", id, state.pendingFocus, want)
		}
	}
}

// Cancelling a dialog opened from the snippets page stays on that page: it
// must not queue a terminal focus request.
func TestCancellingTheVariablesDialogFromThePageKeepsThePage(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionSnippets
	state.modal = modalSnippetVariables
	state.snippetPrompt.fromPage = true
	state.pendingFocus = gfx.ID{}
	state.clickSnippetVariablesDialog(hitSnippetVarCancel)
	if !state.pendingFocus.IsZero() {
		t.Fatal("cancelling from the snippets page asked for the terminal")
	}
}

// A quick command with variables opens a dialog instead of sending; sending it
// must bring the keyboard back to the terminal the command went to.
func TestSubmittingTheSnippetVariablesDialogReturnsTheKeyboardToTheTerminal(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionTerminal
	state.modal = modalSnippetVariables
	state.snippets.list = []snippet{{Name: "ls", Command: "ls", Run: true}}
	state.snippetPrompt.snippetIndex = 0
	state.snippetPrompt.targetSessionID = state.sessions[0].ID
	state.submitSnippetVariables()
	want := state.activeTerminalFocusHit()
	if want.IsZero() {
		t.Fatal("no terminal focus target")
	}
	if state.pendingFocus != want {
		t.Fatalf("pending focus = %v, want the terminal %v", state.pendingFocus, want)
	}
}

// A right click on a chip opens that snippet's own menu, so a command added to the
// bar can still be edited or removed there (#1005).
func TestRightClickingACommandChipOpensItsMenu(t *testing.T) {
	state := openCommandsDrawerTest(t)
	event := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitCommandsTag.At(1)}
	if got := pointer(state, event); !got.Handled || !state.snippetMenu.open || !state.snippetMenu.fromBar || state.snippetMenu.index != 1 {
		t.Fatalf("secondary: handled=%v menu=%+v", got.Handled, state.snippetMenu)
	}
	if menuHasRow(state, state.messages.Tools.SnippetMoveUp) {
		t.Fatal("a command-bar menu offers Move up, which reorders by the Snippets page")
	}
	chooseMenu(t, state, hitSnippetMenuEdit)
	if state.modal != modalSnippet {
		t.Fatalf("edit from the bar opened modal %d, want the snippet form", state.modal)
	}
	state.modal = modalNone
	state.snippetMenu = snippetMenu{}

	pointer(state, event)
	chooseMenu(t, state, hitSnippetMenuDelete)
	if state.modal != modalSnippetDelete || len(state.snippets.list) != 2 {
		t.Fatalf("delete from the bar opened modal %d with %d snippets", state.modal, len(state.snippets.list))
	}
}

// A rebuilt snippet list no longer holds the snippet the open menu was asked for,
// so the menu closes instead of editing whatever now sits at its index.
func TestACommandBarMenuFollowsARebuiltList(t *testing.T) {
	state := openCommandsDrawerTest(t)
	event := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitCommandsTag.At(1)}
	if got := pointer(state, event); !got.Handled || !state.snippetMenu.open {
		t.Fatalf("the menu did not open: handled=%v menu=%+v", got.Handled, state.snippetMenu)
	}
	state.snippets.list = []snippet{{Name: "other", Command: "echo other"}}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.snippetMenu.open {
		t.Fatal("the menu stayed open over a rebuilt list")
	}
}

// The drawer reserves no space off the terminal page, a fixed single-bar height on
// the terminal page, and nothing once closed.
func TestCommandDrawerHeight(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.activeSection = sectionHosts // not the terminal page
	if h := state.commandsDrawerHeight(); h != 0 {
		t.Fatalf("drawer reserved space off the terminal page: %v", h)
	}
	state.activeSection = sectionTerminal
	// Opting in is the only way the strip appears: a fresh state reserves nothing.
	state.commands.shown = false
	if h := state.commandsDrawerHeight(); h != 0 {
		t.Fatalf("a fresh state reserved bar space: %v", h)
	}
	state.commands.shown = true
	if h := state.commandsDrawerHeight(); h != commandsBarHeight {
		t.Fatalf("open drawer height = %v, want %v", h, commandsBarHeight)
	}
}

// The close control hides the whole bar, freeing its reserved strip. The
// snippets button keeps its one meaning (open the picker) while the bar is
// hidden; the picker's own checkbox switches it back without closing the
// dialog.
func TestCommandDrawerCloseAndReopen(t *testing.T) {
	state := openCommandsDrawerTest(t)
	if h := state.commandsDrawerHeight(); h != commandsBarHeight {
		t.Fatal("drawer was not open before closing")
	}
	state.Click(hitCommandsClose)
	if state.commands.shown {
		t.Fatal("close did not hide the drawer")
	}
	if h := state.commandsDrawerHeight(); h != 0 {
		t.Fatalf("closed drawer still reserved space: %v", h)
	}
	state.Click(hitTerminalSnippets)
	if state.modal != modalCommandPalette {
		t.Fatalf("the snippet button opened %v, want the picker", state.modal)
	}
	if state.commands.shown {
		t.Fatal("the snippet button reopened the bar instead of opening the picker")
	}
	if !state.commandBarSwitchShown() {
		t.Fatal("the picker did not offer the bar's checkbox")
	}
	state.Click(hitCommandsBarToggle)
	if state.modal != modalCommandPalette {
		t.Fatalf("ticking the checkbox closed the picker: modal = %v", state.modal)
	}
	if !state.commands.shown {
		t.Fatal("ticking the checkbox did not show the bar")
	}
	if h := state.commandsDrawerHeight(); h != commandsBarHeight {
		t.Fatalf("reopened bar height = %v, want %v", h, commandsBarHeight)
	}
	// Unticking hides it again, with the picker still open.
	state.Click(hitCommandsBarToggle)
	if state.commands.shown || state.modal != modalCommandPalette {
		t.Fatalf("unticking left shown=%v modal=%v", state.commands.shown, state.modal)
	}
}

// The bar's checkbox is chrome of the picker, never a row of its searchable
// list: absent from entries, unaffected by a query, and switching it leaves
// the search text and the dialog's layout alone.
func TestCommandBarSwitchIsOutsideTheSearchList(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.Click(hitCommandsClose)
	state.Click(hitTerminalSnippets)
	palette := &state.commandPalette
	if !state.commandBarSwitchShown() || hasTarget(state, hitCommandsBarToggle) {
		t.Fatal("the bar switch leaked into the searchable list")
	}
	shownHeight := state.commandPaletteHeight()
	palette.search.Insert("no-such-command")
	state.refreshCommandPalette()
	if len(palette.visible) != 0 || hasTarget(state, hitCommandsBarToggle) {
		t.Fatal("a search turned the bar switch into a result")
	}
	if state.commandPaletteHeight() != shownHeight {
		t.Fatal("the search changed the dialog height reserved for the switch")
	}
	state.Click(hitCommandsBarToggle)
	if state.modal != modalCommandPalette || !state.commands.shown {
		t.Fatalf("ticking left modal=%v shown=%v", state.modal, state.commands.shown)
	}
	if palette.search.String() != "no-such-command" {
		t.Fatalf("ticking the checkbox reset the query to %q", palette.search.String())
	}
}

// The checkbox stays put while the bar is shown: that is what makes it a
// checkbox the user can untick. The command list is the same length with or
// without it.
func TestCommandBarCheckboxStaysInPlace(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.Click(hitTerminalSnippets)
	if !state.commandBarSwitchShown() {
		t.Fatal("the picker lost the bar checkbox while the bar was shown")
	}
	palette := &state.commandPalette
	if len(palette.visible) != len(state.snippets.list) {
		t.Fatalf("the list holds %d rows, want %d snippets", len(palette.visible), len(state.snippets.list))
	}
	palette.search.Insert("pwd")
	state.refreshCommandPalette()
	if len(palette.visible) != 1 {
		t.Fatalf("searching matched %d rows, want 1", len(palette.visible))
	}
	state.Click(hitCommandsBarToggle)
	if state.commands.shown || state.modal != modalCommandPalette ||
		!state.commandBarSwitchShown() || len(palette.visible) != 1 {
		t.Fatalf("unticking left shown=%v modal=%v rows=%d", state.commands.shown, state.modal, len(palette.visible))
	}
	state.Click(hitCommandsBarToggle)
	if !state.commands.shown {
		t.Fatal("ticking the checkbox again did not bring the bar back")
	}
	if palette.search.String() != "pwd" {
		t.Fatal("ticking the checkbox cleared the search")
	}
}

func hasTarget(state *State, target gfx.ID) bool {
	palette := &state.commandPalette
	for _, index := range palette.visible {
		if palette.entries[index].target == target {
			return true
		}
	}
	return false
}

// The group filter hides a snippet whose group is unchecked, but keeps ungrouped
// commands; ungrouped is its own checkbox and hides only those commands.
func TestCommandGroupFilter(t *testing.T) {
	state := openCommandsDrawerTest(t)
	// "ls" is ungrouped (index 0); "pwd" is in group "shell" (index 1).
	if order := state.commandDisplayOrder(); len(order) != 2 {
		t.Fatalf("both commands should show by default: %v", order)
	}
	// Hide the "shell" group: only the ungrouped command remains.
	state.commands.hiddenGroups = map[string]bool{"shell": true}
	if order := state.commandDisplayOrder(); len(order) != 1 || order[0] != 0 {
		t.Fatalf("hiding a group should leave only ungrouped commands: %v", order)
	}
}

// The filter dropdown's checkboxes route through the central handler and flip the
// group's visibility; the menu stays open so several groups can be toggled at once.
func TestCommandGroupFilterToggle(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.commands.filterOpen = true // the menu is open while its checkboxes are clicked
	// Hide ungrouped first: only the grouped "pwd" should remain.
	clickPageMenuRow(t, state, state.messages.Tools.CommandsUngrouped)
	if !state.commands.hiddenGroups[""] {
		t.Fatal("ungrouped checkbox did not hide ungrouped commands")
	}
	if order := state.commandDisplayOrder(); len(order) != 1 || order[0] != 1 {
		t.Fatalf("hiding ungrouped should leave only the grouped command: %v", order)
	}
	// Re-checking ungrouped brings "ls" back; the menu never closed on the click.
	clickPageMenuRow(t, state, state.messages.Tools.CommandsUngrouped)
	if state.commands.hiddenGroups[""] {
		t.Fatal("re-checking ungrouped did not show ungrouped commands")
	}
	if order := state.commandDisplayOrder(); len(order) != 2 {
		t.Fatalf("re-showing ungrouped should restore both commands: %v", order)
	}
	if !state.commands.filterOpen {
		t.Fatal("the filter menu closed after a checkbox toggle")
	}
}

// Show all clears every hidden group; Hide all hides the ungrouped bucket and every
// named group the snippets name.
func TestCommandGroupFilterShowAndHideAll(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.commands.filterOpen = true
	clickPageMenuRow(t, state, state.messages.Tools.CommandsHideAll)
	if !state.commandGroupVisible("does-not-exist") {
		t.Fatal("Hide all should have hidden every named group")
	}
	if !state.commands.hiddenGroups[""] {
		t.Fatal("Hide all should have hidden ungrouped commands")
	}
	if order := state.commandDisplayOrder(); len(order) != 0 {
		t.Fatalf("Hide all should leave the bar empty: %v", order)
	}
	clickPageMenuRow(t, state, state.messages.Tools.CommandsShowAll)
	if len(state.commands.hiddenGroups) != 0 {
		t.Fatalf("Show all should clear the hidden groups: %v", state.commands.hiddenGroups)
	}
	if order := state.commandDisplayOrder(); len(order) != 2 {
		t.Fatalf("Show all should restore every command: %v", order)
	}
}

// The bar orders its chips by the chosen sort: creation order keeps the list
// order, name sorts alphabetically, and group sorts by group then name.
func TestCommandSort(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.snippets.list = []snippet{
		{Name: "zeta", Command: "zeta", Group: "b"},
		{Name: "alpha", Command: "alpha", Group: "a"},
		{Name: "beta", Command: "beta"},
	}
	names := func() string {
		order := state.commandDisplayOrder()
		out := make([]string, len(order))
		for i, index := range order {
			out[i] = state.snippets.list[index].Name
		}
		return strings.Join(out, ",")
	}
	// Creation order is the list order.
	if got := names(); got != "zeta,alpha,beta" {
		t.Fatalf("creation order = %q", got)
	}
	// Alphabetical by name.
	state.commands.sort = commandsSortName
	if got := names(); got != "alpha,beta,zeta" {
		t.Fatalf("name order = %q", got)
	}
	// Group first (ungrouped sorts before any named group), then name.
	state.commands.sort = commandsSortGroup
	if got := names(); got != "beta,alpha,zeta" {
		t.Fatalf("group order = %q", got)
	}
	for _, sort := range settingsCommandsSortChoices {
		if commandsSortFromString(sort.String()) != sort {
			t.Fatalf("sort %v did not round-trip through its saved string", sort)
		}
	}
}

// The order lives on the bar itself: its own button opens a menu of orders,
// picking one applies it and closes the menu, and the two menus never show at
// once.
func TestCommandSortMenuPicksAnOrder(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.Click(hitCommandsSort)
	if !state.commands.sortOpen {
		t.Fatal("the order button did not open its menu")
	}
	// Opening the filter closes the order menu, so the bar shows one at a time.
	state.Click(hitCommandsFilter)
	if state.commands.sortOpen || !state.commands.filterOpen {
		t.Fatalf("two menus open at once: order=%v filter=%v", state.commands.sortOpen, state.commands.filterOpen)
	}
	state.Click(hitCommandsSort)
	state.commands.offset = 40
	clickPageMenuRow(t, state, state.settingsCommandsSortLabel(settingsCommandsSortChoices[1]))
	if state.commands.sort != settingsCommandsSortChoices[1] {
		t.Fatalf("picking an order set %v", state.commands.sort)
	}
	if state.commands.sortOpen {
		t.Fatal("picking an order left the menu open")
	}
	if state.commands.offset != 0 {
		t.Fatalf("a new order kept the old scroll: %v", state.commands.offset)
	}
}

// Both dropdowns the bar opens are painted above it with every row intact. A
// fixed height that ignores the heading row and the divider clips the last
// action, which is the easiest way for these menus to look broken.
func TestCommandMenusKeepTheirLastRowAboveTheBar(t *testing.T) {
	tests := []struct {
		name  string
		open  func(*State)
		label func(*State) string
	}{
		{"filter", func(state *State) { state.commands.filterOpen = true }, func(state *State) string {
			return state.messages.Tools.CommandsHideAll
		}},
		{"sort", func(state *State) { state.commands.sortOpen = true }, func(state *State) string {
			return state.settingsCommandsSortLabel(settingsCommandsSortChoices[len(settingsCommandsSortChoices)-1])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := openCommandsDrawerTest(t)
			test.open(state)
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
			label := test.label(state)
			var row, surface gfx.Hit
			for _, hit := range canvas.Hits() {
				switch {
				case hit.Role == gfx.RoleMenuItem && hit.Label == label:
					row = hit
				case hit.Role == gfx.RoleMenu:
					surface = hit
				}
			}
			if row.ID.IsZero() {
				t.Fatal("the menu's last row was not painted")
			}
			if row.Bounds.Y+row.Bounds.Height > surface.Bounds.Y+surface.Bounds.Height+1 {
				t.Fatalf("the last row %v falls outside the menu surface %v", row.Bounds, surface.Bounds)
			}
			if row.Bounds.Y >= state.commands.bar.Y {
				t.Fatalf("the menu opened below the bar: %v vs %v", row.Bounds, state.commands.bar)
			}
		})
	}
}

// A wheel over the bar moves its chips sideways, stops at both ends, and stays
// out of the way when the chips already fit or a menu is open.
func TestCommandBarScroll(t *testing.T) {
	state := openCommandsDrawerTest(t)
	bar := gfx.Rect{X: 0, Y: 860, Width: 1400, Height: commandsBarHeight}
	state.commands.bar = bar
	state.commands.maxOffset = commandsScrollStep * 2
	over := gfx.ScrollEvent{X: 400, Y: bar.Y + 10, DeltaY: 1}
	if _, handled := state.scrollCommandsBar(over); !handled {
		t.Fatal("a wheel over the bar was not taken")
	}
	if state.commands.offset != commandsScrollStep {
		t.Fatalf("a notch moved the chips to %v, want %v", state.commands.offset, commandsScrollStep)
	}
	// The end of the strip is the largest offset that still shows a chip.
	state.scrollCommandsBar(over)
	state.scrollCommandsBar(over)
	if state.commands.offset != state.commands.maxOffset {
		t.Fatalf("scrolling past the end stopped at %v, want %v", state.commands.offset, state.commands.maxOffset)
	}
	state.scrollCommandsBar(gfx.ScrollEvent{X: 400, Y: bar.Y + 10, DeltaY: -1})
	if state.commands.offset >= state.commands.maxOffset {
		t.Fatal("a wheel the other way did not scroll back")
	}
	// Nothing past the bar's ends, nothing when the chips fit.
	if _, handled := state.scrollCommandsBar(gfx.ScrollEvent{X: 400, Y: bar.Y - 40, DeltaY: 1}); handled {
		t.Fatal("a wheel above the bar was taken by it")
	}
	state.commands.maxOffset = 0
	if _, handled := state.scrollCommandsBar(over); handled {
		t.Fatal("a wheel was taken although every chip fits")
	}
}

// The bar's opt-in survives a restart, hiding it does too, and a fresh profile
// starts with the bar hidden.
func TestCommandBarOptInSurvivesRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.commands.shown = true
	state.save()
	again := newTestState(t, options)
	if !again.commands.shown {
		t.Fatal("after a restart the opted-in bar was hidden again")
	}
	again.commands.shown = false
	again.save()
	hidden := newTestState(t, options)
	if hidden.commands.shown {
		t.Fatal("after hiding the bar a restart showed it again")
	}

	fresh := newTestState(t, testOptions(t))
	if fresh.commands.shown {
		t.Fatal("a new profile must start with the bar hidden")
	}
}

// The bar is the content area's bottom strip, so a file panel docked beside the
// terminal must not narrow it: the corners beside the terminal column are the
// bar's, not blank. This is #981 — the strip used to stop at the terminal column
// and leave a gap under each side panel.
func TestCommandBarSpansTheContentWidthBesideAFilePanel(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.snippets.list = []snippet{{Name: "pwd", Command: "pwd"}}
	state.commands.shown = true
	size := gfx.Size{Width: 1444, Height: 808}
	canvas, _ := paintAt(state, size)
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || context.Side != layoutcore.ContextLeft {
		t.Fatalf("the files panel is not docked left: %+v", context)
	}
	full := state.fullWorkspacePixelBounds(size)
	frame := state.toolbarDockFrame(size, true)
	bar := state.commands.bar
	if bar.Width <= 0 || bar.Height <= 0 {
		t.Fatalf("the bar was not painted: %+v", bar)
	}
	if bar.X != full.X || bar.Width != full.Width {
		t.Fatalf("bar spans %v..%v, want the content frame %v..%v", bar.X, bar.X+bar.Width, full.X, full.X+full.Width)
	}
	if bar.Y != frame.Y+frame.Height {
		t.Fatalf("bar top = %v, want the dock frame's bottom %v", bar.Y, frame.Y+frame.Height)
	}
	// Both bottom corners of the content frame are painted by the strip: the top
	// rectangle at each corner is the bar's own fill, so no side panel covers it
	// and no gap is left beside it.
	want := withAlpha(state.pal.shell, 0.6)
	mid := bar.Y + commandsBarHeight/2
	for _, corner := range []gfx.Point{{X: full.X + 2, Y: mid}, {X: full.X + full.Width - 3, Y: mid}} {
		var top gfx.Rectangle
		found := false
		for _, r := range canvas.Rectangles() {
			if corner.X >= r.Bounds.X && corner.X < r.Bounds.X+r.Bounds.Width &&
				corner.Y >= r.Bounds.Y && corner.Y < r.Bounds.Y+r.Bounds.Height {
				top, found = r, true
			}
		}
		if !found || top.Color != want {
			t.Fatalf("bottom corner %v is %+v, want the bar fill %+v", corner, top.Color, want)
		}
	}
}

// A terminal toolbar docked to a side takes its rail width out of the panes,
// but the command strip still spans the content frame under the rail: the rail
// ends at the strip's top, so the corner below it belongs to the bar.
func TestCommandBarSpansTheContentWidthBesideASideRail(t *testing.T) {
	state, _ := monitorFixture(t)
	state.snippets.list = []snippet{{Name: "pwd", Command: "pwd"}}
	state.commands.shown = true
	state.terminalToolbarPosition = terminalToolbarLeft
	size := gfx.Size{Width: 1444, Height: 808}
	canvas, _ := paintAt(state, size)

	full := state.fullWorkspacePixelBounds(size)
	frame := state.toolbarDockFrame(size, true)
	if frame.Width >= full.Width {
		t.Fatalf("the side rail did not narrow the dock frame (%v vs %v); the case proves nothing", frame.Width, full.Width)
	}
	bar := state.commands.bar
	if bar.Width <= 0 || bar.Height <= 0 {
		t.Fatalf("the bar was not painted: %+v", bar)
	}
	if bar.X != full.X || bar.Width != full.Width {
		t.Fatalf("bar spans %v..%v, want the content frame %v..%v", bar.X, bar.X+bar.Width, full.X, full.X+full.Width)
	}
	want := withAlpha(state.pal.shell, 0.6)
	mid := bar.Y + commandsBarHeight/2
	for _, corner := range []gfx.Point{{X: full.X + 2, Y: mid}, {X: full.X + full.Width - 3, Y: mid}} {
		var top gfx.Rectangle
		found := false
		for _, r := range canvas.Rectangles() {
			if corner.X >= r.Bounds.X && corner.X < r.Bounds.X+r.Bounds.Width &&
				corner.Y >= r.Bounds.Y && corner.Y < r.Bounds.Y+r.Bounds.Height {
				top, found = r, true
			}
		}
		if !found || top.Color != want {
			t.Fatalf("bottom corner %v is %+v, want the bar fill %+v", corner, top.Color, want)
		}
	}
}
