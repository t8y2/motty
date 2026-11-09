package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/configstore"
	"motty/internal/i18n"
	layoutcore "motty/internal/workspace"
)

// This exercises the real pointer/click dispatcher and a local PTY. No SSH
// server, saved hosts, providers or synthetic terminal output are installed.
func toolbarDockView(t *testing.T) (*State, *headless.Session, Options) {
	return toolbarDockViewAtScale(t, 1.5)
}

func toolbarDockViewAtScale(t *testing.T, scale float64) (*State, *headless.Session, Options) {
	t.Helper()
	skipWithoutUnixShell(t)
	dir := t.TempDir()
	options := Options{StatePath: filepath.Join(dir, "state.json"), KnownHostsPath: filepath.Join(dir, "known_hosts")}
	state := newTestState(t, options)
	state.setLanguage(i18n.Chinese)
	state.Click(hitLocalTerminal)
	state.Wake()
	if session, ok := state.sessionByIDPointer(state.activeSessionID); !ok || session.term == nil || session.term.shell == nil {
		t.Fatal("local PTY did not start")
	}
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(app.Config{Width: 1200, Height: 800, WindowMode: app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return state, view, options
}

func TestToolbarDockSizesKeepMixedActionsReachableAcrossScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, view, options := toolbarDockViewAtScale(t, scale)
			state.moveToolbarDockTool(hitSFTPOpen, toolbarDockLeftTop, gfx.ID{})
			state.moveToolbarDockTool(hitTheme, toolbarDockLeftBottom, gfx.ID{})
			state.moveToolbarDockTool(hitConnExport, toolbarDockBottom, gfx.ID{})
			state.moveToolbarDockTool(hitWindowSettings, toolbarDockBottomRight, gfx.ID{})
			wantDock := state.toolbars.snapshot()
			for _, width := range []int{760, 1200} {
				previousButtonSize := float32(0)
				for size := terminalToolbarSmall; size <= terminalToolbarLarge; size++ {
					state.openToolbarSettings(toolbarTerminal)
					if err := view.Resize(width, 500); err != nil {
						t.Fatal(err)
					}
					click := func(id gfx.ID) {
						t.Helper()
						box := dualHit(t, view, id)
						if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
							t.Fatal(err)
						}
					}
					ids := [...]gfx.ID{hitToolbarSizeSmall, hitToolbarSizeMedium, hitToolbarSizeLarge}
					click(ids[size])
					if state.terminalToolbarSize != size {
						t.Fatalf("size selector did not apply %s", size.String())
					}
					click(hitToolbarSettingsClose)
					metrics := state.terminalToolbarMetrics()
					workspace := state.baseWorkspacePixelBounds(state.viewport)
					full := state.fullWorkspacePixelBounds(state.viewport)
					if workspace.Width != full.Width-2*metrics.railExtent || workspace.Height != full.Height-metrics.railExtent {
						t.Fatalf("size %s did not reserve both sides and bottom: %+v", size.String(), workspace)
					}
					for _, id := range []gfx.ID{hitSFTPOpen, hitTheme, hitConnExport, hitWindowSettings} {
						box := dualHit(t, view, id)
						if box.Width != metrics.toolSize || box.Height != metrics.toolSize {
							t.Fatalf("size %s control %v clipped or incorrectly sized: %+v", size.String(), id, box)
						}
						if overlap := intersectRect(box, workspace); overlap.Width > 0 && overlap.Height > 0 {
							t.Fatalf("size %s control %v overlaps terminal: %+v", size.String(), id, box)
						}
					}
					button := dualHit(t, view, hitSFTPOpen)
					if button.Width <= previousButtonSize {
						t.Fatal("larger toolbar setting did not enlarge its hit target")
					}
					previousButtonSize = button.Width
					for index := range state.sessions {
						session := &state.sessions[index]
						geometry := state.layoutForSession(session.ID)
						if cols, rows := session.term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
							t.Fatalf("size %s PTY grid does not match reserved geometry", size.String())
						}
					}
					if !reflect.DeepEqual(state.toolbars.snapshot(), wantDock) {
						t.Fatal("resizing rewrote mixed-context placement or ordering")
					}
					restarted := newTestState(t, options)
					if restarted.terminalToolbarSize != size || !reflect.DeepEqual(restarted.toolbars.snapshot(), wantDock) {
						t.Fatal("size or mixed-context layout did not survive restart")
					}
				}
			}
			state.openToolbarSettings(toolbarTerminal)
			if err := view.Resize(760, 500); err != nil {
				t.Fatal(err)
			}
			result := state.Key(gfx.KeyEvent{Target: hitToolbarSizeLarge, Key: gfx.KeyLeft})
			if !result.Handled || result.Focus != hitToolbarSizeMedium || state.terminalToolbarSize != terminalToolbarMedium {
				t.Fatalf("size keyboard navigation failed: %+v", result)
			}
			if err := view.Resize(760, 500); err != nil {
				t.Fatal(err)
			}
			box := dualHit(t, view, hitToolbarReset)
			if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
				t.Fatal(err)
			}
			if state.terminalToolbarSize != terminalToolbarSmall ||
				state.toolbarDockZoneFor(toolbarWindow, "theme") != toolbarDockLeftBottom ||
				state.toolbarDockZoneFor(toolbarWindow, "settings") != toolbarDockBottomRight {
				t.Fatal("terminal reset did not reset size or disturbed window placements")
			}
			state.closeModal()
			state.setTerminalToolbarSize(terminalToolbarLarge)
			state.toolbarDock.menu = true
			state.Click(hitToolbarDockReset)
			if state.terminalToolbarSize != terminalToolbarSmall {
				t.Fatal("dock menu reset retained a custom size")
			}
		})
	}
}

