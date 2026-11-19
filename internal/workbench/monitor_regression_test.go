//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/monitoring"
)

func canvasHitWhere(canvas *gfx.Canvas, match func(gfx.Hit) bool) (gfx.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if match(hit) {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

func canvasHitByID(canvas *gfx.Canvas, id gfx.ID) (gfx.Hit, bool) {
	return canvasHitWhere(canvas, func(hit gfx.Hit) bool { return hit.ID == id })
}

func canvasHitByLabel(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	return canvasHitWhere(canvas, func(hit gfx.Hit) bool { return hit.Label == label })
}

func canvasHitByLabelRole(canvas *gfx.Canvas, label string, role gfx.Role) (gfx.Hit, bool) {
	return canvasHitWhere(canvas, func(hit gfx.Hit) bool { return hit.Label == label && hit.Role == role })
}

func canvasTextByValue(canvas *gfx.Canvas, value string) (gfx.Text, bool) {
	for _, line := range canvas.Texts() {
		if line.Value == value {
			return line, true
		}
	}
	return gfx.Text{}, false
}

func canvasTextIn(canvas *gfx.Canvas, value string, within gfx.Rect) (gfx.Text, bool) {
	for _, line := range canvas.Texts() {
		if line.Value != value {
			continue
		}
		if line.Bounds.X >= within.X && line.Bounds.X < within.X+within.Width &&
			line.Bounds.Y >= within.Y && line.Bounds.Y < within.Y+within.Height {
			return line, true
		}
	}
	return gfx.Text{}, false
}

func canvasHasRect(canvas *gfx.Canvas, want gfx.Rect) bool {
	for _, rect := range canvas.Rectangles() {
		if rect.Bounds == want {
			return true
		}
	}
	return false
}

// The header's controls keep the hand-painted page's explicit geometry: the
// mode tabs at 136x34, the search at x+152, the sampling selects at their old
// offsets, and the actions at least 112 wide with a 12px gap.
func TestDashboardHeaderKeepsExplicitGeometry(t *testing.T) {
	state := dashboardNativeFixture(t)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	m := state.messages.Dashboard
	inner := max(state.catalogSectionBounds().Width-36, 0)
	if inner < 1000 {
		t.Fatalf("fixture is not a wide dashboard: %v", inner)
	}
	title, ok := canvasHitByLabelRole(&canvas, m.Title, gfx.RoleCombobox)
	if !ok {
		t.Fatal("panel selector is missing")
	}
	if title.Bounds.Height != 36 {
		t.Fatalf("panel selector height = %v, want 36", title.Bounds.Height)
	}
	simpleMode, ok := canvasHitByLabelRole(&canvas, m.Simple, gfx.RoleTab)
	if !ok {
		t.Fatal("simple mode tab is missing")
	}
	detailedMode, ok := canvasHitByLabelRole(&canvas, m.Detailed, gfx.RoleTab)
	if !ok {
		t.Fatal("detailed mode tab is missing")
	}
	modeLeft := min(simpleMode.Bounds.X, detailedMode.Bounds.X)
	modeRight := max(simpleMode.Bounds.X+simpleMode.Bounds.Width, detailedMode.Bounds.X+detailedMode.Bounds.Width)
	modeHeight := max(simpleMode.Bounds.Height, detailedMode.Bounds.Height)
	if modeRight-modeLeft+6 != 136 || modeHeight+6 != 34 {
		t.Fatalf("mode tabs = %v/%v, want a 136x34 strip", simpleMode.Bounds, detailedMode.Bounds)
	}
	if simpleMode.Bounds.Y-3 != title.Bounds.Y+58 {
		t.Fatalf("mode row y = %v, want %v", simpleMode.Bounds.Y, title.Bounds.Y+58)
	}
	search, ok := canvasHitByLabelRole(&canvas, m.Search, gfx.RoleTextField)
	if !ok {
		if search, ok = canvasHitByLabel(&canvas, m.Search); !ok {
			if search, ok = canvasHitByLabel(&canvas, m.Search); !ok {
				t.Fatalf("search field is missing; hits: %+v", canvas.Hits())
			}
		}
	}
	rowTop := title.Bounds.Y + 58
	if simpleMode.Bounds.Y-3 != rowTop {
		t.Fatalf("mode row top = %v, want %v", simpleMode.Bounds.Y-3, rowTop)
	}
	if search.Bounds.X != title.Bounds.X+152 || search.Bounds.Y != rowTop {
		t.Fatalf("search at %v, want x=%v y=%v", search.Bounds, title.Bounds.X+152, rowTop)
	}
	if want := max(100, inner-136-16-316); search.Bounds.Width != want {
		t.Fatalf("search width = %v, want %v", search.Bounds.Width, want)
	}
	if search.Bounds.Height != 34 {
		t.Fatalf("search height = %v, want 34", search.Bounds.Height)
	}
	sampling, ok := canvasHitByLabelRole(&canvas, m.Sampling, gfx.RoleCombobox)
	if !ok {
		t.Fatal("sampling select is missing")
	}
	retention, ok := canvasHitByLabelRole(&canvas, m.Retention, gfx.RoleCombobox)
	if !ok {
		t.Fatal("retention select is missing")
	}
	if sampling.Bounds.X != title.Bounds.X+inner-304 || sampling.Bounds.Width != 152 || sampling.Bounds.Height != 34 {
		t.Fatalf("sampling select = %v", sampling.Bounds)
	}
	if retention.Bounds.X != title.Bounds.X+inner-142 || retention.Bounds.Width != 142 || retention.Bounds.Height != 34 {
		t.Fatalf("retention select = %v", retention.Bounds)
	}
	newButton, ok := canvasHitByLabelRole(&canvas, m.New, gfx.RoleButton)
	if !ok {
		t.Fatal("new action is missing")
	}
	stop, ok := canvasHitByLabelRole(&canvas, m.Stop, gfx.RoleButton)
	if !ok {
		t.Fatal("start/stop action is missing")
	}
	if newButton.Bounds.Height != 36 || stop.Bounds.Height != 36 {
		t.Fatalf("action heights = %v, %v, want 36", newButton.Bounds.Height, stop.Bounds.Height)
	}
	if newButton.Bounds.Width < 112 || stop.Bounds.Width < 112 {
		t.Fatalf("action widths = %v, %v, want at least 112", newButton.Bounds.Width, stop.Bounds.Width)
	}
	if newButton.Bounds.X+newButton.Bounds.Width+12 != stop.Bounds.X {
		t.Fatalf("action gap = %v, want 12", stop.Bounds.X-(newButton.Bounds.X+newButton.Bounds.Width))
	}
	if stop.Bounds.X+stop.Bounds.Width != title.Bounds.X+inner {
		t.Fatalf("actions end at %v, want %v", stop.Bounds.X+stop.Bounds.Width, title.Bounds.X+inner)
	}
}

// The header's first divider sits ten pixels under the control row, the body
// eight pixels under it, and the footer line five pixels under the body; the
// scroll body ends at footer.Y-5.
func TestDashboardBodyKeepsItsVerticalRhythm(t *testing.T) {
	state := dashboardNativeFixture(t)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	m := state.messages.Dashboard
	title, ok := canvasHitByLabelRole(&canvas, m.Title, gfx.RoleCombobox)
	if !ok {
		t.Fatal("panel selector is missing")
	}
	headerY := title.Bounds.Y
	controlH := float32(42)
	bodyTop := headerY + 76 + controlH
	if state.dashboard.body.Y != bodyTop {
		t.Fatalf("body top = %v, want %v", state.dashboard.body.Y, bodyTop)
	}
	headerLine := gfx.Rect{X: title.Bounds.X, Y: headerY + 68 + controlH, Width: state.dashboard.body.Width, Height: 1}
	if !canvasHasRect(&canvas, headerLine) {
		t.Fatalf("header divider at %v missing", headerLine)
	}
	footerLine := gfx.Rect{X: state.dashboard.body.X, Y: state.dashboard.body.Y + state.dashboard.body.Height + 5, Width: state.dashboard.body.Width, Height: 1}
	if !canvasHasRect(&canvas, footerLine) {
		t.Fatalf("footer divider at %v missing", footerLine)
	}
	count, ok := canvasTextByValue(&canvas, fmt.Sprintf(m.Count, len(state.dashboardPanel().Hosts)))
	if !ok {
		t.Fatal("footer count is missing")
	}
	if count.Bounds.Y < footerLine.Y || count.Bounds.Y+count.Bounds.Height > footerLine.Y+34 {
		t.Fatalf("footer count %v is outside the footer row under %v", count.Bounds, footerLine.Y)
	}
}

// A card's controls are placed at the row's own offsets once: the tabs at +5,
// the terminal and sudo at +5, and the retry over the status at +7, never over
// the tabs.
func TestDashboardCardControlsUseTheRowOnce(t *testing.T) {
	state := dashboardNativeFixture(t)
	p := state.dashboardPanel()
	inner := max(state.catalogSectionBounds().Width-36, 0)
	r := state.dashboardRow(p, "host-0", int(inner))
	row := gfx.Rect{X: 0, Y: 120, Width: inner - 12, Height: dashboardHostHeight(r, inner-12)}
	var canvas gfx.Canvas
	controls := state.dashboardRowControlRects(&canvas, p, state.hosts[0], r, row, true, false)
	if controls.tabs.Y != 5 || controls.terminal.Y != 5 || controls.sudo.Y != 5 {
		t.Fatalf("controls carry the row Y twice: %+v", controls)
	}
	if controls.retry.Y != 7 || controls.retry.Height != 28 {
		t.Fatalf("retry = %v, want the status rect at +7", controls.retry)
	}
	if controls.retry == controls.tabs {
		t.Fatal("retry overwrote the view tabs")
	}
	if wantX := controls.tabs.X + controls.tabs.Width + 8; controls.retry.X != wantX {
		t.Fatalf("retry x = %v, want %v", controls.retry.X, wantX)
	}
}

// The simple view keeps its headings inside the scrolling content and lays the
// cards out at least 960 wide, with the controls over the same width.
func TestDashboardSimpleViewScrollGeometry(t *testing.T) {
	state := dashboardNativeFixture(t)
	p := state.dashboardPanel()
	p.Simple = true
	size := gfx.Size{Width: 800, Height: 700}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	scroll := state.sectionBodies.dashboard.scroll
	if !scroll.Valid() {
		t.Fatal("dashboard scroll area is missing")
	}
	area := scroll.ScrollState()
	if area.ContentWidth < 960 {
		t.Fatalf("simple content width = %v, want at least 960", area.ContentWidth)
	}
	m := state.messages.Dashboard
	dashboardText := func(value string) (gfx.Text, bool) {
		for _, line := range canvas.Texts() {
			if line.Value == value && line.Bounds.X >= state.dashboard.body.X {
				return line, true
			}
		}
		return gfx.Text{}, false
	}
	head, ok := dashboardText(m.Host)
	if !ok {
		t.Fatal("simple column headings are missing")
	}
	// The terminal action is the last column of the 960-wide row, so it is off
	// screen until the content is scrolled sideways.
	state.sectionBodies.dashboard.scroll.ScrollState().ScrollToX(area.MaxX())
	state.Paint(&canvas, size)
	terminal, ok := canvasHitByLabel(&canvas, m.Terminal)
	if !ok {
		t.Fatal("simple row terminal action is missing")
	}
	offsetY := state.sectionBodies.dashboard.scroll.ScrollState().Offset
	offsetX := state.sectionBodies.dashboard.scroll.ScrollState().OffsetX
	if want := state.dashboard.body.X + 960 - 12 - 38 - offsetX; terminal.Bounds.X != want {
		t.Fatalf("terminal x = %v, want the 960-wide row at %v (offset %v)", terminal.Bounds.X, want, offsetX)
	}
	before := head.Bounds.Y
	state.dashboard.scroll = 40
	state.dashboard.scrollSynced = -1
	state.Paint(&canvas, size)
	if head, ok = dashboardText(m.Host); !ok {
		t.Fatal("simple column headings disappeared after scrolling")
	}
	if head.Bounds.Y+offsetY >= before+40-1 {
		t.Fatalf("simple headings did not scroll with the content: %v then %v", before, head.Bounds.Y)
	}
}

// The sampling clock reads the interval, paused state and live count each
// declaration; an unwritten view left its hands still.
func TestDashboardRefreshClockTracksCollection(t *testing.T) {
	state := dashboardNativeFixture(t)
	p := state.dashboardPanel()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	m := state.messages.Dashboard
	if state.dashboard.refresh.interval != p.Interval {
		t.Fatalf("refresh interval = %v, want %v", state.dashboard.refresh.interval, p.Interval)
	}
	if want := fmt.Sprintf(m.Every, p.Interval); state.dashboard.refresh.intervalText != want {
		t.Fatalf("refresh text = %q, want %q", state.dashboard.refresh.intervalText, want)
	}
	if state.dashboard.refresh.paused {
		t.Fatal("a running panel was recorded as paused")
	}
	if state.dashboard.refresh.count == 0 || state.dashboard.refresh.version == 0 {
		t.Fatal("a live panel did not advance the clock")
	}
	state.dashboard.running[p.ID] = false
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !state.dashboard.refresh.paused {
		t.Fatal("a stopped panel was not recorded as paused")
	}
}

// The collection state sits in the header on a wide panel and in the muted
// footer on a narrow one, and counts every host in the panel, not only the
// hosts the search shows.
func TestDashboardCollectionStateCountsEveryHost(t *testing.T) {
	state := dashboardNativeFixture(t)
	p := state.dashboardPanel()
	m := state.messages.Dashboard
	want := fmt.Sprintf(m.Collecting, len(p.Hosts), len(p.Hosts))
	state.dashboard.search = *text.NewEditor("no-such-host")
	var wide gfx.Canvas
	state.Paint(&wide, gfx.Size{Width: 1400, Height: 900})
	line, ok := canvasTextByValue(&wide, want)
	if !ok {
		t.Fatalf("%q missing from the wide header", want)
	}
	title, _ := canvasHitByLabelRole(&wide, m.Title, gfx.RoleCombobox)
	if line.Bounds.Y+line.Bounds.Height > title.Bounds.Y+36 {
		t.Fatalf("wide collection state %v is not in the header row", line.Bounds)
	}
	var narrow gfx.Canvas
	state.Paint(&narrow, gfx.Size{Width: 800, Height: 700})
	line, ok = canvasTextByValue(&narrow, want)
	if !ok {
		t.Fatalf("%q missing from the narrow footer", want)
	}
	if line.Bounds.Y <= state.dashboard.body.Y+state.dashboard.body.Height {
		t.Fatalf("narrow collection state %v is not in the footer", line.Bounds)
	}
}

// The dashboard's first-run state paints its icon, which the empty declaration
// owns.
func TestDashboardEmptyStatePaintsItsIcon(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionDashboard
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	icon := state.empty.iconBox.Rect()
	if icon.Width <= 0 || icon.Height <= 0 {
		t.Fatal("empty state icon box was not laid out")
	}
	for _, triangle := range canvas.Triangles() {
		for _, point := range triangle.Points {
			if pointInRect(point.X, point.Y, icon) {
				return
			}
		}
	}
	t.Fatalf("no icon drawn in the empty state's tile %v", icon)
}

// The monitor panel centers its loading/empty block, wraps it to the panel
// width, and paints no footer band or freshness line when it has no sections.
func TestMonitorEmptyStateCentersWithoutFooter(t *testing.T) {
	for _, stateName := range []string{"loading", "empty"} {
		t.Run(stateName, func(t *testing.T) {
			state, session := monitorFixture(t)
			if stateName == "loading" {
				session.stats.state = remoteStatsMeasuring
				session.stats.panel.count = 0
			} else {
				session.stats = nil
			}
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1280, Height: 900})
			m := state.messages.Monitor
			if _, ok := canvasTextByValue(&canvas, m.Updated); ok {
				t.Fatal("a footer freshness line was declared without sections")
			}
			if stateName == "empty" {
				band := gfx.Rect{X: state.monitorRect.X, Y: state.monitorRect.Y + state.monitorRect.Height - 24, Width: state.monitorRect.Width, Height: 24}
				if canvasHasRect(&canvas, band) {
					t.Fatalf("empty panel painted its footer band %v", band)
				}
				return
			}
			message := state.remoteStatsText(session.stats)
			line, ok := canvasTextByValue(&canvas, message)
			if !ok {
				t.Fatalf("%q missing", message)
			}
			if line.Bounds.Width > state.monitorRect.Width-32+1 {
				t.Fatalf("message width %v exceeds the panel inset", line.Bounds.Width)
			}
			top := state.monitorRect.Y + infoBarHeight + monitorControlsHeight + monitorHostInfoHeight
			bottom := state.monitorRect.Y + state.monitorRect.Height
			center := line.Bounds.Y + line.Bounds.Height/2
			if center <= top || center >= bottom {
				t.Fatalf("message center %v is outside the body %v..%v", center, top, bottom)
			}
			if middle := (top + bottom) / 2; center < middle-20 || center > middle+45 {
				t.Fatalf("message center %v is not vertically centered around %v", center, middle)
			}
		})
	}
}

