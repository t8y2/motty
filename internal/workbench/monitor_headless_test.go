//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/remotestats"
)

func TestMonitorNativePanelAtDifferentScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, _ := monitorFixture(t)
			view, err := headless.Open(app.Config{Title: "Monitor", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err = view.Rest(); err != nil {
				t.Fatal(err)
			}
			if state.monitorRect.Width != monitorWidth {
				t.Fatalf("panel width=%v", state.monitorRect.Width)
			}
			for _, id := range []struct{ label string }{{state.messages.Monitor.Interval}, {state.messages.Monitor.Pause}, {state.messages.Monitor.Overview}} {
				if _, ok := view.Hit(id.label); !ok {
					t.Fatalf("missing %q", id.label)
				}
			}
			if dir := os.Getenv("MOTTY_MONITOR_SCREENSHOTS"); dir != "" {
				if err = view.Screenshot(filepath.Join(dir, fmt.Sprintf("monitor-tabs-%g.png", scale))); err != nil {
					t.Fatal(err)
				}
				state.setAppearance(appearanceDark)
				if err = view.Resize(1280, 900); err != nil {
					t.Fatal(err)
				}
				if err = view.Screenshot(filepath.Join(dir, fmt.Sprintf("monitor-dark-overview-%g.png", scale))); err != nil {
					t.Fatal(err)
				}
				state.setAppearance(appearanceLight)
				if err = view.Resize(1280, 900); err != nil {
					t.Fatal(err)
				}
			}

			for page, label := range []string{state.messages.Monitor.Overview, state.messages.Monitor.Disks, state.messages.Monitor.Network} {
				if err = view.ClickLabel(label); err != nil {
					t.Fatal(err)
				}
				if state.contextSession().stats.panel.page != page {
					t.Fatalf("navigation page=%d, want %d", state.contextSession().stats.panel.page, page)
				}
				if dir := os.Getenv("MOTTY_MONITOR_SCREENSHOTS"); dir != "" && scale == 1.5 {
					if err = view.Screenshot(filepath.Join(dir, fmt.Sprintf("monitor-page-%d.png", page))); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err = view.ClickLabel(state.messages.Monitor.Overview); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"right", "enter"} {
				if err = view.Keys(key); err != nil {
					t.Fatal(err)
				}
			}
			if state.contextSession().stats.panel.page != 1 {
				t.Fatal("native tab keyboard navigation failed")
			}
			if err = view.ClickLabel(state.messages.Monitor.Overview); err != nil {
				t.Fatal(err)
			}
			for _, width := range []float32{280, 480, 640} {
				state.resizeMonitor(width)
				if err = view.Resize(1280, 900); err != nil {
					t.Fatal(err)
				}
				if state.monitorRect.Width != width {
					t.Fatalf("stretch width=%v want%v", state.monitorRect.Width, width)
				}
				for _, label := range []string{state.messages.Monitor.Overview, state.messages.Monitor.Disks, state.messages.Monitor.Network} {
					hit, ok := view.Hit(label)
					if !ok || hit.Bounds.Width < 60 || hit.Bounds.X < state.monitorRect.X || hit.Bounds.X+hit.Bounds.Width > state.monitorRect.X+state.monitorRect.Width {
						t.Fatalf("tab outside elastic panel: %+v", hit)
					}
				}
				if dir := os.Getenv("MOTTY_MONITOR_SCREENSHOTS"); dir != "" && scale == 1.5 {
					if err = view.Screenshot(filepath.Join(dir, fmt.Sprintf("monitor-width-%g.png", width))); err != nil {
						t.Fatal(err)
					}
				}
			}
			state.resizeMonitor(monitorWidth)
			if err = view.Resize(1280, 900); err != nil {
				t.Fatal(err)
			}
			if err = view.ClickLabel(state.messages.Monitor.Interval); err != nil {
				t.Fatal(err)
			}
			if err = view.ClickLabel(state.messages.Monitor.IntervalLabels[2]); err != nil {
				t.Fatal(err)
			}
			if state.contextSession().stats.panel.interval != 5 || state.monitorMenu {
				t.Fatal("refresh menu did not apply")
			}
			if err = view.Resize(1280, 900); err != nil {
				t.Fatal(err)
			}
			if state.monitorRect.X <= state.contextRect.X+state.contextRect.Width {
				t.Fatal("independent monitor overlaps Files")
			}
			if dir := os.Getenv("MOTTY_MONITOR_SCREENSHOTS"); dir != "" {
				if err = view.Screenshot(filepath.Join(dir, fmt.Sprintf("monitor-independent-%g.png", scale))); err != nil {
					t.Fatal(err)
				}
			}
			// Long rows and expanded summaries remain reachable in a short,
			// narrow window; the detached mode becomes a drawer.
			p := &state.contextSession().stats.panel
			p.disks[0].path = "/long/path/包含中文的目录/that-must-not-expand-the-panel/production"
			p.disksOpen, p.hardwareOpen = true, true
			state.setAppearance(appearanceDark)
			if err = view.Resize(800, 600); err != nil {
				t.Fatal(err)
			}
			if err = view.Rest(); err != nil {
				t.Fatal(err)
			}
			b := state.monitorRect
			if b.Width > monitorWidth || b.X < 0 || b.X+b.Width > 800 {
				t.Fatalf("narrow monitor=%+v", b)
			}
			if err = view.ClickLabel(state.messages.Monitor.Interval); err != nil {
				t.Fatal(err)
			}
			if err = view.ClickLabel(state.messages.Monitor.IntervalLabels[0]); err != nil {
				t.Fatal(err)
			}
			if p.interval != 1 {
				t.Fatal("drawer refresh menu did not apply")
			}
			if err = view.Scroll(gfx.ScrollEvent{X: b.X + 100, Y: b.Y + 250, DeltaY: 10000}); err != nil {
				t.Fatal(err)
			}
			if !view.HasText(state.messages.Monitor.NoHardware) || p.scroll <= 0 {
				t.Fatal("expanded content is unreachable")
			}
			if dir := os.Getenv("MOTTY_MONITOR_SCREENSHOTS"); dir != "" {
				if err = view.Screenshot(filepath.Join(dir, fmt.Sprintf("monitor-narrow-dark-%g.png", scale))); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestMonitorSpecialServerNativeStates(t *testing.T) {
	for _, kind := range []string{"windows", "partial", "unsupported"} {
		t.Run(kind, func(t *testing.T) {
			state, session := monitorFixture(t)
			state.renameSession(session, strings.Repeat("Windows服务器-", 12))
			session.Address = "administrator@[2001:db8:1234:5678:90ab:cdef:1234:5678]:2222"
			p := &session.stats.panel
			p.data.LoadUnavailable = true
			p.data.CPUCount = 32
			p.data.PrimaryDisk = `C:\`
			p.data.Kernel = "Windows Server 2025"
			p.data.Disks = []remotestats.Disk{{Path: `C:\`, Used: 128 << 30, Total: 256 << 30, Percent: 50}, {Path: `D:\`, Used: 1 << 40, Total: 2 << 40, Percent: 50}}
			p.sample.Cores = nil
			for i := range 32 {
				p.sample.Cores = append(p.sample.Cores, remotestats.Core{ID: strconv.Itoa(i), Percent: float64(i % 19)})
			}
			if kind == "partial" {
				p.sample.CPUUnavailable = true
				p.sample.NetworkUnavailable = true
				p.sample.Cores = nil
				p.data.Disks = nil
			}
			if kind == "unsupported" {
				session.stats.state = remoteStatsUnsupported
			}
			state.formatMonitor(session.stats)
			view, err := headless.Open(app.Config{Title: "Monitor", Width: 1100, Height: 760, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: 1.75})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err = view.Rest(); err != nil {
				t.Fatal(err)
			}
			if kind == "partial" && (p.cpuText != "—" || p.diskText != "—" || p.downText != "—") {
				t.Fatal("unavailable counters were represented as zero")
			}
			if dir := os.Getenv("MOTTY_MONITOR_SCREENSHOTS"); dir != "" {
				if err = view.Screenshot(filepath.Join(dir, "monitor-server-"+kind+".png")); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "windows" {
				if err = view.ClickLabel(p.coreMoreText); err != nil {
					t.Fatal(err)
				}
				if !p.coresMore {
					t.Fatal("high-core-count view did not expand")
				}
				if err = view.Scroll(gfx.ScrollEvent{X: state.monitorRect.X + 30, Y: state.monitorRect.Y + 400, DeltaY: 10000}); err != nil {
					t.Fatal(err)
				}
				if p.scroll <= 0 {
					t.Fatal("expanded core data cannot scroll")
				}
			}
		})
	}
}

func TestMonitorIdleHostNativeRendering(t *testing.T) {
	state, session := monitorFixture(t)
	p := &session.stats.panel
	p.data.CPUCount, p.data.Load = 18, [3]float64{.01, .01, 0}
	p.data.Disks = []remotestats.Disk{{Path: "/", Used: 38 << 30, Total: 10069 * (1 << 30) / 10, Percent: 4}, {Path: "/home/motty", Used: 38 << 30, Total: 10069 * (1 << 30) / 10, Percent: 4}, {Path: "/run", Used: 1024, Total: 1 << 30, Virtual: true}}
	p.data.Networks = nil
	p.count = 0
	for i := range 6 {
		state.sampleMonitor(session.stats, remotestats.Sample{CPUPercent: .05, MemoryUsedBytes: 1 << 30, MemoryTotalBytes: 4 << 30, ReceiveBytesPerSecond: uint64(300 + i%3*450), SendBytesPerSecond: uint64(100 + i%4*200), NetworkInterface: "eth0", UptimeSeconds: 10800})
		p.x[i] = float32(i * 2)
	}
	state.formatMonitor(session.stats)
	session.stats.parts = remoteStatsParts(p.sample)
	session.stats.text = formatRemoteStats(p.sample)
	view, err := headless.Open(app.Config{Title: "Monitor", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err = view.Rest(); err != nil {
		t.Fatal(err)
	}
	if view.HasText("CPU 0%") {
		t.Fatal("the visible monitor duplicates its metrics in the terminal header")
	}
	if dir := os.Getenv("MOTTY_MONITOR_SCREENSHOTS"); dir != "" {
		if err = view.Screenshot(filepath.Join(dir, "monitor-idle-1.5.png")); err != nil {
			t.Fatal(err)
		}
	}
	state.closeMonitor()
	if err = view.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if session.stats != nil || state.monitorShown(state.deck.ActiveWorkspace()) {
		t.Fatal("closing monitor did not stop sampling")
	}
}