func TestToolbarDockHidesTerminalSideRailWithoutVisibleContent(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.activeSessionID = session.ID
	state.activeSection = sectionTerminal
	state.terminalShowLatency = false
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Enabled = false
	}

	size := gfx.Size{Width: 1200, Height: 800}
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(position.String(), func(t *testing.T) {
			state.terminalToolbarPosition = position
			state.Resize(size)
			left := position == terminalToolbarLeft
			if reserve := state.toolbarDockSideReserveFor(left, true); reserve != 0 {
				t.Fatalf("empty %s rail reserved %.1f pixels", position, reserve)
			}
			full := state.fullWorkspacePixelBounds(size)
			workspace := state.baseWorkspacePixelBounds(size)
			if workspace != full {
				t.Fatalf("empty %s rail changed workspace: full=%+v workspace=%+v", position, full, workspace)
			}
		})
	}

	state.terminalShowLatency = true
	atomic.StoreInt32(&session.rttAvailable, 0)
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		state.terminalToolbarPosition = position
		state.Resize(size)
		for _, left := range []bool{false, true} {
			if reserve := state.toolbarDockSideReserveFor(left, true); reserve != 0 {
				t.Fatalf("latency enabled without RTT kept %s side left=%t open at %.1f", position, left, reserve)
			}
		}
	}
	atomic.StoreInt64(&session.rttMs, 42)
	atomic.StoreInt32(&session.rttAvailable, 1)
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		state.terminalToolbarPosition = position
		state.Resize(size)
		if _, _, ok := state.latencyInfo(session); !ok {
			t.Fatal("latency test requires a connected remote session with RTT")
		}
		primaryLeft := position == terminalToolbarLeft
		for _, left := range []bool{false, true} {
			want := float32(0)
			if left == primaryLeft {
				want = state.terminalToolbarMetrics().railExtent
			}
			if reserve := state.toolbarDockSideReserveFor(left, true); reserve != want {
				t.Fatalf("latency-only %s side left=%t reserve = %.1f, want %.1f", position, left, reserve, want)
			}
		}
	}
	state.terminalShowLatency = false
	for index := range entries {
		if entries[index].Key == "files" {
			entries[index].Enabled = true
		}
	}
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		state.terminalToolbarPosition = position
		state.Resize(size)
		left := position == terminalToolbarLeft
		if reserve := state.toolbarDockSideReserveFor(left, true); reserve != state.terminalToolbarMetrics().railExtent {
			t.Fatalf("visible %s Files action did not reserve its rail: %.1f", position, reserve)
		}
	}

	state.moveToolbarDockTool(hitSFTPOpen, toolbarDockTop, gfx.ID{})
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		state.terminalToolbarPosition = position
		state.Resize(size)
		for _, left := range []bool{false, true} {
			if reserve := state.toolbarDockSideReserveFor(left, true); reserve != 0 {
				t.Fatalf("moved-out %s side left=%t reserve = %.1f", position, left, reserve)
			}
		}
	}
}

func TestToolbarDockPresenceCheckDoesNotRecalculateManagedSplitBounds(t *testing.T) {
	state, _, _, _ := liveSession(t)
	workspaceID := state.deck.ActiveWorkspace()
	state.managedWorkspaceLayouts = map[layoutcore.WorkspaceID]layoutcore.LayoutSpec{workspaceID: layoutcore.GridSpec(1)}
	state.activeSection = sectionTerminal
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Enabled = false
	}
	state.Resize(gfx.Size{Width: 1200, Height: 800})
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		state.terminalToolbarPosition = position
		if reserve := state.toolbarDockSideReserveFor(position == terminalToolbarLeft, true); reserve != 0 {
			t.Fatalf("managed %s rail reserved %.1f pixels", position, reserve)
		}
	}
}

func TestToolbarDockPresenceCheckDoesNotAllocate(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.activeSection = sectionTerminal
	state.Resize(gfx.Size{Width: 1200, Height: 800})
	for _, latency := range []bool{false, true} {
		t.Run(fmt.Sprintf("latency=%t", latency), func(t *testing.T) {
			state.terminalShowLatency = latency
			atomic.StoreInt64(&session.rttMs, 42)
			if latency {
				atomic.StoreInt32(&session.rttAvailable, 1)
			} else {
				atomic.StoreInt32(&session.rttAvailable, 0)
			}
			allocations := testing.AllocsPerRun(100, func() {
				state.terminalToolbarShowsOnSide(session, true)
				state.terminalToolbarShowsOnSide(session, false)
			})
			if allocations != 0 {
				t.Fatalf("toolbar presence check allocated %.1f times", allocations)
			}
		})
	}
}