// The monitor header keeps the close action at the right edge and the move
// action beside it, the title at x+35, and an 82-wide interval with the pause
// four pixels to its right.
func TestMonitorHeaderOrderAndWidths(t *testing.T) {
	state, _ := monitorFixture(t)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1280, Height: 900})
	b := state.monitorRect
	closeHit, ok := canvasHitByID(&canvas, hitMonitorClose)
	if !ok {
		t.Fatal("close action is missing")
	}
	moveHit, ok := canvasHitByID(&canvas, hitMonitorSide)
	if !ok {
		t.Fatal("move action is missing")
	}
	if closeHit.Bounds.X <= moveHit.Bounds.X {
		t.Fatalf("close %v is not right of move %v", closeHit.Bounds, moveHit.Bounds)
	}
	if closeHit.Bounds.X != b.X+b.Width-34 || closeHit.Bounds.Width != 24 {
		t.Fatalf("close at %v, want the right edge %v", closeHit.Bounds, b.X+b.Width-34)
	}
	if moveHit.Bounds.X != b.X+b.Width-64 || moveHit.Bounds.Width != 24 {
		t.Fatalf("move at %v, want %v", moveHit.Bounds, b.X+b.Width-64)
	}
	title, ok := canvasTextIn(&canvas, state.messages.Monitor.Title, gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: infoBarHeight})
	if !ok {
		t.Fatal("panel title is missing")
	}
	if title.Bounds.X != b.X+35 {
		t.Fatalf("title x = %v, want %v", title.Bounds.X, b.X+35)
	}
	interval, ok := canvasHitByID(&canvas, hitMonitorInterval)
	if !ok {
		t.Fatal("interval control is missing")
	}
	if interval.Bounds.Width != 82 || interval.Bounds.Height != 26 {
		t.Fatalf("interval = %v, want 82x26", interval.Bounds)
	}
	titleWidth := canvas.TextWidthWeight(state.messages.Monitor.Title, 14, gfx.WeightSemibold)
	if interval.Bounds.X != b.X+35+titleWidth+12 {
		t.Fatalf("interval x = %v, want %v", interval.Bounds.X, b.X+35+titleWidth+12)
	}
	pause, ok := canvasHitByID(&canvas, hitMonitorPause)
	if !ok {
		t.Fatal("pause control is missing")
	}
	if pause.Bounds.X != interval.Bounds.X+82+4 || pause.Bounds.Width != 24 {
		t.Fatalf("pause = %v, want four pixels right of the interval", pause.Bounds)
	}
}

