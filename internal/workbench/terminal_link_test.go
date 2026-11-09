package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/i18n"
	"motty/internal/term"
)

func TestTerminalLinkUsesConfiguredExternalOpenerAndReportsFailure(t *testing.T) {
	opened := ""
	options := testOptions(t)
	options.OpenURL = func(target string) error {
		opened = target
		return errors.New("no handler")
	}
	state := newTestState(t, options)
	state.openTerminalLink("https://example.test")
	if opened != "https://example.test" || state.status != "Could not open link: no handler" {
		t.Fatalf("opened=%q status=%q", opened, state.status)
	}
}

func TestOSC8TerminalLinkUsesVifPublicOpenURL(t *testing.T) {
	driver, err := app.NewDriver(app.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	state, session, geometry := selectionState()
	state.terminalLinkPlainClick = true
	state.messages = i18n.For(i18n.English)
	state.options.OpenURL = app.OpenURL
	session.term.openLink = state.openTerminalLink
	session.term.write([]byte("go \x1b]8;id=docs;https://example.test/docs\x1b\\documentation\x1b]8;;\x07 now"))
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 5, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: x, Y: y, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: y, Viewport: state.viewport})
	if target, ok := driver.LastOpenedURL(); !ok || target != "https://example.test/docs" {
		t.Fatalf("LastOpenedURL = %q, %t", target, ok)
	}
}

func TestTerminalLinkHitHoverPaintAndClick(t *testing.T) {
	state, session, geometry := selectionState()
	state.terminalLinkPlainClick = true
	session.term.write([]byte("go https://example.test/a now"))
	var opened []string
	session.term.openLink = func(target string) { opened = append(opened, target) }
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 5, state.cellWidth, state.cellHeight)

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, geometry)
	if session.term.links.hasHover {
		t.Fatal("painting an unhovered terminal set a link")
	}
	terminalHit, ok := hitByID(&canvas, hit)
	if !ok || terminalHit.Role != gfx.RoleTerminal || terminalHit.Bounds != geometry.area {
		t.Fatalf("terminal hit = %+v, %v", terminalHit, ok)
	}

	hover := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, X: x, Y: y, Viewport: state.viewport})
	if !hover.Handled || !hover.Repaint {
		t.Fatalf("hover = %+v", hover)
	}
	link, ok := session.term.links.hoveredLink(session.term.screen)
	if !ok || link.Target != "https://example.test/a" || link.Start != (term.BufferPosition{Line: 0, Column: 3}) || link.End != (term.BufferPosition{Line: 1, Column: 5}) {
		t.Fatalf("hovered link = %+v, %v", link, ok)
	}

	canvas.Reset()
	state.paintGrid(&canvas, session, geometry)
	wantUnderlines := []gfx.Rect{
		{X: geometry.left + 3*state.cellWidth, Y: geometry.top + state.cellHeight - 1, Width: 17 * state.cellWidth, Height: 1},
		{X: geometry.left, Y: geometry.top + 2*state.cellHeight - 1, Width: 5 * state.cellWidth, Height: 1},
	}
	for _, want := range wantUnderlines {
		if !hasTerminalLinkUnderline(canvas.Rectangles(), want, state.pal.text) {
			t.Fatalf("missing link underline %+v in %+v", want, canvas.Rectangles())
		}
	}

	down := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit,
		X: x, Y: y, Viewport: state.viewport,
	})
	up := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
		X: x, Y: y, Viewport: state.viewport,
	})
	if !down.Handled || !up.Handled || len(opened) != 1 || opened[0] != "https://example.test/a" {
		t.Fatalf("down=%+v up=%+v opened=%q", down, up, opened)
	}
	if session.term.selection.selected || session.term.selection.dragging {
		t.Fatalf("click left selection %+v", session.term.selection)
	}

	exit := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerExit, Target: hit})
	if !exit.Repaint {
		t.Fatalf("exit = %+v", exit)
	}
	if _, ok := session.term.links.hoveredLink(session.term.screen); ok {
		t.Fatal("pointer exit kept the hovered link")
	}
}