func TestToolbarDockManagedCompactionUsesActualVisibleSide(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.activeSection = sectionTerminal
	state.terminalToolbarPosition = terminalToolbarLeft
	workspaceID := state.deck.ActiveWorkspace()
	state.managedWorkspaceLayouts = map[layoutcore.WorkspaceID]layoutcore.LayoutSpec{workspaceID: layoutcore.GridSpec(1)}
	size := gfx.Size{Width: 1200, Height: 800}
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Enabled = entries[index].Key == "export-log"
	}
	state.moveToolbarDockTool(hitConnExport, toolbarDockSideTop, gfx.ID{})
	state.Resize(size)
	if reserve := state.toolbarDockSideReserveFor(true, true); reserve != state.terminalToolbarMetrics().railExtent {
		t.Fatalf("managed menu did not reserve the terminal default side: %.1f", reserve)
	}
	if reserve := state.toolbarDockSideReserveFor(false, true); reserve != 0 {
		t.Fatalf("managed menu reserved its obsolete configured side: %.1f", reserve)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if hit, ok := hitByID(&canvas, hitWorkspacePaneTools); !ok || hit.Bounds.X+hit.Bounds.Width > size.Width/2 {
		t.Fatalf("managed menu was not painted on the terminal default side: %+v, %t", hit.Bounds, ok)
	}

	for index := range entries {
		entries[index].Enabled = entries[index].Key == "files"
	}
	state.moveToolbarDockTool(hitSFTPOpen, toolbarDockSideTop, gfx.ID{})
	state.Resize(size)
	if reserve := state.toolbarDockSideReserveFor(true, true); reserve != 0 {
		t.Fatalf("compactable action reserved its old side: %.1f", reserve)
	}
	if reserve := state.toolbarDockSideReserveFor(false, true); reserve != state.terminalToolbarMetrics().railExtent {
		t.Fatalf("compactable action did not reserve its configured side: %.1f", reserve)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if hit, ok := hitByID(&canvas, hitSFTPOpen); !ok || hit.Bounds.X < size.Width/2 {
		t.Fatalf("compactable action was not painted on its configured side: %+v, %t", hit.Bounds, ok)
	}
}

func TestToolbarDockClearsRailInputAfterLatencyOnlySideDisappears(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.activeSection = sectionTerminal
	state.terminalToolbarPosition = terminalToolbarRight
	state.terminalShowLatency = true
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Enabled = false
	}
	atomic.StoreInt64(&session.rttMs, 42)
	atomic.StoreInt32(&session.rttAvailable, 1)
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	rail := session.term.railBounds
	if rail.Width <= 0 || rail.Height <= 0 {
		t.Fatalf("latency-only rail did not reserve its painted bounds: %+v", rail)
	}

	atomic.StoreInt32(&session.rttAvailable, 0)
	state.Paint(&canvas, size)
	if reserve := state.toolbarDockSideReserveFor(false, true); reserve != 0 {
		t.Fatalf("latency-only rail retained %v pixels", reserve)
	}
	if session.term.railBounds != (gfx.Rect{}) || session.term.railViewport != (gfx.Rect{}) ||
		session.term.railContentExtent != 0 || session.term.railCount != 0 {
		t.Fatalf("hidden rail retained input state: bounds=%+v viewport=%+v extent=%v count=%d",
			session.term.railBounds, session.term.railViewport, session.term.railContentExtent, session.term.railCount)
	}
	event := gfx.ScrollEvent{X: rail.X + rail.Width/2, Y: rail.Y + rail.Height/2}
	if _, handled := state.scrollTerminalToolbar(session, event); handled {
		t.Fatal("hidden rail consumed wheel input over the expanded terminal")
	}
}

func TestToolbarDockIgnoresUnavailableToolsOnSide(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.activeSection = sectionTerminal
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Enabled = entries[index].Key == "broadcast"
	}
	state.moveToolbarDockTool(hitBroadcast, toolbarDockLeftTop, gfx.ID{})
	state.terminalToolbarPosition = terminalToolbarRight
	state.Resize(gfx.Size{Width: 1200, Height: 800})

	for _, left := range []bool{false, true} {
		if reserve := state.toolbarDockSideReserveFor(left, true); reserve != 0 {
			t.Fatalf("unavailable broadcast action kept side left=%t open at %.1f", left, reserve)
		}
	}
}

func TestToolbarDockFormat21UpgradePreservesPlacementAndGuardsSize(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	options.Keystore = &memKeys{}
	saved := savedState{
		Version: stateFileVersion, HostCatalogVersion: hostCatalogSchemaVersion, ShortcutVersion: shortcutSchemaVersion,
		TerminalToolbarPosition: "left",
		Toolbars: &savedToolbars{Dock: []toolbarDockEntry{
			{toolbarTerminal, "files", toolbarDockLeftBottom},
			{toolbarWindow, "settings", toolbarDockBottomRight},
			{toolbarWindow, "theme", toolbarDockLeftBottom},
		}},
	}
	entries, local, _, err := splitState(saved, newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(options.StatePath, 21)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{Put: entries, Local: map[string][]byte{localKey: local}}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if state.terminalToolbarSize != terminalToolbarSmall {
		t.Fatal("format 21 did not retain historical size")
	}
	for _, entry := range saved.Toolbars.Dock {
		if zone := state.toolbarDockZoneFor(entry.Group, entry.Key); zone != entry.Zone {
			t.Fatalf("format 21 placement lost for %s: %s", entry.Key, zone)
		}
	}
	want := state.toolbars.snapshot()
	state.setTerminalToolbarSize(terminalToolbarLarge)
	restarted := newTestState(t, options)
	if restarted.terminalToolbarSize != terminalToolbarLarge || !reflect.DeepEqual(restarted.toolbars.snapshot(), want) {
		t.Fatal("saving size discarded format-21 dock entries or their ordering")
	}
	if _, _, err := configstore.ReadOnly(options.StatePath, 21); !errors.Is(err, configstore.ErrTooNew) {
		t.Fatalf("format-21 writer can discard size: %v", err)
	}
}

func toolbarDockShot(t *testing.T, view *headless.Session, name string) {
	t.Helper()
	if dir := os.Getenv("MOTTY_TOOLBAR_DOCK_SCREENSHOT_DIR"); dir != "" {
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
			t.Fatal(err)
		}
	}
}

