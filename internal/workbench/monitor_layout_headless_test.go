//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	layoutcore "motty/internal/workspace"
)

func TestMonitorOuterPanelLayoutsAndResizeAtDisplayScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
			for _, left := range []bool{false, true} {
				t.Run(fmt.Sprintf("%g/files-%d/monitor-left-%v", scale, side, left), func(t *testing.T) {
					state, s := monitorFixture(t)
					original := s.stats
					id := state.deck.ActiveWorkspace()
					c, _ := state.deck.Context(id)
					c.Side = side
					c.MonitorLeft = left
					_ = state.deck.SetContext(id, c)
					view, err := headless.Open(app.Config{Title: "Panels", Width: 1600, Height: 900, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer, OnKey: state.Key}, nil, headless.Options{Scale: scale})
					if err != nil {
						t.Fatal(err)
					}
					defer view.Close()
					check := func() {
						t.Helper()
						m, f, term := state.monitorRect, state.contextRect, state.workspacePixelBounds(state.viewport)
						ordered := []gfx.Rect{f, term}
						if side == layoutcore.ContextRight {
							ordered = []gfx.Rect{term, f}
						}
						if left {
							ordered = append([]gfx.Rect{m}, ordered...)
						} else {
							ordered = append(ordered, m)
						}
						if rail, ok := state.terminalSideRailBounds(state.viewport); !ok {
							t.Fatal("shared terminal toolbar is missing")
						} else if state.terminalToolbarPosition == terminalToolbarLeft {
							if ordered[0].X != rail.X+rail.Width {
								t.Fatalf("panels do not begin inside the left rail: panels=%v rail=%v", ordered, rail)
							}
						} else if ordered[2].X+ordered[2].Width != rail.X {
							t.Fatalf("panels do not end inside the right rail: panels=%v rail=%v", ordered, rail)
						}
						for i := 0; i < 2; i++ {
							if ordered[i].X+ordered[i].Width > ordered[i+1].X {
								t.Fatalf("overlapping or wrong order: %v", ordered)
							}
						}
					}
					check()
					x := state.monitorDividerX()
					delta := float32(40)
					if !left {
						delta = -delta
					}
					state.monitorPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitMonitorDivider, Button: gfx.PointerButtonPrimary, X: x, Y: 200})
					state.monitorPointer(gfx.PointerEvent{Kind: gfx.PointerUp, X: x + delta, Y: 200})
					if err = view.Rest(); err != nil {
						t.Fatal(err)
					}
					// Rest may have nothing scheduled by direct input; resize explicitly publishes.
					if err = view.Resize(1600, 900); err != nil {
						t.Fatal(err)
					}
					if state.monitorRect.Width != monitorWidth+40 {
						t.Fatalf("resize width=%v", state.monitorRect.Width)
					}
					check()
					left = !left
					hint := state.messages.Monitor.MoveRight
					if left {
						hint = state.messages.Monitor.MoveLeft
					}
					if err = view.ClickLabel(hint); err != nil {
						t.Fatal(err)
					}
					check()
					after, _ := state.deck.Context(id)
					if after.Side != side || after.MonitorLeft != left || after.Width != c.Width || !after.Open || after.MonitorWidth != monitorWidth+40 || state.contextSession().stats != original {
						t.Fatal("moving panels changed content or widths")
					}
					_, fileHint := state.sftpPanelMoveAction()
					if err = view.ClickLabel(fileHint); err != nil {
						t.Fatal(err)
					}
					side = layoutcore.ContextRight - side
					check()
					after, _ = state.deck.Context(id)
					if after.MonitorLeft != left || after.Side != side {
						t.Fatal("file move changed monitor side")
					}
					if dir := os.Getenv("MOTTY_PANEL_SCREENSHOTS"); dir != "" && scale == 1.5 {
						if err = view.Screenshot(filepath.Join(dir, fmt.Sprintf("panels-files-%d-monitor-left-%v.png", side, left))); err != nil {
							t.Fatal(err)
						}
						if left {
							s.stats.state = remoteStatsMeasuring
							s.stats.panel.count = 0
							if err = view.Resize(1600, 900); err != nil {
								t.Fatal(err)
							}
							if err = view.Screenshot(filepath.Join(dir, "panels-loading.png")); err != nil {
								t.Fatal(err)
							}
						}
					}
				})
			}
		}
	}
}

