package workbench

import (
	"slices"
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

func TestCommandBarRepeatsWithoutTakingTerminalFocus(t *testing.T) {
	for _, placement := range commandsPlacementChoices {
		t.Run(placementName(placement), func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			state.commands.shown = true
			state.commands.placement = placement
			state.snippets.list = []snippet{
				{Name: "List", Command: "ls", Run: true},
				{Name: "Directory", Command: "pwd"},
			}
			view := openPickerView(t, state, 1)
			terminalID := terminalHit(state, 0)
			terminal := dualHit(t, view, terminalID)
			if err := view.Click(terminal.X+20, terminal.Y+20); err != nil {
				t.Fatal(err)
			}
			if focused := view.Snapshot().Focused; focused != terminalID {
				t.Fatalf("terminal did not receive focus: %v", focused)
			}
			for _, index := range []int{0, 0, 1} {
				chip := dualHit(t, view, hitCommandsTag.At(index))
				if overlap := intersectRect(terminal, chip); overlap.Width > 0 && overlap.Height > 0 {
					t.Fatalf("command chip overlaps terminal: chip=%+v terminal=%+v", chip, terminal)
				}
				if err := view.Click(chip.X+chip.Width/2, chip.Y+chip.Height/2); err != nil {
					t.Fatal(err)
				}
				if focused := view.Snapshot().Focused; focused != terminalID {
					t.Fatalf("command stole terminal focus: %v", focused)
				}
			}
			if got := shell.typed(); got != "ls\rls\rpwd" {
				t.Fatalf("terminal received %q, want two runs and one insert", got)
			}
		})
	}
}

// placementName is a subtest name for the bar's two docks.
func placementName(placement commandsPlacement) string {
	if placement == commandsPlacementSide {
		return "side"
	}
	return "bottom"
}

func TestCommandBarKeyboardActivationReturnsToTerminal(t *testing.T) {
	for _, key := range []string{"enter", "space"} {
		t.Run(key, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			state.commands.shown = true
			state.snippets.list = []snippet{{Name: "List", Command: "ls", Run: true}}
			view := openPickerView(t, state, 1)
			sort := dualHit(t, view, hitCommandsSort)
			if err := view.Click(sort.X+sort.Width/2, sort.Y+sort.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Keys("escape"); err != nil {
				t.Fatal(err)
			}
			for count := 0; count < 30 && view.Snapshot().Focused != hitCommandsTag.At(0); count++ {
				if err := view.Keys("tab"); err != nil {
					t.Fatal(err)
				}
			}
			if got := view.Snapshot().Focused; got != hitCommandsTag.At(0) {
				t.Fatalf("tab did not reach the command: %v", got)
			}
			if err := view.Keys(key); err != nil {
				t.Fatal(err)
			}
			if got := view.Snapshot().Focused; got != terminalHit(state, 0) {
				t.Fatalf("focus after activating command = %v", got)
			}
			if got := shell.typed(); got != "ls\r" {
				t.Fatalf("terminal received %q, want command", got)
			}
			if err := view.Type("after"); err != nil {
				t.Fatal(err)
			}
			if got := shell.typed(); got != "ls\rafter" {
				t.Fatalf("typing after activation went to %q", got)
			}
		})
	}
}

