package workbench

import (
	"encoding/json"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestConnectionColorPersistsCopiesAndUpdatesOpenSessions(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	host := state.hosts[0]
	state.sessions = append(state.sessions, Session{ID: 99, HostID: host.ID, host: host})
	state.openEditHostID(host.ID)
	state.Click(hitHostColors.At(5))
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.hosts[0].Color != "#3b82f6" || state.sessions[0].host.Color != "#3b82f6" {
		t.Fatal("saved color did not reach host and open session")
	}
	restarted := newTestState(t, options)
	if restarted.hosts[0].Color != "#3b82f6" {
		t.Fatal("color lost on restart")
	}
	data, err := json.Marshal(savedHostFromHost(state.hosts[0]))
	if err != nil {
		t.Fatal(err)
	}
	var saved savedHost
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	imported, ok := restarted.hostFromCatalogEntry(saved)
	if !ok || imported.Color != "#3b82f6" {
		t.Fatal("catalog lost color")
	}
	restarted.editor.load(imported)
	if restarted.editor.host(123).Color != "#3b82f6" {
		t.Fatal("copy lost color")
	}
	state.openEditHostID(host.ID)
	state.Click(hitHostColors.At(1))
	state.Click(hitDialogCancel)
	if state.hosts[0].Color != "#3b82f6" {
		t.Fatal("cancel saved color")
	}
	state.openEditHostID(host.ID)
	state.editor.color = *text.NewEditor("#12ABEF")
	state.Click(hitDialogSubmit)
	if state.hosts[0].Color != "#12abef" {
		t.Fatal("custom color not normalized")
	}
	state.openEditHostID(host.ID)
	state.editor.color = *text.NewEditor("#xyz123")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrHostColorInvalid {
		t.Fatal("invalid custom color accepted")
	}
	state.Click(hitHostColors.At(0))
	state.Click(hitDialogSubmit)
	if state.hosts[0].Color != "" || state.sessions[0].host.Color != "" {
		t.Fatal("reset did not clear accent")
	}
}

func TestConnectionColorChoicesFitShortDialog(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	canvas, _ := paintAt(state, ui.Size{Width: 900, Height: 600})
	footer, _ := hitByID(canvas, hitDialogSubmit)
	for i := range hostColors {
		hit, ok := hitByID(canvas, hitHostColors.At(i))
		if !ok || hit.Bounds.Y+hit.Bounds.Height > footer.Bounds.Y || hit.Role != ui.RoleRadio {
			t.Fatal("color choice missing or outside dialog")
		}
	}
	var hit ui.Hit
	for _, h := range canvas.Hits() {
		if h.ID == hostPickerIDs.Trigger && !h.IsLabel {
			hit = h
			break
		}
	}
	if hit.ID.IsZero() || hit.Role != ui.RoleButton || hit.Bounds.Width != 32 || hit.Bounds.Y+hit.Bounds.Height > footer.Bounds.Y {
		t.Fatalf("custom color trigger missing or clipped: %+v", hit)
	}
	for _, hit := range canvas.Hits() {
		if hit.Label == state.messages.FieldColorCustom && hit.Role == ui.RoleTextField {
			t.Fatal("HEX input should only appear inside the picker")
		}
	}
	var name ui.Hit
	for _, h := range canvas.Hits() {
		if h.ID == hitDialogLabel && h.Role == ui.RoleTextField {
			name = h
			break
		}
	}
	if name.Bounds.Width < 290 {
		t.Fatal("color field is taking too much name space", name.Bounds)
	}
}

func TestConnectionColorUsesANeutralRailWithAnOrderedMarker(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := Session{ID: 1, host: Host{Color: "#3b82f6"}, term: &terminal{}}
	bounds := ui.Rect{X: 100, Y: 40, Width: 40, Height: 320}
	tools := []barTool{{
		id: hitTerminalConnectionColor, kind: barToolConnectionColor,
		hint: state.messages.Toolbars.ConnectionColor, height: terminalRailCapsuleHeight,
	}}
	var canvas ui.Canvas
	state.paintTerminalSideRail(&canvas, bounds, &session, tools, nil, state.pal.workspace, state.pal.line, terminalToolbarLeft, true)

	neutralRail, marker, halo := false, false, false
	for _, rectangle := range canvas.Rectangles() {
		switch {
		case rectangle.Bounds == bounds && rectangle.Color == state.pal.workspace:
			neutralRail = true
		case rectangle.Color == hostAccent(session.host.Color) &&
			rectangle.Bounds.Width == terminalRailAccentWidth &&
			rectangle.Bounds.Height == terminalRailAccentHeight &&
			rectangle.Bounds.Y >= bounds.Y && rectangle.Bounds.Y+rectangle.Bounds.Height <= bounds.Y+bounds.Height:
			marker = true
		case rectangle.Bounds.Width == terminalRailCapsuleWidth && rectangle.Bounds.Height == terminalRailCapsuleHeight &&
			rectangle.Bounds.Y >= bounds.Y && rectangle.Bounds.Y+rectangle.Bounds.Height <= bounds.Y+bounds.Height:
			halo = true
		}
	}
	if !neutralRail || !marker || !halo {
		t.Fatalf("tool rail neutral=%v marker=%v halo=%v rects=%+v",
			neutralRail, marker, halo, canvas.Rectangles())
	}
	if got, want := session.term.rails[terminalToolbarLeft].zones[terminalToolbarDockTop].contentExtent, state.terminalToolbarMetrics().inset*2+terminalRailCapsuleHeight; got != want {
		t.Fatalf("connection-color rail extent = %v, want compact capsule extent %v", got, want)
	}
}

func TestConnectionColorCapsuleCanBeHiddenWithoutChangingTheRail(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := Session{ID: 1, host: Host{Color: "#3b82f6"}, term: &terminal{}}
	bounds := ui.Rect{X: 100, Y: 40, Width: 40, Height: 320}
	state.terminalHideConnectionColor = true
	tools := []barTool{{id: hitTerminalConnectionColor, kind: barToolConnectionColor, hint: state.messages.Toolbars.ConnectionColor}}
	var canvas ui.Canvas
	state.paintTerminalSideRail(&canvas, bounds, &session, tools, nil, state.pal.workspace, state.pal.line, terminalToolbarRight, true)

	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color == hostAccent(session.host.Color) ||
			(rectangle.Bounds.Width == terminalRailCapsuleWidth && rectangle.Bounds.Height == terminalRailCapsuleHeight) {
			t.Fatalf("hidden connection-color marker was painted: %+v", rectangle)
		}
	}
}