func TestToolbarDockRealDragReorderAndRestart(t *testing.T) {
	state, view, options := toolbarDockView(t)
	toolbarDockShot(t, view, "01-default")
	drag := func(id gfx.ID, to gfx.Point) {
		t.Helper()
		box := dualHit(t, view, id)
		if err := view.Press(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Move(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
		if !state.toolbarDock.drag.active {
			t.Fatal("drag did not start")
		}
		if err := view.Release(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
	}
	before := state.appearance
	box := dualHit(t, view, hitTheme)
	if err := view.Press(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(1180, 700); err != nil {
		t.Fatal(err)
	}
	if state.toolbars.dock != nil {
		t.Fatal("drag modified saved layout before release")
	}
	toolbarDockShot(t, view, "02-drag-preview")
	if err := view.Release(1180, 700); err != nil {
		t.Fatal(err)
	}
	if state.appearance != before {
		t.Fatal("dropping theme also activated it")
	}
	if got := state.toolbarDockZoneFor(toolbarWindow, "theme"); got != toolbarDockSideBottom {
		t.Fatalf("theme zone = %q", got)
	}
	box = dualHit(t, view, hitTheme)
	if box.Y < 600 {
		t.Fatalf("lower group was not bottom aligned: %+v", box)
	}

	drag(hitAIToggle, gfx.Point{X: 1180, Y: 100})
	if state.aiPanelVisible() {
		t.Fatal("dragging AI also opened its panel")
	}
	if err := view.ClickLabel(state.messages.AIPanel.ToggleTip); err != nil {
		t.Fatal(err)
	}
	if !state.aiPanelVisible() {
		t.Fatal("relocated AI button did not open the panel")
	}
	toolbarDockShot(t, view, "03-side-actions")
	if err := view.ClickLabel(state.messages.AIPanel.ToggleTip); err != nil {
		t.Fatal(err)
	}
	// The trailing window controls occupy this corner on Windows/Linux. Drop
	// inside the toolbar actually painted by the current platform.
	top := state.toolbarDock.zones[0]
	drag(hitConnExport, gfx.Point{X: top.X + top.Width - 2, Y: top.Y + top.Height/2})
	if box := dualHit(t, view, hitConnExport); box.Y >= windowBarHeight {
		t.Fatal("terminal action did not move to top")
	}
	settings := dualHit(t, view, hitWindowSettings)
	drag(hitConnExport, gfx.Point{X: settings.X - 1, Y: 18})
	if box := dualHit(t, view, hitConnExport); box.X >= dualHit(t, view, hitWindowSettings).X {
		t.Fatal("top reorder failed")
	}

	want := state.toolbars.snapshot()
	reopened := newTestState(t, options)
	if !reflect.DeepEqual(reopened.toolbars.snapshot(), want) {
		t.Fatal("layout did not survive opening a new state")
	}
	// Global actions on the side stay available without an active terminal page.
	state.Click(hitHome)
	if err := view.Resize(int(state.viewport.Width), int(state.viewport.Height)); err != nil {
		t.Fatal(err)
	}
	if box := dualHit(t, view, hitTheme); box.X < 1100 || box.Y <= windowBarHeight {
		t.Fatal("section page lost the relocated global action")
	}
	toolbarDockShot(t, view, "04-section-actions")
}

func TestToolbarDockCancelMenuAndOverflow(t *testing.T) {
	state, view, _ := toolbarDockView(t)
	box := dualHit(t, view, hitTheme)
	x, y := box.X+box.Width/2, box.Y+box.Height/2
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(1180, 700); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(1180, 700); err != nil {
		t.Fatal(err)
	}
	if state.toolbars.dock != nil {
		t.Fatal("Escape committed a layout")
	}
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(500, 400); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(500, 400); err != nil {
		t.Fatal(err)
	}
	if state.toolbars.dock != nil {
		t.Fatal("invalid drop committed a layout")
	}
	if err := view.RightClick(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.toolbarDock.menu {
		t.Fatal("right click did not open the layout menu")
	}
	toolbarDockShot(t, view, "05-context-menu")
	if err := view.ClickLabel(state.messages.Toolbars.MoveSideBottom); err != nil {
		t.Fatal(err)
	}
	box = dualHit(t, view, hitTheme)
	if err := view.RightClick(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.messages.Toolbars.Hide); err != nil {
		t.Fatal(err)
	}
	if state.toolbarVisible(toolbarWindow, hitTheme) {
		t.Fatal("hide did not disable the tool")
	}
	state.openToolbarSettings(toolbarWindow)
	if err := view.Resize(int(state.viewport.Width), int(state.viewport.Height)); err != nil {
		t.Fatal(err)
	}
	toolbarDockShot(t, view, "06-settings")
	index := toolbarEntryIndex(toolbarWindow, "theme")
	box = dualHit(t, view, hitToolbarToggles.At(index))
	if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.toolbarVisible(toolbarWindow, hitTheme) {
		t.Fatal("settings did not restore hidden tool")
	}
	if err := view.ClickLabel(state.messages.Toolbars.Close); err != nil {
		t.Fatal(err)
	}

	for _, spec := range toolbarCatalog[toolbarTerminal] {
		state.moveToolbarDockTool(spec.id, toolbarDockTop, gfx.ID{})
	}
	if err := view.Resize(640, 420); err != nil {
		t.Fatal(err)
	}
	box = dualHit(t, view, hitToolbarDockMore)
	if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
		t.Fatal(err)
	}
	toolbarDockShot(t, view, "07-overflow")
	if _, ok := view.Hit(state.messages.Toolbars.ResetLayout); !ok {
		t.Fatal("overflow menu lost layout controls")
	}
	if err := view.ClickLabel(state.messages.Toolbars.ResetLayout); err != nil {
		t.Fatal(err)
	}
	if state.toolbars.dock != nil {
		t.Fatal("restore defaults kept custom positions")
	}
}

func TestToolbarDockMovesLatencyReadoutAndPersists(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.terminalShowLatency = true
	state.terminalToolbarPosition = terminalToolbarRight
	state.activeSection = sectionTerminal
	atomic.StoreInt64(&session.rttMs, 42)
	atomic.StoreInt32(&session.rttAvailable, 1)
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	hit, ok := hitByID(&canvas, hitToolbarLatency)
	if !ok {
		t.Fatal("the latency readout was not painted")
	}
	if hit.Bounds.X < size.Width/2 {
		t.Fatalf("the default readout was not on the configured right rail: %+v", hit.Bounds)
	}
	found := false
	for _, text := range canvas.Texts() {
		// The titlebar slot may elide the "ms" suffix under a wider fallback
		// font; the reading itself must still be shown.
		if strings.Contains(text.Value, "42") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the readout did not show its compact sample")
	}

	// It is a dock hit now, so the ordinary toolbar drag moves it.
	start := gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitToolbarLatency, X: start.X, Y: start.Y, Viewport: size})
	bottom := state.toolbarDockBottomBounds(size, true)
	to := gfx.Point{X: bottom.X + bottom.Width/4, Y: bottom.Y + bottom.Height/2}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: to.X, Y: to.Y, Viewport: size})
	if !state.toolbarDock.drag.active {
		t.Fatal("pressing the readout did not start a toolbar drag")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: to.X, Y: to.Y, Viewport: size})
	if zone := state.toolbarDockZoneFor(toolbarTerminal, "latency"); zone != toolbarDockBottom {
		t.Fatalf("dropped readout zone = %s", zone)
	}

	canvas.Reset()
	state.Paint(&canvas, size)
	hit, ok = hitByID(&canvas, hitToolbarLatency)
	if !ok {
		t.Fatal("the moved readout was not painted")
	}
	if hit.Bounds.Y < size.Height-state.terminalToolbarMetrics().railExtent {
		t.Fatalf("the readout did not move to the bottom strip: %+v", hit.Bounds)
	}
	if reserve := state.toolbarDockBottomReserve(true); reserve != state.terminalToolbarMetrics().railExtent {
		t.Fatalf("the docked readout did not reserve its strip: %.1f", reserve)
	}
	geometry := state.layoutForSession(session.ID)
	if geometry.area.Y+geometry.area.Height > hit.Bounds.Y {
		t.Fatalf("terminal %+v overlaps the docked readout %+v", geometry.area, hit.Bounds)
	}

	state.save()
	restarted := newTestState(t, state.options)
	if !restarted.terminalShowLatency {
		t.Fatal("the Show latency preference did not survive restart")
	}
	if zone := restarted.toolbarDockZoneFor(toolbarTerminal, "latency"); zone != toolbarDockBottom {
		t.Fatalf("restart lost the readout zone: %s", zone)
	}
}