func TestTerminalLinkHoverChecksTheNextLineAfterALinklessOne(t *testing.T) {
	state, session, geometry := selectionState()
	state.terminalLinkPlainClick = true
	session.term.write([]byte("no links here\r\nsee https://example.test/x now"))
	hit := hitTerminalList.At(int(session.ID))

	emptyX, emptyY := cellPoint(geometry, 0, 3, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, X: emptyX, Y: emptyY, Viewport: state.viewport})
	if _, ok := session.term.links.hoveredLink(session.term.screen); ok {
		t.Fatal("a linkless line reported a link")
	}

	linkX, linkY := cellPoint(geometry, 1, 8, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, X: linkX, Y: linkY, Viewport: state.viewport})
	link, ok := session.term.links.hoveredLink(session.term.screen)
	if !ok || link.Target != "https://example.test/x" {
		t.Fatalf("link on the next line = %+v, %v", link, ok)
	}
}

func TestTerminalLinkShiftSelectionAndRemoteMouseTakePriority(t *testing.T) {
	tests := []struct {
		name       string
		modifiers  gfx.Modifiers
		wantInput  bool
		wantSelect bool
	}{
		{name: "shift selects locally", modifiers: gfx.ModifierShift, wantSelect: true},
		{name: "remote mouse receives click", wantInput: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, session, geometry := selectionState()
			shell := &fakeShell{}
			session.term.shell = shell
			session.term.write([]byte("https://example.test/a"))
			session.term.screen.Write([]byte("\x1b[?1002h\x1b[?1006h"))
			opened := ""
			session.term.openLink = func(target string) { opened = target }
			hit := hitTerminalList.At(int(session.ID))
			startX, startY := cellPoint(geometry, 0, 2, state.cellWidth, state.cellHeight)
			endX, endY := cellPoint(geometry, 0, 8, state.cellWidth, state.cellHeight)

			state.Pointer(gfx.PointerEvent{
				Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, Modifiers: test.modifiers,
				X: startX, Y: startY, Viewport: state.viewport,
			})
			if test.wantSelect {
				state.Pointer(gfx.PointerEvent{
					Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
					X: endX, Y: endY, Viewport: state.viewport,
				})
			}
			state.Pointer(gfx.PointerEvent{
				Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
				X: endX, Y: endY, Viewport: state.viewport,
			})

			if opened != "" || session.term.selection.selected != test.wantSelect || (shell.typed() != "") != test.wantInput {
				t.Fatalf("opened=%q selected=%t input=%q", opened, session.term.selection.selected, shell.typed())
			}
		})
	}
}

func TestTerminalLinkSteadyPaintDoesNotAllocate(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("go https://example.test/a now"))
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 5, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, X: x, Y: y, Viewport: state.viewport})

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, geometry)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.paintGrid(&canvas, session, geometry)
	})
	if allocations != 0 {
		t.Fatalf("steady link paint allocations = %v", allocations)
	}
}

func hasTerminalLinkUnderline(rectangles []gfx.Rectangle, bounds gfx.Rect, color gfx.Color) bool {
	for _, rectangle := range rectangles {
		if rectangle.Bounds == bounds && rectangle.Color == color {
			return true
		}
	}
	return false
}

func TestTerminalLinkOpensOnlyWithTheModifierUnlessPlainClicksAreChosen(t *testing.T) {
	modifier := keymap.Primary
	tests := []struct {
		name      string
		plain     bool
		modifiers gfx.Modifiers
		wantOpen  bool
	}{
		{name: "a plain click only selects", modifiers: 0},
		{name: "the modifier opens", modifiers: modifier, wantOpen: true},
		{name: "shift keeps selecting", modifiers: modifier | gfx.ModifierShift},
		{name: "plain clicks chosen", plain: true, modifiers: 0, wantOpen: true},
		{name: "plain clicks chosen, modifier too", plain: true, modifiers: modifier, wantOpen: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, session, geometry := selectionState()
			state.terminalLinkPlainClick = test.plain
			session.term.write([]byte("go https://example.test/a now"))
			opened := ""
			session.term.openLink = func(target string) { opened = target }
			hit := hitTerminalList.At(int(session.ID))
			x, y := cellPoint(geometry, 0, 5, state.cellWidth, state.cellHeight)

			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, Modifiers: test.modifiers, X: x, Y: y, Viewport: state.viewport})
			_, hovered := session.term.links.hoveredLink(session.term.screen)
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, Modifiers: test.modifiers, X: x, Y: y, Viewport: state.viewport})
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Modifiers: test.modifiers, Captured: true, X: x, Y: y, Viewport: state.viewport})

			if (opened != "") != test.wantOpen || hovered != test.wantOpen {
				t.Fatalf("opened = %q, hover underline = %t; want open = %t", opened, hovered, test.wantOpen)
			}
		})
	}
}