func TestConnectionColorMarkerDoesNotReserveSpaceWithoutAHostColor(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	session.host.Color = ""
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Side = terminalToolbarRight.String()
		entries[index].Zone = terminalToolbarZoneRightTop.String()
		if entries[index].Key == "connection-color" {
			entries[index].Side = terminalToolbarLeft.String()
			entries[index].Zone = terminalToolbarZoneLeftTop.String()
		}
	}
	state.applyTerminalToolbarLayoutChange()

	if occupied, ok := state.terminalToolbarOccupiedSides(); !ok || occupied[terminalToolbarLeft] {
		t.Fatalf("a colorless connection reserved the left rail: occupied=%v ok=%v", occupied, ok)
	}

	session.host.Color = "#3b82f6"
	state.applyTerminalToolbarLayoutChange()
	if occupied, ok := state.terminalToolbarOccupiedSides(); !ok || !occupied[terminalToolbarLeft] {
		t.Fatalf("a colored connection did not occupy the left rail: occupied=%v ok=%v", occupied, ok)
	}
}

func TestConnectionColorEditRefitsLiveTerminalForARailChange(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Side = terminalToolbarRight.String()
		entries[index].Zone = terminalToolbarZoneRightTop.String()
		if entries[index].Key == "connection-color" {
			entries[index].Side = terminalToolbarLeft.String()
			entries[index].Zone = terminalToolbarZoneLeftTop.String()
		}
	}
	state.Resize(ui.Size{Width: 1200, Height: 800})
	state.applyTerminalToolbarLayoutChange()
	beforeCols, _ := session.term.screen.Size()

	previous := state.hosts[0]
	updated := previous
	updated.Color = "#3b82f6"
	state.replaceHost(0, previous, updated)

	afterCols, _ := session.term.screen.Size()
	if session.host.Color != updated.Color || afterCols >= beforeCols {
		t.Fatalf("live terminal was not refit for the new rail: color=%q cols %d -> %d",
			session.host.Color, beforeCols, afterCols)
	}
}