func TestToolbarDockLatencyReadoutReachesEveryZone(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.terminalShowLatency = true
	state.activeSection = sectionTerminal
	atomic.StoreInt64(&session.rttMs, 42)
	atomic.StoreInt32(&session.rttAvailable, 1)
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	for _, test := range []struct {
		zone  toolbarDockZone
		check func(gfx.Rect) bool
	}{
		{toolbarDockTop, func(b gfx.Rect) bool { return b.Y < windowBarHeight }},
		{toolbarDockLeftTop, func(b gfx.Rect) bool { return b.X < size.Width/2 }},
		{toolbarDockBottomRight, func(b gfx.Rect) bool {
			return b.Y >= size.Height-state.terminalToolbarMetrics().railExtent && b.X >= size.Width/2
		}},
	} {
		state.moveToolbarDockTool(hitToolbarLatency, test.zone, gfx.ID{})
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		hit, ok := hitByID(&canvas, hitToolbarLatency)
		if !ok {
			t.Fatalf("zone %s did not paint the readout", test.zone)
		}
		if !test.check(hit.Bounds) {
			t.Fatalf("zone %s painted the readout at %+v", test.zone, hit.Bounds)
		}
		found := false
		var values []string
		for _, text := range canvas.Texts() {
			values = append(values, text.Value)
			// The slot may elide "ms" under a wider fallback font.
			if strings.Contains(text.Value, "42") {
				found = true
			}
		}
		if !found {
			t.Fatalf("zone %s lost the sample text: %q", test.zone, values)
		}
	}
}

func TestToolbarLatencySwitchSharesTheSetting(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.terminalShowLatency {
		t.Fatal("latency should be off by default")
	}
	index := toolbarEntryIndex(toolbarTerminal, "latency")
	state.openToolbarSettings(toolbarTerminal)
	state.clickToolbarSettings(hitToolbarToggles.At(index))
	if !state.terminalShowLatency || !state.toolbarVisible(toolbarTerminal, hitToolbarLatency) {
		t.Fatal("the toolbar switch did not turn latency on")
	}
	state.clickToolbarSettings(hitToolbarToggles.At(index))
	if state.terminalShowLatency || state.toolbarVisible(toolbarTerminal, hitToolbarLatency) {
		t.Fatal("the toolbar switch did not turn latency off")
	}
	state.clickToolbarSettings(hitToolbarDisableAll)
	if state.terminalShowLatency {
		t.Fatal("disable all left the shared switch on")
	}
	state.clickToolbarSettings(hitToolbarEnableAll)
	if !state.terminalShowLatency {
		t.Fatal("enable all did not turn the shared switch on")
	}
	state.closeModal()

	// The Settings > Terminal row writes the same preference.
	state.Click(hitSettingsLatencyToggle)
	if state.terminalShowLatency || state.toolbarVisible(toolbarTerminal, hitToolbarLatency) {
		t.Fatal("the settings row did not turn the shared switch off")
	}
	if entries := state.toolbars.layouts[toolbarTerminal]; entries != nil {
		for _, entry := range entries {
			if entry.Key == "latency" && entry.Enabled {
				t.Fatal("the layout kept a stale latency switch")
			}
		}
	}
	state.Click(hitSettingsLatencyToggle)
	if !state.terminalShowLatency || !state.toolbarVisible(toolbarTerminal, hitToolbarLatency) {
		t.Fatal("the settings row did not turn the shared switch back on")
	}
}