func TestCommandBarVariableDialogKeepsKeyboard(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.commands.shown = true
	state.snippets.list = []snippet{{Name: "Deploy", Command: "deploy {{version}}", Run: true}}
	view := openPickerView(t, state, 1)
	chip := dualHit(t, view, hitCommandsTag.At(0))
	if err := view.Click(chip.X+chip.Width/2, chip.Y+chip.Height/2); err != nil {
		t.Fatal(err)
	}
	if got := view.Snapshot().Focused; got != hitSnippetVarList.At(0) {
		t.Fatalf("focus after opening variables = %v", got)
	}
	if err := view.Type("v1"); err != nil {
		t.Fatal(err)
	}
	if got := shell.typed(); got != "" {
		t.Fatalf("typing in dialog reached terminal: %q", got)
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	if got := shell.typed(); got != "deploy v1\r" {
		t.Fatalf("terminal received %q, want expanded command", got)
	}
	if got := view.Snapshot().Focused; got != terminalHit(state, 0) {
		t.Fatalf("focus after submission = %v", got)
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
	if got := canvas.FocusOnce(); got != want {
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
	if got := canvas.FocusOnce(); !got.IsZero() {
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
	if got := canvas.FocusOnce(); !got.IsZero() {
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
	if state.activeSection != sectionSnippets || state.snippets.selected != 1 || state.pendingFocus != hitSnippetInlineName {
		t.Fatalf("edit from the bar: section=%d selected=%d focus=%v", state.activeSection, state.snippets.selected, state.pendingFocus)
	}
	state.activeSection = sectionTerminal
	state.snippetMenu = snippetMenu{}

	pointer(state, event)
	chooseMenu(t, state, hitSnippetMenuDelete)
	if state.modal != modalSnippetDelete || len(state.snippets.list) != 2 {
		t.Fatalf("delete from the bar opened modal %d with %d snippets", state.modal, len(state.snippets.list))
	}
}

// Sending through the bar's right-click menu is another way the bar acts on
// the terminal, so it must hand the keyboard back like a chip click does.
func TestSendingFromACommandChipMenuReturnsTheKeyboardToTheTerminal(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionTerminal
	state.snippets.list = []snippet{{Name: "ls", Command: "ls", Run: true}}
	state.commands.shown = true
	want := state.activeTerminalFocusHit()
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitCommandsTag.At(0)})
	chooseMenu(t, state, hitSnippetMenuSend)
	if state.pendingFocus != want {
		t.Fatalf("pending focus = %v, want the terminal %v", state.pendingFocus, want)
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

// The bar reserves the strip it is docked in, and only while the terminal page
// can host it: a bottom strip takes height from every panel, a sidebar takes
// width from the terminals' own column.
func TestCommandBarBoundsFollowItsDock(t *testing.T) {
	state := openCommandsDrawerTest(t)
	size := gfx.Size{Width: 1400, Height: 900}
	state.activeSection = sectionHosts
	if height := state.commandsDrawerHeight(); height != 0 {
		t.Fatalf("bottom bar reserved space off the terminal page: %v", height)
	}
	if bounds := state.commandsSidebarBounds(size); bounds.Width != 0 {
		t.Fatalf("sidebar reserved space off the terminal page: %v", bounds)
	}

	state.activeSection = sectionTerminal
	state.commands.shown = false
	if height := state.commandsDrawerHeight(); height != 0 {
		t.Fatalf("a fresh, hidden bar reserved space: %v", height)
	}

	// The default dock is the strip under the panes, so the terminals give up
	// its height, not their width.
	state.commands.shown = true
	state.viewport = size
	full := state.fullWorkspacePixelBounds(size)
	bar := state.commandsBarRect()
	frame := state.toolbarDockFrame(size, true)
	content := state.baseWorkspacePixelBounds(size)
	if bar.Height != commandsBarHeight || bar.Width != full.Width || bar.X != full.X ||
		bar.Y != frame.Y+frame.Height || bar.Y+bar.Height != size.Height ||
		content.Width != full.Width || state.commandsSidebarBounds(size).Width != 0 {
		t.Fatalf("bottom bar=%+v frame=%+v content=%+v full=%+v", bar, frame, content, full)
	}

	// The side dock gives up width instead and keeps the strip's height free.
	state.setCommandsPlacement(commandsPlacementSide)
	sidebar := state.commandsSidebarBounds(size)
	frame = state.toolbarDockFrame(size, true)
	content = state.baseWorkspacePixelBounds(size)
	if sidebar.Width != commandsSidebarWidth || sidebar.X != frame.X+frame.Width-sidebar.Width ||
		content.X+content.Width != sidebar.X || sidebar.X+sidebar.Width != frame.X+frame.Width ||
		sidebar.Y != full.Y || sidebar.Y+sidebar.Height != size.Height {
		t.Fatalf("sidebar=%+v frame=%+v content=%+v full=%+v", sidebar, frame, content, full)
	}
	if height := state.commandsDrawerHeight(); height != 0 {
		t.Fatalf("side dock still reserved bottom height: %v", height)
	}
}

func TestCommandSidebarResizeDragsAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.deck = layoutcore.Deck{}
	if _, _, err := state.deck.Create("test"); err != nil {
		t.Fatal(err)
	}
	state.activeSection = sectionTerminal
	state.snippets.list = []snippet{{Name: "pwd", Command: "pwd"}}
	state.commands.shown = true
	state.commands.placement = commandsPlacementSide
	size := gfx.Size{Width: 1400, Height: 900}
	paintAt(state, size)
	bar := state.commands.bar
	if bar.Width != commandsSidebarWidth {
		t.Fatalf("default bar width = %v, want %v", bar.Width, commandsSidebarWidth)
	}
	content := state.baseWorkspacePixelBounds(size).Width
	down := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitCommandsResize, X: bar.X + 1, Y: bar.Y + 100, Viewport: size}
	if got := state.Pointer(down); !got.Handled {
		t.Fatal("pressing the bar's edge was not taken by the resize")
	}
	move := gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hitCommandsResize, X: bar.X - 59, Y: bar.Y + 100, Viewport: size, Captured: true}
	state.Pointer(move)
	paintAt(state, size)
	want := bar.Width + 60
	if got := state.commands.bar.Width; got != want {
		t.Fatalf("dragging the edge left gave %v, want %v", got, want)
	}
	if got := state.baseWorkspacePixelBounds(size).Width; got != content-60 {
		t.Fatalf("the terminals kept %v beside a wider bar, want %v", got, content-60)
	}
	up := gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hitCommandsResize, X: bar.X - 59, Y: bar.Y + 100, Viewport: size}
	if got := state.Pointer(up); !got.Handled {
		t.Fatal("releasing the edge was not taken by the resize")
	}
	again := newTestState(t, options)
	if again.commands.width != want {
		t.Fatalf("a restart restored width %v, want %v", again.commands.width, want)
	}
}

func TestCommandSidebarMovesBesideHostsAndRemembersItsSide(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.hostsPanel.open = true
	state.commands.shown = true
	state.commands.placement = commandsPlacementSide
	state.snippets.list = []snippet{{Name: "List", Group: "Shell", Command: "ls"}}
	view := openPickerView(t, state, 1)
	click := func(id gfx.ID) {
		t.Helper()
		rect := dualHit(t, view, id)
		if err := view.Click(rect.X+rect.Width/2, rect.Y+rect.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	size := state.viewport
	frame := state.toolbarDockFrame(size, true)
	hostEdge := frame.X + state.hostsPanelExtent(frame.Width)
	right := state.commands.bar
	if right.X+right.Width != frame.X+frame.Width {
		t.Fatalf("existing preferences should open on the right: %+v", right)
	}

	click(hitCommandsMove)
	left := state.commands.bar
	content := state.baseWorkspacePixelBounds(size)
	if left.X != hostEdge || left.X+left.Width != content.X || left.Width != right.Width {
		t.Fatalf("sidebar %+v should fit between hosts (ending at %v) and terminal content %+v", left, hostEdge, content)
	}
	if again := newTestState(t, state.options); !again.commands.sidebarLeft {
		t.Fatal("left side was not restored from disk")
	}
	grip := dualHit(t, view, hitCommandsResize)
	if grip.X+grip.Width != content.X {
		t.Fatalf("resize grip %+v should be on the terminal edge at %v", grip, content.X)
	}
	x, y := grip.X+1, grip.Y+100
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x+30, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x+30, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.commands.width != left.Width+30 {
		t.Fatalf("dragging the left sidebar to the right gave width %v", state.commands.width)
	}
	click(hitCommandsFilter)
	if !state.commands.filterOpen {
		t.Fatal("filter menu did not open after resizing")
	}
	filter := dualHitLabel(t, view, state.messages.Tools.CommandsShowAll)
	if filter.X < state.commands.bar.X+state.commands.bar.Width || filter.X+filter.Width > size.Width {
		t.Fatalf("filter menu %+v should open to the right of sidebar %+v", filter, state.commands.bar)
	}
	if err := view.Click(size.Width/2, size.Height-40); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click(hitCommandsMove)
	if state.commands.sidebarLeft || !state.commands.shown || shell.typed() != "" {
		t.Fatal("moving back should keep the command sidebar open without sending input")
	}
	if again := newTestState(t, state.options); again.commands.sidebarLeft {
		t.Fatal("moving back did not restore the right side on restart")
	}
}

func TestCommandSidebarDoesNotCoverTheRightToolbar(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.commands.shown = true
	state.commands.placement = commandsPlacementSide
	state.snippets.list = []snippet{{Name: "List", Command: "ls"}}
	view := openPickerView(t, state, 1)
	bar := state.commandsSidebarBounds(state.viewport)
	button := dualHit(t, view, hitTerminalSnippets)
	if overlap := intersectRect(bar, button); overlap.Width > 0 && overlap.Height > 0 {
		t.Fatalf("command sidebar %+v covers toolbar button %+v", bar, button)
	}
	terminal := dualHit(t, view, terminalHit(state, 0))
	if overlap := intersectRect(terminal, button); overlap.Width > 0 && overlap.Height > 0 {
		t.Fatalf("toolbar button %+v covers terminal %+v", button, terminal)
	}
}

// The close control hides the whole bar, freeing its reserved strip. The
// snippets button keeps its one meaning (open the picker) while the bar is
// hidden; the picker's own checkbox switches it back without closing the
// dialog.
func TestCommandDrawerCloseAndReopen(t *testing.T) {
	state := openCommandsDrawerTest(t)
	if state.commandsDrawerHeight() != commandsBarHeight {
		t.Fatal("drawer was not open before closing")
	}
	state.Click(hitCommandsClose)
	if state.commands.shown {
		t.Fatal("close did not hide the drawer")
	}
	if height := state.commandsDrawerHeight(); height != 0 {
		t.Fatalf("closed drawer still reserved space: %v", height)
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
	if height := state.commandsDrawerHeight(); height != commandsBarHeight {
		t.Fatalf("reopened bar height = %v, want %v", height, commandsBarHeight)
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

// The bar lists its groups, then the commands in each, by the chosen sort:
// creation order keeps the list order, name sorts groups and commands
// alphabetically, and group sorts only the groups. Ungrouped commands close the
// list in every order.
func TestCommandSort(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.snippets.list = []snippet{
		{Name: "zeta", Command: "zeta", Group: "b"},
		{Name: "loose", Command: "loose"},
		{Name: "yak", Command: "yak", Group: "a"},
		{Name: "beta", Command: "beta", Group: "b"},
		{Name: "alpha", Command: "alpha", Group: "a"},
	}
	names := func() string {
		order := state.commandDisplayOrder()
		out := make([]string, len(order))
		for i, index := range order {
			out[i] = state.snippets.list[index].Name
		}
		return strings.Join(out, ",")
	}
	// Groups sit where their first command is; commands keep the list order.
	if got := names(); got != "zeta,beta,yak,alpha,loose" {
		t.Fatalf("creation order = %q", got)
	}
	state.commands.sort = commandsSortName
	if got := names(); got != "alpha,yak,beta,zeta,loose" {
		t.Fatalf("name order = %q", got)
	}
	state.commands.sort = commandsSortGroup
	if got := names(); got != "yak,alpha,zeta,beta,loose" {
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

// Both dropdowns the bar opens are painted clear of it with every row intact: a
// fixed height that ignores the heading row and the divider clips the last
// action, which is the easiest way for these menus to look broken.
func TestCommandMenusKeepTheirLastRowBesideTheBar(t *testing.T) {
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
	for _, placement := range commandsPlacementChoices {
		for _, test := range tests {
			t.Run(placementName(placement)+"/"+test.name, func(t *testing.T) {
				state := openCommandsDrawerTest(t)
				state.commands.placement = placement
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
				bar := state.commands.bar
				if state.commands.placement == commandsPlacementSide {
					if surface.Bounds.X+surface.Bounds.Width > bar.X {
						t.Fatalf("the menu overlaps the sidebar: %v vs %v", surface.Bounds, bar)
					}
				} else if surface.Bounds.Y+surface.Bounds.Height > bar.Y {
					t.Fatalf("the menu overlaps the bottom bar: %v vs %v", surface.Bounds, bar)
				}
			})
		}
	}
}

// A wheel over the bar moves its chips, stops at both ends, and stays out of the
// way when the chips already fit or a menu is open.
func TestCommandBarScroll(t *testing.T) {
	state := openCommandsDrawerTest(t)
	bar := gfx.Rect{X: 1176, Y: 48, Width: commandsSidebarWidth, Height: 852}
	state.commands.bar = bar
	state.commands.maxOffset = commandsScrollStep * 2
	over := gfx.ScrollEvent{X: bar.X + 10, Y: bar.Y + 40, DeltaY: 1}
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
	state.scrollCommandsBar(gfx.ScrollEvent{X: over.X, Y: over.Y, DeltaY: -1})
	if state.commands.offset >= state.commands.maxOffset {
		t.Fatal("a wheel the other way did not scroll back")
	}
	// Nothing past the bar's ends, nothing when the chips fit.
	if _, handled := state.scrollCommandsBar(gfx.ScrollEvent{X: bar.X - 40, Y: over.Y, DeltaY: 1}); handled {
		t.Fatal("a wheel beside the sidebar was taken by it")
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
	// The default dock restores as the bottom strip; a chosen side dock is saved.
	state.setCommandsPlacement(commandsPlacementSide)
	again = newTestState(t, options)
	if again.commands.placement != commandsPlacementSide {
		t.Fatalf("after a restart the side dock became %v", again.commands.placement)
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
	if fresh.commands.placement != commandsPlacementBottom {
		t.Fatalf("a new profile started with the %v dock, want the bottom strip", fresh.commands.placement)
	}
}

func TestCommandSidebarSpansTheContentHeightBesideAFilePanel(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.snippets.list = []snippet{{Name: "pwd", Command: "pwd"}}
	state.commands.shown = true
	state.commands.placement = commandsPlacementSide
	size := gfx.Size{Width: 1444, Height: 808}
	canvas, _ := paintAt(state, size)
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || context.Side != layoutcore.ContextLeft {
		t.Fatalf("the files panel is not docked left: %+v", context)
	}
	assertCommandSidebarSitsInsideTheDockFrame(t, state, canvas, size)
}

func TestCommandSidebarSpansTheContentHeightBesideASideRail(t *testing.T) {
	state, _ := monitorFixture(t)
	state.snippets.list = []snippet{{Name: "pwd", Command: "pwd"}}
	state.commands.shown = true
	state.commands.placement = commandsPlacementSide
	state.terminalToolbarPosition = terminalToolbarRight
	size := gfx.Size{Width: 1444, Height: 808}
	canvas, _ := paintAt(state, size)

	full := state.fullWorkspacePixelBounds(size)
	frame := state.toolbarDockFrame(size, true)
	if frame.Width >= full.Width {
		t.Fatalf("the side rail did not narrow the dock frame (%v vs %v); the case proves nothing", frame.Width, full.Width)
	}
	assertCommandSidebarSitsInsideTheDockFrame(t, state, canvas, size)
}

func assertCommandSidebarSitsInsideTheDockFrame(t *testing.T, state *State, canvas *gfx.Canvas, size gfx.Size) {
	t.Helper()
	frame := state.toolbarDockFrame(size, true)
	content := state.baseWorkspacePixelBounds(size)
	bar := state.commands.bar
	if bar.Width <= 0 || bar.X != frame.X+frame.Width-bar.Width || bar.X+bar.Width != frame.X+frame.Width ||
		content.X+content.Width != bar.X || bar.Y != frame.Y || bar.Y+bar.Height != size.Height {
		t.Fatalf("sidebar=%+v frame=%+v content=%+v viewport=%+v", bar, frame, content, size)
	}
	if right := state.toolbarDockSideReserveFor(false, true); right > 0 && bar.X+bar.Width != size.Width-right {
		t.Fatalf("sidebar %+v does not stop at the right toolbar rail (%v)", bar, right)
	}
	want := withAlpha(state.pal.shell, 0.6)
	x := bar.X + 2
	for _, y := range []float32{bar.Y + 2, size.Height - 2} {
		var top gfx.Rectangle
		found := false
		for _, r := range canvas.Rectangles() {
			if x >= r.Bounds.X && x < r.Bounds.X+r.Bounds.Width &&
				y >= r.Bounds.Y && y < r.Bounds.Y+r.Bounds.Height {
				top, found = r, true
			}
		}
		if !found || top.Color != want {
			t.Fatalf("sidebar edge at (%v, %v) is %+v, want %+v", x, y, top.Color, want)
		}
	}
}

// Each group is listed under its own heading, and a heading folds its commands
// away without taking the group out of the bar. Folding is a display state that
// outlives a restart.
func TestCommandGroupHeadingFoldsItsCommands(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.snippets.list = []snippet{
		{Name: "loose", Command: "loose"},
		{Name: "mysql-a", Command: "a", Group: "mysql"},
		{Name: "mysql-b", Command: "b", Group: "MySQL"},
		{Name: "os-a", Command: "c", Group: "os"},
	}
	sections := state.commandSections()
	if len(sections) != 3 || sections[0].key != "mysql" || sections[1].key != "os" || sections[2].key != "" {
		t.Fatalf("sections = %+v, want mysql and os, then ungrouped last", sections)
	}
	if len(sections[0].items) != 2 {
		t.Fatalf("groups differing in case must share a section: %+v", sections[0])
	}
	if !commandSectionsGrouped(sections) {
		t.Fatal("named groups must show headings")
	}
	if commandSectionsGrouped([]commandSection{{key: ""}}) {
		t.Fatal("a list nobody grouped needs no heading")
	}

	if !state.clickCommandsDrawer(hitCommandsGroupHeading.At(0)) {
		t.Fatal("the heading is not part of the bar")
	}
	if !state.commandSections()[0].collapsed {
		t.Fatal("clicking the heading did not fold its group")
	}
	state.commands.shown = true
	// A folded group still counts for the order and the filter; only the
	// painted rows leave.
	if len(state.commandDisplayOrder()) != 4 {
		t.Fatalf("a folded group left the bar: %v", state.commandDisplayOrder())
	}
	saved := state.snapshot()
	if !slices.Equal(saved.CommandsCollapsedGroups, []string{"mysql"}) {
		t.Fatalf("folded groups saved as %v", saved.CommandsCollapsedGroups)
	}
	state.clickCommandsDrawer(hitCommandsGroupHeading.At(0))
	if state.commandSections()[0].collapsed {
		t.Fatal("a second click did not unfold the group")
	}
}

func TestCommandBarNestedGroupFilterIncludesDescendants(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.snippets.list = []snippet{
		{Name: "prod", Command: "deploy prod", Group: "Clients/Acme/Prod"},
		{Name: "stage", Command: "deploy stage", Group: "Clients/Acme/Staging"},
		{Name: "top", Command: "top", Group: "Ops"},
	}
	groups, hasUngrouped := state.commandGroups()
	if hasUngrouped {
		t.Fatal("expected no ungrouped snippets")
	}
	wantGroups := []string{"Clients", "Clients/Acme", "Clients/Acme/Prod", "Clients/Acme/Staging", "Ops"}
	if !slices.Equal(groups, wantGroups) {
		t.Fatalf("commandGroups = %v, want %v", groups, wantGroups)
	}

	// Toggling Clients hides both Prod and Staging, leaving only Ops.
	state.Click(hitCommandsGroupToggle.At(0))
	if order := state.commandDisplayOrder(); !slices.Equal(order, []int{2}) {
		t.Fatalf("after hiding Clients: order = %v, want [2]", order)
	}
	if state.commandGroupVisible("Clients") || state.commandGroupVisible("Clients/Acme") || state.commandGroupVisible("Clients/Acme/Prod") {
		t.Fatal("hiding Clients did not hide its descendants")
	}

	// Re-enabling just Clients/Acme/Prod reveals it while keeping Staging hidden.
	state.Click(hitCommandsGroupToggle.At(2))
	if order := state.commandDisplayOrder(); !slices.Equal(order, []int{0, 2}) {
		t.Fatalf("after enabling Prod under hidden Clients: order = %v, want [0 2]", order)
	}
	if state.commandGroupVisible("Clients/Acme/Staging") {
		t.Fatal("enabling Prod should keep sibling Staging hidden")
	}

	// Re-enabling Clients restores all descendants.
	state.Click(hitCommandsGroupToggle.At(0)) // Clients is visible now -> hides
	state.Click(hitCommandsGroupToggle.At(0)) // Clients is hidden -> shows all
	if order := state.commandDisplayOrder(); len(order) != 3 {
		t.Fatalf("after re-enabling Clients: order = %v, want all 3", order)
	}
}

func TestCommandBarNestedGroupHeadingsFoldSubtreesInSidebarAndStrip(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.deck = layoutcore.Deck{}
	if _, _, err := state.deck.Create("test"); err != nil {
		t.Fatal(err)
	}
	state.activeSection = sectionTerminal
	state.commands.shown = true
	state.commands.placement = commandsPlacementSide
	state.snippets.list = []snippet{
		{Name: "prod", Command: "deploy prod", Group: "Clients/Acme/Prod"},
		{Name: "stage", Command: "deploy stage", Group: "Clients/Acme/Staging"},
		{Name: "top", Command: "top", Group: "Ops"},
	}
	state.refreshSnippets()

	sections := state.commandSections()
	wantKeys := []string{"clients", "clients/acme", "clients/acme/prod", "clients/acme/staging", "ops"}
	if len(sections) != len(wantKeys) {
		t.Fatalf("sections = %+v, want %v", sections, wantKeys)
	}
	for i, want := range wantKeys {
		if sections[i].key != want {
			t.Fatalf("sections[%d].key = %q, want %q", i, sections[i].key, want)
		}
	}
	if sections[0].label != "Clients" || sections[1].label != "Acme" || sections[2].label != "Prod" {
		t.Fatalf("section leaf labels = %q, %q, %q", sections[0].label, sections[1].label, sections[2].label)
	}
	if sections[0].depth != 0 || sections[1].depth != 1 || sections[2].depth != 2 {
		t.Fatalf("section depths = %d, %d, %d", sections[0].depth, sections[1].depth, sections[2].depth)
	}

	hitByID := func(c *gfx.Canvas, id gfx.ID) (gfx.Hit, bool) {
		for _, h := range c.Hits() {
			if h.ID == id {
				return h, true
			}
		}
		return gfx.Hit{}, false
	}

	// Sidebar indents nested headings by depth.
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	clientsHit, ok := hitByID(canvas, hitCommandsGroupHeading.At(0))
	acmeHit, ok2 := hitByID(canvas, hitCommandsGroupHeading.At(1))
	prodHit, ok3 := hitByID(canvas, hitCommandsGroupHeading.At(2))
	if !ok || !ok2 || !ok3 {
		t.Fatalf("expected nested heading hits: clients=%v acme=%v prod=%v", ok, ok2, ok3)
	}
	if !(clientsHit.Bounds.X < acmeHit.Bounds.X && acmeHit.Bounds.X < prodHit.Bounds.X) {
		t.Fatalf("expected increasing indent: clients=%v acme=%v prod=%v",
			clientsHit.Bounds.X, acmeHit.Bounds.X, prodHit.Bounds.X)
	}

	// Collapsing Clients hides Acme, Prod, Staging and their chips in both
	// the sidebar and the bottom strip, while keeping Ops visible.
	state.Click(hitCommandsGroupHeading.At(0))
	for _, placement := range commandsPlacementChoices {
		state.commands.placement = placement
		canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
		if _, found := hitByID(canvas, hitCommandsGroupHeading.At(1)); found {
			t.Fatalf("placement %d: Acme remained visible after collapsing Clients", placement)
		}
		if _, found := hitByID(canvas, hitCommandsTag.At(0)); found {
			t.Fatalf("placement %d: prod chip remained visible after collapsing Clients", placement)
		}
		if _, found := hitByID(canvas, hitCommandsTag.At(2)); !found {
			t.Fatalf("placement %d: Ops chip should remain visible when Clients is collapsed", placement)
		}
	}

	// Collapsed state survives restart.
	restarted := newTestState(t, options)
	if !restarted.commands.collapsedGroups["clients"] {
		t.Fatal("collapsed group state did not persist across restart")
	}
}

func TestCommandBarRestoresLegacyHiddenAndCollapsedSlashGroupKeys(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.restoreTerminalTools(savedState{
		Snippets: []savedSnippet{
			{Name: "build", Command: "make", Group: "CI/CD"},
			{Name: "deploy", Command: "deploy", Group: " CI / Deploy "},
			{Name: "root", Command: "ls /", Group: "/"},
			{Name: "top", Command: "top", Group: "Ops"},
			{Name: "plain", Command: "pwd"},
		},
		CommandsHiddenGroups:    []string{"ci/cd", " ci / deploy ", "/"},
		CommandsCollapsedGroups: []string{"ci/cd", " ci / deploy ", "/", ""},
	})

	if state.commandGroupVisible("CI/CD") || state.commandGroupVisible("CI/Deploy") || state.commandGroupVisible("／") {
		t.Fatalf("restored hidden slash keys did not hide their groups: %v", state.commands.hiddenGroups)
	}
	if !state.commandGroupVisible("Ops") || !state.commandGroupVisible("") {
		t.Fatalf("unrelated groups were hidden: %v", state.commands.hiddenGroups)
	}
	// Only Ops (3) and ungrouped (4) remain visible.
	if got := state.commandDisplayOrder(); !slices.Equal(got, []int{3, 4}) {
		t.Fatalf("visible order after restoring hidden slash keys = %v, want [3 4]", got)
	}
	for _, key := range []string{"ci/cd", "ci/deploy", "／", ""} {
		if !state.commands.collapsedGroups[key] {
			t.Fatalf("collapsed key %q was not restored: %v", key, state.commands.collapsedGroups)
		}
	}

	// Restoring a collapsed parent key folds its nested descendants too.
	state.restoreTerminalTools(savedState{
		Snippets: []savedSnippet{
			{Name: "build", Command: "make", Group: "CI/CD"},
			{Name: "deploy", Command: "deploy", Group: "CI/Deploy"},
		},
		CommandsCollapsedGroups: []string{"CI"},
	})
	sections := state.commandSections()
	if len(sections) != 3 || !sections[0].collapsed || !sections[1].ancestorCollapsed || !sections[2].ancestorCollapsed {
		t.Fatalf("restored collapsed parent did not fold descendants: %+v", sections)
	}
}
