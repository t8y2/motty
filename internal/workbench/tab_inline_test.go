package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestAnIdleEditorMatchesNoTab(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)

	// A file tab has no workspace: its ID is zero, which must not read as the
	// idle editor's empty workspace and paint it as a name field.
	if state.tabInlineEditing(0) {
		t.Fatal("an idle editor claims the tab with no workspace")
	}
	if state.tabInlineEditing(state.workspaceTabs[0].ID) {
		t.Fatal("an idle editor claims a workspace tab")
	}
}

func TestDoubleClickOnATabStartsTheInlineRename(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID
	label := state.tabLabel(state.workspaceTabs[0])

	result := pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: hitTabList.At(int(workspace)), ClickCount: 2, X: 100, Y: 18,
	})
	if !result.Handled {
		t.Fatal("the double click was left to the tab underneath")
	}
	if !state.tabInlineActive() || state.tabInline.workspace != workspace {
		t.Fatalf("the name field is not open on the tab: %+v", state.tabInline)
	}
	if state.tabDrag.pressed {
		t.Fatal("the double click started a drag of the tab")
	}
	if got := state.tabInline.editor.String(); got != label {
		t.Fatalf("the field starts with %q, want the tab's label %q", got, label)
	}
}

func TestTabInlineRefusesToOpenWhileAModalOwnsTheWindow(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID

	state.modal = modalNewHost
	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: hitTabList.At(int(workspace)), ClickCount: 2, X: 100, Y: 18,
	})
	if state.tabInlineActive() {
		t.Fatal("a double click opened the name field under a modal")
	}
}

func TestTabInlineCommitWaitsForTheComposition(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID
	before := state.workspaceTabs[0].Title

	state.beginTabInline(workspace)
	state.tabInline.editor.SetMarkedText("生产环境", text.Range{End: 5})
	if state.commitTabInline() {
		t.Fatal("a composing field committed")
	}
	if !state.tabInlineActive() || state.workspaceTabs[0].Title != before {
		t.Fatalf("the composing field did not stay as it was: active=%t title=%q", state.tabInlineActive(), state.workspaceTabs[0].Title)
	}

	state.tabInline.editor.Unmark()
	if !state.commitTabInline() {
		t.Fatal("the settled composition did not commit")
	}
	if state.tabInlineActive() {
		t.Fatal("the field stayed open after committing")
	}
	if got := state.workspaceTabs[0].Title; got != "生产环境" {
		t.Fatalf("tab title = %q, want the composed name", got)
	}
}

func TestTabInlineScrollsALongNameUnderItsTab(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID
	var canvas gfx.Canvas
	title := gfx.Rect{Width: 134, Height: 20}

	state.beginTabInline(workspace)
	if got := tabInlineScroll(&canvas, title, state.tabInline.editor.String(), 0); got != 0 {
		t.Fatalf("a name that fits scrolls: %v", got)
	}
	state.tabInline.editor.Insert(strings.Repeat("a long name ", 20))
	end := state.tabInline.editor.ByteOffset(state.tabInline.editor.Caret())
	scroll := tabInlineScroll(&canvas, title, state.tabInline.editor.String(), end)
	if scroll <= 0 {
		t.Fatal("a caret past the field does not scroll the line")
	}
	width := canvas.TextCaretStyle(state.tabInline.editor.String(), gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium}, end)
	if got := width - scroll; got > title.Width {
		t.Fatalf("the caret is still past the field: %v > %v", got, title.Width)
	}

	// Opening on a long name selects all of it with the caret at its end, so
	// the line opens at the end too: that is where typing replaces the name.
	long := state.tabInline.editor.String()
	state.tabInline = tabInline{workspace: workspace}
	state.tabInline.editor.Insert(long)
	state.tabInline.editor.SetSelection(text.Range{End: state.tabInline.editor.RuneCount()})
	if got := tabInlineScroll(&canvas, title, long, state.tabInline.editor.ByteOffset(state.tabInline.editor.Caret())); got <= 0 {
		t.Fatalf("a select-all does not scroll to its caret: %v", got)
	}
}

func TestTabInlineFieldHidesThePlatformCaret(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID

	state.beginTabInline(workspace)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, hitTabInline)
	if !ok {
		t.Fatal("the name field declares no hit")
	}
	if hit.Role != gfx.RoleTextField {
		t.Fatalf("the field's role = %v", hit.Role)
	}
	// The platform hides its caret while text is selected, and a rename opens
	// with the whole name selected. The field must therefore paint the caret
	// itself, from the platform's text only for typing and the input method.
	if !hit.HideNativeOverlays {
		t.Fatal("the field leaves the caret to the platform, which hides it while the name is selected")
	}
}

func TestTabInlineWorksWithVerticalTabs(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID

	state.setTabOrientation(tabsVertical)
	state.beginTabInline(workspace)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTabInline); !ok {
		t.Fatal("the sidebar's row holds no name field")
	}
	state.tabInline.editor.Insert("侧栏改名")
	state.commitTabInline()
	if got := state.tabLabel(state.workspaceTabs[0]); got != "侧栏改名" {
		t.Fatalf("tab label = %q", got)
	}
}

func TestTabInlineClosesWhenItsWorkspaceIsGone(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID

	state.beginTabInline(workspace)
	state.closeWorkspace(workspace)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.tabInlineActive() {
		t.Fatal("the field stayed open on a closed workspace")
	}
}