func TestToolbarDockMovedActionFollowsTheActiveLocalSplit(t *testing.T) {
	state, view, _ := toolbarDockView(t)
	first := state.activeSessionID
	state.Click(hitLocalTerminal)
	state.Wake()
	second := state.activeSessionID
	// Move the new terminal tab beside the first through the same docking path
	// used by tab dragging, then act on each pane with the relocated control.
	firstWorkspace, firstPane, _ := state.deck.PaneForSession(layoutcore.SessionID(first))
	secondWorkspace, _, _ := state.deck.PaneForSession(layoutcore.SessionID(second))
	state.activateWorkspace(firstWorkspace)
	if !state.dockWorkspace(secondWorkspace, firstPane, dockRight) {
		t.Fatal("could not dock real local terminals")
	}
	state.moveToolbarDockTool(hitTerminalRecord, toolbarDockTop, gfx.ID{})
	for _, id := range []uint32{first, second} {
		state.ActivatePane(id)
		if err := view.Resize(int(state.viewport.Width), int(state.viewport.Height)); err != nil {
			t.Fatal(err)
		}
		box := dualHit(t, view, hitTerminalRecord)
		if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
		session, _ := state.sessionByIDPointer(id)
		if !session.term.recording.active {
			t.Fatalf("relocated action missed active session %d", id)
		}
	}
}