func TestConnectionColorTintsTabsButKeepsFileHeaderNeutral(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	session.host.Color = "#3b82f6"
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	for _, id := range []ui.ID{contextID(hitSFTPRefresh)} {
		hit, ok := hitByID(&canvas, id)
		if !ok {
			t.Fatal("missing header action", id)
		}
		found := false
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Color == state.pal.workspace && rectangle.Bounds.X <= hit.Bounds.X && rectangle.Bounds.X+rectangle.Bounds.Width >= hit.Bounds.X+hit.Bounds.Width && rectangle.Bounds.Y <= hit.Bounds.Y && rectangle.Bounds.Y+rectangle.Bounds.Height >= hit.Bounds.Y+hit.Bounds.Height {
				found = true
			}
		}
		if !found {
			t.Fatal("file header must stay neutral", id)
		}
	}
	foundTab := false
	for _, tab := range state.chrome.bar.tabs {
		if tab.workspace.ID == state.deck.ActiveWorkspace() {
			foundTab = true
			if tab.background == state.pal.tabActive {
				t.Fatal("active workspace tab lost its connection color")
			}
		}
	}
	if !foundTab {
		t.Fatal("active workspace tab is missing")
	}
	session.host.Color = ""
	canvas.Reset()
	state.Paint(&canvas, size)
	for _, tab := range state.chrome.bar.tabs {
		if tab.workspace.ID == state.deck.ActiveWorkspace() && tab.background != state.pal.tabActive {
			t.Fatal("clearing connection color kept a stale workspace tint")
		}
	}
	host := state.hosts[0]
	host.Color = "#3b82f6"
	for _, grid := range []bool{false, true} {
		canvas.Reset()
		bounds := ui.Rect{Width: 700, Height: hostRowHeight}
		if grid {
			state.paintHostCard(&canvas, bounds, host)
		} else {
			state.paintHostTreeRow(&canvas, bounds, bounds, host, hitHostList.At(int(host.ID)))
		}
		found := false
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Color == hostAccent(host.Color) && rectangle.Bounds.Width == 3 {
				found = true
			}
		}
		if !found {
			t.Fatal("workbench connection color marker missing", grid)
		}
	}
}

func TestConnectionColorKeepsEmptyFilePanelNeutral(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for _, session := range []*Session{nil, {host: Host{Color: "#a855f7"}}, {}} {
		var canvas ui.Canvas
		state.paintEmptyContextSFTP(&canvas, ui.Size{Width: 340, Height: 800}, session)
		button, ok := hitByID(&canvas, hitSFTPClose)
		if !ok {
			t.Fatal("empty panel has no close action")
		}
		found := false
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Color == state.pal.workspace && rectangle.Bounds.Height == infoBarHeight && rectangle.Bounds.X <= button.Bounds.X && rectangle.Bounds.X+rectangle.Bounds.Width >= button.Bounds.X+button.Bounds.Width {
				found = true
			}
		}
		if !found {
			t.Fatal("empty file panel header must stay neutral")
		}
	}
}

func TestConnectionColorKeepsMonitorHeaderNeutral(t *testing.T) {
	state, session := monitorFixture(t)
	bounds := ui.Rect{X: 600, Y: 40, Width: 320, Height: 800}
	var canvas ui.Canvas
	state.paintMonitor(&canvas, bounds, session)
	header := ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: infoBarHeight}
	found := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds == header && rectangle.Color == state.pal.workspace {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("monitor header must stay neutral")
	}
}

// A colored host must keep both the list's selection tone and the card's icon
// badge: the tint goes on top of the selection fill, and the badge keeps picking
// its fill from the card background it was painted on.
func TestColoredHostsKeepSelectionAndBadgeContrast(t *testing.T) {
	for _, light := range []bool{false, true} {
		name := "dark"
		if light {
			name = "light"
		}
		t.Run(name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			if light {
				state.setAppearance(appearanceLight)
			}
			host := state.hosts[0]
			host.Icon = hostIconTerminal
			host.Color = "#3b82f6"
			state.selectedHostID = host.ID
			if tint := tintHostSurface(state.pal.workspace, host.Color, .10); tint == state.pal.workspace {
				t.Fatal("connection color does not reach the surface")
			}

			var canvas ui.Canvas
			row := ui.Rect{Width: 900, Height: hostRowHeight}
			state.paintHostTreeRow(&canvas, row, row, host, hitHostList.At(int(host.ID)))
			want, hover := tintHostSurface(state.pal.surfaceHot, host.Color, .10), tintHostSurface(state.pal.surfaceSoft, host.Color, .10)
			selected := false
			for _, rect := range canvas.Rectangles() {
				if rect.Bounds != row {
					continue
				}
				if rect.Color == hover {
					t.Fatal("a selected colored row is painted with the hover fill")
				}
				if rect.Color == want {
					selected = true
				}
			}
			if !selected {
				t.Fatal("a selected colored row lost the selection tone")
			}

			canvas.Reset()
			state.paintHostCard(&canvas, ui.Rect{Width: 360, Height: 150}, host)
			badge := false
			for _, rect := range canvas.Rectangles() {
				if rect.Bounds.Width == 36 && rect.Bounds.Height == 36 && rect.Color == state.pal.surface {
					badge = true
				}
			}
			if !badge {
				t.Fatal("a selected colored card paints its icon badge in the card's own color")
			}
		})
	}
}