// In drawer mode the panel's own rectangle is the content the drawer lays out,
// not a rectangle copied from the window edge before layout.
func TestMonitorDrawerRectIsItsContent(t *testing.T) {
	state, _ := monitorFixture(t)
	size := gfx.Size{Width: 800, Height: 700}
	base, drawer := state.monitorBaseBounds(size)
	if !drawer {
		t.Fatal("fixture is not a drawer at this width")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	side := components.DrawerRight
	if context.MonitorLeft {
		side = components.DrawerLeft
	}
	expected := components.BeginDrawer(&canvas, state.Theme(), gfx.Rect{Width: size.Width, Height: size.Height},
		components.DrawerOptions{Side: side, ContentExtent: base.Width}).Layout("").Content
	if state.monitorRect != expected {
		t.Fatalf("drawer monitor rect = %v, want the drawer content %v", state.monitorRect, expected)
	}
}

// A click on the dashboard's scroll region focuses it and the arrow keys move
// the page, as the hand-painted viewport did.
func TestDashboardScrollKeepsKeyboardScrolling(t *testing.T) {
	state := dashboardNativeFixture(t)
	view, err := headless.Open(state.WindowConfig(app.Config{Title: "Motty", Width: 900, Height: 700, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	hit, ok := hitByIDInSnapshot(view.Snapshot(), hitDashboardScroll)
	if !ok || !hit.Focusable {
		t.Fatalf("dashboard scroll region has no focusable hit: %+v", hit)
	}
	if err := view.Click(hit.Bounds.X+2, hit.Bounds.Y+80); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	if state.dashboard.scroll != 32 {
		t.Fatalf("Down on the focused dashboard scroll region left it at %v", state.dashboard.scroll)
	}
}

// A click on the monitor's scroll region focuses it and the arrow keys move its
// sections.
func TestMonitorScrollKeepsKeyboardScrolling(t *testing.T) {
	state, session := monitorFixture(t)
	view, err := headless.Open(state.WindowConfig(app.Config{Title: "Motty", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	hit, ok := hitByIDInSnapshot(view.Snapshot(), hitMonitorScroll)
	if !ok || !hit.Focusable {
		t.Fatalf("monitor scroll region has no focusable hit: %+v", hit)
	}
	if err := view.Click(hit.Bounds.X+4, hit.Bounds.Y+80); err != nil {
		t.Fatal(err)
	}
	before := session.stats.panel.scroll
	if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	if session.stats.panel.scroll != before+28 {
		t.Fatalf("Down on the focused monitor scroll region left it at %v, want %v", session.stats.panel.scroll, before+28)
	}
}

// The retry action replaces the status label, not the view tabs.
func TestDashboardRetryPaintsOverTheStatus(t *testing.T) {
	state := dashboardNativeFixture(t)
	state.dashboard.engine.SetStatus("host-1", monitoring.Failed)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	retry, ok := canvasHitByID(&canvas, hitDashboardRetryList.At(int(state.hosts[1].ID)))
	if !ok {
		t.Fatal("retry action is missing on a failed host")
	}
	// The retry action of that card sits over its status, two pixels below the
	// card's view tabs and to their right, never in their place.
	var sameRow *gfx.Hit
	for _, hit := range canvas.Hits() {
		if hit.Label == state.messages.Monitor.Overview && hit.Role == gfx.RoleTab && math.Abs(float64(hit.Bounds.Y-retry.Bounds.Y)) <= 10 {
			found := hit
			sameRow = &found
			break
		}
	}
	if sameRow == nil {
		t.Fatalf("the failed host's view tabs are missing")
	}
	if retry.Bounds.X <= sameRow.Bounds.X+sameRow.Bounds.Width {
		t.Fatalf("retry %v was painted over the view tabs %v", retry.Bounds, sameRow.Bounds)
	}
}