func TestToolbarDockVerticalTabsSideAndBottom(t *testing.T) {
	state, view, options := toolbarDockView(t)
	first := state.activeSessionID
	state.Click(hitLocalTerminal)
	state.Wake()
	firstWorkspace, firstPane, _ := state.deck.PaneForSession(layoutcore.SessionID(first))
	secondWorkspace := state.deck.ActiveWorkspace()
	state.activateWorkspace(firstWorkspace)
	if !state.dockWorkspace(secondWorkspace, firstPane, dockRight) {
		t.Fatal("could not dock local terminals")
	}
	state.setTabOrientation(tabsVertical)
	if err := view.Resize(1200, 800); err != nil {
		t.Fatal(err)
	}
	drag := func(id gfx.ID, to gfx.Point) {
		t.Helper()
		box := dualHit(t, view, id)
		if err := view.Press(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Move(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
		if !state.toolbarDock.drag.active {
			t.Fatal("vertical mode did not start dragging")
		}
		if err := view.Release(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
	}
	drag(hitAIToggle, gfx.Point{X: 1180, Y: 200})
	drag(hitTheme, gfx.Point{X: 1180, Y: 650})
	drag(hitConnExport, gfx.Point{X: 500, Y: 788})
	drag(hitWindowSettings, gfx.Point{X: 650, Y: 788})
	if zone := state.toolbarDockZoneFor(toolbarTerminal, "export-log"); zone != toolbarDockBottom {
		t.Fatalf("bottom drop = %s", zone)
	}
	if box := dualHit(t, view, hitConnExport); box.Y < 760 {
		t.Fatalf("bottom action geometry = %+v", box)
	}
	for _, pane := range state.panes {
		if pane.SessionID == 0 {
			continue
		}
		geometry := state.layoutForSession(pane.SessionID)
		if geometry.area.Y+geometry.area.Height > 760 || geometry.area.X < state.verticalTabsReserve(state.viewport) {
			t.Fatalf("terminal overlaps toolbars: %+v", geometry.area)
		}
	}
	if err := view.ClickLabel(state.messages.AIPanel.ToggleTip); err != nil {
		t.Fatal(err)
	}
	if state.ai.panel.rect.Y+state.ai.panel.rect.Height > 760 {
		t.Fatal("AI panel overlaps bottom actions")
	}
	toolbarDockShot(t, view, "08-vertical-side-bottom")
	// Horizontal bottom controls use Left/Right; vertical side controls retain
	// Up/Down. Home/End also reveal tools when the corresponding strip scrolls.
	result := state.Key(gfx.KeyEvent{Target: hitConnExport, Key: gfx.KeyRight})
	if result.Focus != hitWindowSettings || !result.Handled {
		t.Fatalf("bottom keyboard = %+v", result)
	}
	want := state.toolbars.snapshot()
	state.setTabOrientation(tabsHorizontal)
	if err := view.Resize(1200, 800); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.toolbars.snapshot(), want) {
		t.Fatal("tab orientation changed icon placement")
	}
	state.save()
	if _, _, err := configstore.ReadOnly(options.StatePath, 20); !errors.Is(err, configstore.ErrTooNew) {
		t.Fatalf("old writer can discard dock layout: %v", err)
	}
	reopened := newTestState(t, options)
	if !reflect.DeepEqual(reopened.toolbars.snapshot(), want) {
		t.Fatal("bottom layout did not survive restart")
	}
	state.setTabOrientation(tabsVertical)
	state.ai.open = false
	// Scroll a bottom strip that is deliberately narrower than its real actions.
	for _, spec := range toolbarCatalog[toolbarTerminal] {
		state.moveToolbarDockTool(spec.id, toolbarDockBottom, gfx.ID{})
	}
	for _, spec := range toolbarCatalog[toolbarWindow] {
		state.moveToolbarDockTool(spec.id, toolbarDockBottom, gfx.ID{})
	}
	if err := view.Resize(560, 420); err != nil {
		t.Fatal(err)
	}
	if err := view.Scroll(gfx.ScrollEvent{X: 400, Y: 400, DeltaY: 1000}); err != nil {
		t.Fatal(err)
	}
	if state.toolbarDock.bottomScroll[0] <= 0 {
		t.Fatal("bottom overflow did not scroll")
	}
	toolbarDockShot(t, view, "09-vertical-bottom-scroll")
}

func TestToolbarDockVerticalTabsIndependentLeftAndRight(t *testing.T) {
	state, view, options := toolbarDockView(t)
	state.setTabOrientation(tabsVertical)
	state.setTerminalToolbarPosition(terminalToolbarLeft)
	if err := view.Resize(1200, 800); err != nil {
		t.Fatal(err)
	}
	left := dualHit(t, view, hitPaneSplitHorizontal)
	if left.X != state.verticalTabsReserve(state.viewport)+4 {
		t.Fatalf("legacy left toolbar sits under vertical tabs: %+v", left)
	}
	drag := func(id gfx.ID, to gfx.Point) {
		t.Helper()
		box := dualHit(t, view, id)
		if err := view.Press(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Move(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
		if err := view.Release(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
	}
	drag(hitAIToggle, gfx.Point{X: 1180, Y: 120})
	if state.toolbarDockZoneFor(toolbarTerminal, "split-horizontal") != toolbarDockLeftTop {
		t.Fatal("first customized layout moved existing left terminal tools to right")
	}
	drag(hitSFTPOpen, gfx.Point{X: 1180, Y: 200})
	leftX := state.verticalTabsReserve(state.viewport) + 20
	drag(hitTheme, gfx.Point{X: leftX, Y: 700})
	drag(hitConnExport, gfx.Point{X: 500, Y: 790})
	left, right := dualHit(t, view, hitPaneSplitHorizontal), dualHit(t, view, hitSFTPOpen)
	if left.X != state.verticalTabsReserve(state.viewport)+4 || right.X != 1164 {
		t.Fatalf("independent sides: left=%+v right=%+v", left, right)
	}
	if theme := dualHit(t, view, hitTheme); theme.X != left.X || theme.Y < 650 {
		t.Fatalf("left lower group=%+v", theme)
	}
	if err := view.ClickLabel(state.messages.AIPanel.ToggleTip); err != nil {
		t.Fatal(err)
	}
	workspace := state.baseWorkspacePixelBounds(state.viewport)
	if workspace.X != state.verticalTabsReserve(state.viewport)+terminalToolRailExtent || workspace.X+workspace.Width != 1160 {
		t.Fatalf("workspace does not reserve both toolbars: %+v", workspace)
	}
	toolbarDockShot(t, view, "10-vertical-independent-left-right")
	state.setVerticalTabsAutoHide(true)
	if err := view.Resize(1200, 800); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(900, 100); err != nil {
		t.Fatal(err)
	}
	if left := dualHit(t, view, hitPaneSplitHorizontal); left.X != state.verticalTabsReserve(state.viewport)+4 {
		t.Fatalf("collapsed tabs cover left tools: %+v", left)
	}
	geometry := state.layoutForSession(state.activeSessionID)
	if err := view.Move(10, 120); err != nil {
		t.Fatal(err)
	}
	if !state.verticalTabs.over {
		t.Fatal("vertical tabs did not expand")
	}
	if state.layoutForSession(state.activeSessionID).area != geometry.area {
		t.Fatal("hover expansion resized the terminal")
	}
	if err := view.Move(900, 100); err != nil {
		t.Fatal(err)
	}
	toolbarDockShot(t, view, "11-vertical-auto-hide-left-right")
	state.save()
	reopened := newTestState(t, options)
	if !reflect.DeepEqual(reopened.toolbars.snapshot(), state.toolbars.snapshot()) {
		t.Fatal("independent left/right positions were not restored")
	}
}

func TestToolbarDockBottomLeftAndRightGroups(t *testing.T) {
	state, view, options := toolbarDockView(t)
	state.setTabOrientation(tabsVertical)
	if err := view.Resize(1200, 800); err != nil {
		t.Fatal(err)
	}
	drag := func(id gfx.ID, to gfx.Point) {
		t.Helper()
		box := dualHit(t, view, id)
		if err := view.Press(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Move(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
		if err := view.Release(to.X, to.Y); err != nil {
			t.Fatal(err)
		}
	}
	drag(hitConnExport, gfx.Point{X: 400, Y: 788})
	drag(hitTheme, gfx.Point{X: 1100, Y: 788})
	if state.toolbarDockZoneFor(toolbarWindow, "theme") != toolbarDockBottomRight {
		t.Fatal("right half did not select the right bottom group")
	}
	if box := dualHit(t, view, hitWindowSettings); box.Width > 0 {
		if err := view.RightClick(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.ClickLabel(state.messages.Toolbars.MoveBottomRight); err != nil {
			t.Fatal(err)
		}
	}
	assertAnchors := func() {
		t.Helper()
		left, right := dualHit(t, view, hitConnExport), dualHit(t, view, hitWindowSettings)
		bounds := state.toolbarDockBottomBounds(state.viewport, false)
		if left.X != bounds.X+4 || right.X+right.Width != bounds.X+bounds.Width-4 {
			t.Fatalf("bottom groups do not anchor to opposite edges: left=%+v right=%+v bounds=%+v", left, right, bounds)
		}
		if left.X+left.Width >= dualHit(t, view, hitTheme).X {
			t.Fatal("bottom groups lost their empty middle")
		}
	}
	assertAnchors()
	theme := dualHit(t, view, hitTheme)
	drag(hitWindowSettings, gfx.Point{X: theme.X - 1, Y: 788})
	if dualHit(t, view, hitWindowSettings).X >= dualHit(t, view, hitTheme).X {
		t.Fatal("right group did not reorder")
	}
	toolbarDockShot(t, view, "13-bottom-left-right")
	// Move across the middle between the groups with the keyboard.
	result := state.Key(gfx.KeyEvent{Target: hitConnExport, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != hitWindowSettings {
		t.Fatalf("bottom group keyboard traversal: %+v", result)
	}
	drag(hitWindowSettings, gfx.Point{X: 400, Y: 788})
	if state.toolbarDockZoneFor(toolbarWindow, "settings") != toolbarDockBottom {
		t.Fatal("crossing the middle did not move between bottom groups")
	}
	for _, orientation := range []tabOrientation{tabsHorizontal, tabsVertical} {
		state.setTabOrientation(orientation)
		if err := view.Resize(1000, 700); err != nil {
			t.Fatal(err)
		}
		box := dualHit(t, view, hitTheme)
		if box.X+box.Width != 996 || box.Y < 660 {
			t.Fatalf("right group did not follow resized edge: %+v", box)
		}
	}
	state.save()
	reopened := newTestState(t, options)
	if !reflect.DeepEqual(state.toolbars.snapshot(), reopened.toolbars.snapshot()) {
		t.Fatal("bottom groups did not survive restart")
	}
	// Both groups overflow independently in a narrow window.
	for _, spec := range toolbarCatalog[toolbarTerminal] {
		state.moveToolbarDockTool(spec.id, toolbarDockBottom, gfx.ID{})
	}
	for _, spec := range toolbarCatalog[toolbarWindow] {
		state.moveToolbarDockTool(spec.id, toolbarDockBottomRight, gfx.ID{})
	}
	if err := view.Resize(560, 420); err != nil {
		t.Fatal(err)
	}
	for i, viewport := range state.toolbarDock.bottomViewports {
		if err := view.Scroll(gfx.ScrollEvent{X: viewport.X + viewport.Width/2, Y: viewport.Y + 8, DeltaY: 1000}); err != nil {
			t.Fatal(err)
		}
		if state.toolbarDock.bottomScroll[i] <= 0 {
			t.Fatalf("bottom group %d could not scroll", i)
		}
	}
	toolbarDockShot(t, view, "14-bottom-groups-overflow")
}

// TestToolbarDockZoneButtonFitsItsLabel keeps the dock zone control as wide as
// its own label. A fixed 98px box held the Chinese "顶部" and left the English
// "Bottom (right)" wider than the button's own padding, so the caption gives up
// the room instead.
func TestToolbarDockZoneButtonFitsItsLabel(t *testing.T) {
	dir := t.TempDir()
	state := newTestState(t, Options{StatePath: filepath.Join(dir, "state.json"), KnownHostsPath: filepath.Join(dir, "known_hosts")})
	state.setLanguage(i18n.English)
	state.Click(hitLocalTerminal)
	state.Wake()
	state.moveToolbarDockTool(hitPaneClose, toolbarDockBottomRight, hitBroadcast)
	state.openToolbarSettings(toolbarTerminal)
	view, err := headless.Open(app.Config{Width: 1200, Height: 800, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{ReferenceText: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	label := state.messages.Toolbars.BottomRight
	hit, ok := view.Hit(label)
	if !ok {
		t.Fatalf("%q has no dock zone control", label)
	}
	need := components.BeginButton(&gfx.Canvas{}, state.Theme(), gfx.ID{}, gfx.Rect{}).SizeToFit(label).BoundsWidth()
	if hit.Bounds.Width+0.5 < need {
		t.Fatalf("%q needs %.1fpx in a %.1fpx button: the label overflows its padding", label, need, hit.Bounds.Width)
	}
}

func TestToolbarDockPresenceMatchesThePaintedTerminalTools(t *testing.T) {
	size := gfx.Size{Width: 1200, Height: 800}
	for _, spec := range toolbarCatalog[toolbarTerminal] {
		t.Run(spec.key, func(t *testing.T) {
			state, _, _, _ := liveSession(t)
			state.activeSection = sectionTerminal
			state.terminalToolbarPosition = terminalToolbarRight
			entries := state.toolbars.customize(toolbarTerminal)
			for index := range entries {
				entries[index].Enabled = entries[index].Key == spec.key
			}
			state.Resize(size)
			var canvas gfx.Canvas
			state.Paint(&canvas, size)
			// 0 is the left side, 1 the right.
			var painted [2]bool
			for _, other := range toolbarCatalog[toolbarTerminal] {
				hit, ok := hitByID(&canvas, other.id)
				if !ok {
					continue
				}
				if hit.Bounds.X+hit.Bounds.Width/2 < size.Width/2 {
					painted[0] = true
				} else {
					painted[1] = true
				}
			}
			for _, side := range []struct {
				left bool
				at   int
			}{{true, 0}, {false, 1}} {
				reserved := state.toolbarDockSideReserveFor(side.left, true) > 0
				if reserved != painted[side.at] {
					t.Fatalf("%s: reserve(left=%v)=%v, painted=%v", spec.key, side.left, reserved, painted[side.at])
				}
			}
		})
	}
}