func TestMonitorSideWithFilesClosedAndAIDocked(t *testing.T) {
	state, _ := monitorFixture(t)
	id := state.deck.ActiveWorkspace()
	size := gfx.Size{Width: 2000, Height: 900}
	state.ai.open = true
	for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
		c, _ := state.deck.Context(id)
		c.Side = side
		c.MonitorLeft = side == layoutcore.ContextLeft
		c.Open = false
		_ = state.deck.SetContext(id, c)
		state.Paint(&gfx.Canvas{}, size)
		term := state.workspacePixelBounds(size)
		monitor := state.monitorRect
		if term.Width < minPaneWidth || monitor.X+monitor.Width > size.Width-state.aiReserve(id, size) {
			t.Fatalf("overlapping AI: %v %v", term, monitor)
		}
		if side == layoutcore.ContextLeft && monitor.X != 0 {
			t.Fatal("monitor not at left edge")
		}
		c.Open = true
		_ = state.deck.SetContext(id, c)
		state.Paint(&gfx.Canvas{}, size)
		fileWidth := c.Width
		x := state.contextRect.X - 40
		if side == layoutcore.ContextLeft {
			x = state.contextRect.X + state.contextRect.Width + 40
		}
		state.dragContextDivider(gfx.PointerEvent{Viewport: size, X: x})
		after, _ := state.deck.Context(id)
		if after.Width <= fileWidth || after.MonitorWidth != c.MonitorWidth {
			t.Fatalf("file divider changed wrong panel: %+v", after)
		}
	}
}

func TestMonitorMeasuringCentersSpinnerAndText(t *testing.T) {
	state, s := monitorFixture(t)
	s.stats.state = remoteStatsMeasuring
	s.stats.panel.count = 0
	b := gfx.Rect{X: 600, Y: 40, Width: 320, Height: 800}
	var canvas gfx.Canvas
	state.paintMonitor(&canvas, b, s)
	found := false
	for _, text := range canvas.Texts() {
		if text.Value == state.remoteStatsText(s.stats) {
			found = true
			center := text.Bounds.X + canvas.TextWidthWeight(text.Value, 13, gfx.WeightRegular)/2
			if math.Abs(float64(center-(b.X+b.Width/2))) > 1 {
				t.Fatalf("text not horizontally centered: %v", text.Bounds)
			}
			bodyTop := b.Y + infoBarHeight + monitorControlsHeight
			wantY := (bodyTop+b.Y+b.Height)/2 + 16
			if math.Abs(float64(text.Bounds.Y+text.Bounds.Height/2-wantY)) > 2 {
				t.Fatalf("loading group is not vertically centered: %v want %v", text.Bounds, wantY)
			}
		}
	}
	if !found {
		t.Fatal("missing loading text")
	}
	if _, scheduled := canvas.NextFrame(); !scheduled {
		t.Fatal("spinner is not animated")
	}
}

func TestMonitorDrawerFollowsItsOwnSide(t *testing.T) {
	for _, left := range []bool{false, true} {
		state, _ := monitorFixture(t)
		id := state.deck.ActiveWorkspace()
		c, _ := state.deck.Context(id)
		c.Open = false
		c.MonitorLeft = left
		_ = state.deck.SetContext(id, c)
		view, err := headless.Open(app.Config{Width: 800, Height: 700, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer}, nil, headless.Options{Scale: 1.25})
		if err != nil {
			t.Fatal(err)
		}
		if !state.monitorDrawer(state.viewport) || (state.monitorRect.X < 400) != left {
			t.Fatalf("wrong drawer side: left=%v bounds=%v", left, state.monitorRect)
		}
		view.Close()
	}
}
