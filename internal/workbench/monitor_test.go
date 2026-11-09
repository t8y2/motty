package workbench

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func monitorFixture(t testing.TB) (*State, *Session) {
	t.Helper()
	state, s, _, _ := openTestSFTP(t)
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceLight)
	state.renameSession(s, "docker-ubuntu")
	s.host.Color = "#3b82f6"
	s.stats = &remoteStatsView{state: remoteStatsReady}
	p := &s.stats.panel
	p.interval = 2
	p.data = remotestats.Details{CPUCount: 12, Load: [3]float64{2.31, 1.84, 1.22}, Uptime: 69242400, Kernel: "4.18.0",
		Disks: []remotestats.Disk{
			{Path: "/", Used: 2096 * (1 << 30) / 10, Total: 2952 * (1 << 30) / 10, Percent: 71},
			{Path: "/data", Used: 176 << 30, Total: 200 << 30, Percent: 88},
			{Path: "/var/log", Used: 81 * (1 << 30) / 10, Total: 50 << 30, Percent: 16},
			{Path: "/run", Used: 3 << 20, Total: 23 << 30, Percent: 1, Virtual: true},
			{Path: "/dev/shm", Used: 9 << 20, Total: 23 << 30, Percent: 1, Virtual: true},
		},
		Networks: []remotestats.Network{{Name: "eth0", ReceivePerSecond: 204288, SendPerSecond: 159130, HasRate: true}, {Name: "docker0", ReceivePerSecond: 1200, SendPerSecond: 850, HasRate: true}},
	}
	p.haveDetails = true
	for i := range 61 {
		cpu := 28.4 + float64((i*7)%13-6)
		mem := 53 + float64((i*3)%7-3)
		state.sampleMonitor(s.stats, remotestats.Sample{CPUPercent: cpu, MemoryUsedBytes: uint64(mem / 100 * float64(462*(1<<30)/10)), MemoryTotalBytes: 462 * (1 << 30) / 10, ReceiveBytesPerSecond: 204288 + uint64(i%5)*12000, SendBytesPerSecond: 159130 + uint64(i%7)*8000, UptimeSeconds: 69242400, NetworkInterface: "eth0"})
	}
	for i := range 61 {
		p.x[i] = float32(i)
	}
	state.sampleMonitor(s.stats, remotestats.Sample{CPUPercent: 28.4, MemoryUsedBytes: 2443 * (1 << 30) / 100, MemoryTotalBytes: 462 * (1 << 30) / 10, ReceiveBytesPerSecond: 204288, SendBytesPerSecond: 159130, UptimeSeconds: 69242400, NetworkInterface: "eth0"})
	for i := range 61 {
		p.x[i] = float32(i)
	}
	p.sample.Cores = []remotestats.Core{{ID: "0", Percent: 10.8}, {ID: "1", Percent: 23.9}, {ID: "2", Percent: 24.4}, {ID: "3", Percent: 8.7}, {ID: "4", Percent: 15.3}, {ID: "5", Percent: 13.6}, {ID: "6", Percent: 4.1}, {ID: "7", Percent: 11.5}}
	p.data.CPUCount = 8
	state.formatMonitor(s.stats)
	c, _ := state.deck.Context(state.deck.ActiveWorkspace())
	c.Kind, c.Open, c.Width, c.MonitorOpen = layoutcore.ContextSFTP, true, 340, true
	_ = state.deck.SetContext(state.deck.ActiveWorkspace(), c)
	return state, s
}

func TestMonitorUsesTheWindowsSamplerWithoutProcfsCommands(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session := state.contextSession()
	session.host.System = sshconn.SystemWindows
	fake.streamCommand = func(ctx context.Context, command string, limit int, line func([]byte) bool) error {
		if !strings.HasPrefix(command, "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand ") {
			t.Error("Windows server received a non-PowerShell sampler")
		}
		return streamReadings(`gop-stats-win-v1 {"cpu":25,"total":8589934592,"free":4294967296,"uptime":100,"networkAvailable":false,"cores":[{"id":"0","percent":25}],"meta":{"count":1,"kernel":"Windows","disks":[{"path":"C:\\","total":1000,"free":400}]}}`)(ctx, command, limit, line)
	}
	state.Click(hitRemoteStats)
	waitRemoteStats(t, state, func(v *remoteStatsView) bool { return v.state == remoteStatsReady })
	if session.stats.panel.worker != nil || !session.stats.panel.haveDetails || !session.stats.panel.data.LoadUnavailable || session.stats.panel.cpuText != "25.0%" {
		t.Fatal("Windows monitor started Unix details polling or lost CIM metadata")
	}
	if session.stats.panel.downText != "—" {
		t.Fatal("unavailable network counter was shown as zero")
	}
}

// A Mac server gets its own commands, not the Linux procfs script that would
// only tell it "unsupported".
func TestMonitorUsesTheMacOSSamplerForADarwinServer(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session := state.contextSession()
	session.host.System = sshconn.SystemDarwin
	fake.streamCommand = func(ctx context.Context, command string, limit int, line func([]byte) bool) error {
		if !strings.Contains(command, "# motty-stats-darwin-v1") {
			t.Errorf("a Mac received %q", command)
		}
		return streamReadings(
			"gop-stats-darwin-v1 1 70.00 1000 2000 16777216 4194304 100 en0\n",
			"gop-stats-darwin-v1 0 40.60 1000 2000 16777216 4194304 102 en0\n",
		)(ctx, command, limit, line)
	}
	fake.runCommand = func(_ context.Context, command string, _ int) ([]byte, error) {
		if !strings.Contains(command, "# motty-monitor-details-darwin-v1") {
			t.Errorf("a Mac received details command %q", command)
		}
		return []byte("info\t8\t0.5\t0.4\t0.3\t100\t24.5.0\ndisk\t1000\t400\t600\t40\t0\t/\n"), nil
	}
	state.Click(hitRemoteStats)
	waitRemoteStats(t, state, func(v *remoteStatsView) bool { return v.state == remoteStatsReady && v.panel.haveDetails })
	if got := session.stats.text; got != "CPU 41%  RAM 12.0/16.0G  ↓0B/s ↑0B/s" {
		t.Fatalf("readout = %q", got)
	}
	if session.stats.panel.data.CPUCount != 8 || len(session.stats.panel.data.Disks) != 1 {
		t.Fatalf("details = %+v", session.stats.panel.data)
	}
}

func TestMonitorUnsupportedSystemsDoNotRunLinuxScripts(t *testing.T) {
	// ESXi answers uname with VMkernel and carries a busybox userland, not a
	// distribution's: the Linux sampler's /proc reads have nothing to read there.
	for _, system := range []sshconn.RemoteSystem{sshconn.SystemFreeBSD, sshconn.SystemESXi} {
		state, _, fake, _ := liveSession(t)
		session := state.contextSession()
		session.host.System = system
		state.Click(hitRemoteStats)
		if session.stats.state != remoteStatsUnsupported || session.stats.worker != nil || session.stats.panel.worker != nil {
			t.Fatalf("unsupported %s started polling", system)
		}
		fake.mu.Lock()
		calls := len(fake.commandCalls)
		fake.mu.Unlock()
		if calls != 0 {
			t.Fatalf("unsupported server received %d resource commands", calls)
		}
	}
}

func BenchmarkPaintMonitor(b *testing.B) {
	state, _ := monitorFixture(b)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1280, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func TestMonitorKeepsFilesAndRestoresWidth(t *testing.T) {
	state, session := monitorFixture(t)
	state.Resize(gfx.Size{Width: 1600, Height: 900})
	state.resizeMonitor(436)
	before := session.files
	state.Click(hitMonitorClose)
	c, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if c.MonitorOpen || !c.Open || session.files != before || c.MonitorWidth != 436 {
		t.Fatalf("monitor disturbed files or width: %+v", c)
	}
	restored := newTestState(t, state.options)
	c, _ = restored.deck.Context(restored.deck.ActiveWorkspace())
	if c.MonitorWidth != 436 {
		t.Fatalf("width after restart=%v", c.MonitorWidth)
	}
}

func TestMonitorHistoryStaysBounded(t *testing.T) {
	state, s := monitorFixture(t)
	for range 300 {
		state.sampleMonitor(s.stats, remotestats.Sample{CPUPercent: 40, MemoryTotalBytes: 1 << 30})
	}
	if s.stats.panel.count != 61 {
		t.Fatal("history grows without bounds")
	}
	state.Click(hitMonitorClose)
	if state.monitorShown(state.deck.ActiveWorkspace()) {
		t.Fatal("file tab did not hide the monitor")
	}
	if r, _ := state.scrollMonitor(gfx.ScrollEvent{X: 5, Y: 70, DeltaY: 100}); r.Handled {
		t.Fatal("hidden monitor stole file scrolling")
	}
}

func TestMonitorIndependentPanelGeometryPreservesTerminal(t *testing.T) {
	for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
		for _, width := range []float32{800, 1000, 1280, 1600} {
			t.Run(fmt.Sprint(side, "/", width), func(t *testing.T) {
				state, _ := monitorFixture(t)
				id := state.deck.ActiveWorkspace()
				c, _ := state.deck.Context(id)
				c.Side = side
				_ = state.deck.SetContext(id, c)
				size := gfx.Size{Width: width, Height: 900}
				state.Paint(&gfx.Canvas{}, size)
				b := state.workspacePixelBounds(size)
				if b.Width < minPaneWidth || b.X < 0 || b.X+b.Width > width {
					t.Fatalf("terminal geometry=%+v", b)
				}
				if !state.monitorDrawer(size) && state.monitorRect.Width > 360 {
					t.Fatal("monitor became a wide dashboard")
				}
			})
		}
	}
}

func TestMonitorPauseStopsBothReaders(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	fake.streamCommand = streamReadings(statsReadings...)
	state.Click(hitRemoteStats)
	waitRemoteStats(t, state, func(v *remoteStatsView) bool { return v.state == remoteStatsReady })
	s := state.contextSession()
	fast, details := s.stats.worker, s.stats.panel.worker
	state.Click(hitMonitorPause)
	if !s.stats.panel.paused || s.stats.worker != nil || s.stats.panel.worker != nil {
		t.Fatal("pause kept polling")
	}
	for _, done := range []<-chan struct{}{fast.Done(), details.Done()} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("paused worker did not exit")
		}
	}
}

func TestMonitorHistoryGapsAndInterfaceChanges(t *testing.T) {
	p := monitorView{interval: 2, sample: remotestats.Sample{CPUPercent: 32, MemoryUsedBytes: 1, MemoryTotalBytes: 4, NetworkInterface: "eth0", ReceiveBytesPerSecond: 1024}, diskAvailable: true, diskValue: 71}
	p.appendHistory(0)
	p.appendHistory(2)
	p.appendHistory(30)
	if p.count != 4 || !math.IsNaN(float64(p.cpu[2])) || p.cpu[3] != 32 || p.disk[3] != 71 || p.memory[3] != 25 {
		t.Fatalf("missing pause gap: %+v", p.cpu[:p.count])
	}
	p.sample.NetworkInterface = "eth1"
	p.appendHistory(32)
	if !math.IsNaN(float64(p.down[0])) || p.down[p.count-1] != 1 || p.cpu[0] != 32 {
		t.Fatal("interface change mixed traffic histories or cleared CPU")
	}
	p.sample.CPUUnavailable, p.sample.NetworkUnavailable = true, true
	p.sample.MemoryTotalBytes = 0
	p.detailsFailed = true
	p.appendHistory(34)
	for _, a := range [...]*[61]float32{&p.cpu, &p.memory, &p.disk, &p.down, &p.up} {
		if !math.IsNaN(float64(a[p.count-1])) {
			t.Fatal("missing observation became a zero")
		}
	}
}

func TestMonitorResizeDoesNotChangeFilePresentation(t *testing.T) {
	state, _ := monitorFixture(t)
	size := gfx.Size{Width: 1800, Height: 900}
	state.Resize(size)
	id := state.deck.ActiveWorkspace()
	before, _ := state.deck.Context(id)
	state.Paint(&gfx.Canvas{}, size)
	b := state.monitorRect
	state.monitorPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitMonitorDivider, Button: gfx.PointerButtonPrimary, X: b.X - 1, Y: 200})
	state.monitorPointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: b.X - 1 - 120, Y: 200})
	state.Paint(&gfx.Canvas{}, size)
	if state.monitorRect.Width != 440 {
		t.Fatalf("drag width=%v", state.monitorRect.Width)
	}
	state.monitorPointer(gfx.PointerEvent{Kind: gfx.PointerUp, X: state.monitorRect.X - 1, Y: 200})
	after, _ := state.deck.Context(id)
	if after.Side != before.Side || after.Width != before.Width || !after.Open || state.monitorResizing {
		t.Fatalf("resize affected files: %+v", after)
	}
	state.monitorKey(gfx.KeyEvent{Target: hitMonitorDivider, Key: gfx.KeyHome})
	state.Paint(&gfx.Canvas{}, size)
	if state.monitorRect.Width != layoutcore.MinMonitorWidth {
		t.Fatal("keyboard minimum width failed")
	}
	state.monitorKey(gfx.KeyEvent{Target: hitMonitorDivider, Key: gfx.KeyEnd})
	state.Paint(&gfx.Canvas{}, size)
	if state.monitorRect.Width != layoutcore.MaxMonitorWidth {
		t.Fatal("keyboard maximum width failed")
	}
}

func TestMonitorDividerKeysFollowTheDockedSide(t *testing.T) {
	for _, left := range []bool{false, true} {
		state, _ := monitorFixture(t)
		size := gfx.Size{Width: 1800, Height: 900}
		state.Resize(size)
		id := state.deck.ActiveWorkspace()
		context, _ := state.deck.Context(id)
		context.MonitorOpen, context.MonitorWidth, context.MonitorLeft = true, 400, left
		if err := state.deck.SetContext(id, context); err != nil {
			t.Fatal(err)
		}
		state.Paint(&gfx.Canvas{}, size)
		before := state.monitorRect.Width
		state.monitorKey(gfx.KeyEvent{Target: hitMonitorDivider, Key: gfx.KeyLeft})
		state.Paint(&gfx.Canvas{}, size)
		after := state.monitorRect.Width
		// Left always moves the divider left: a right-docked monitor widens and
		// a left-docked one narrows, so the key follows the pointer's direction.
		if left == (after >= before) {
			t.Fatalf("left dock %v: Left key width %v -> %v", left, before, after)
		}
	}
}

func TestMonitorRefreshAnimationPausesAndRespectsReducedMotion(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		for _, paused := range []bool{false, true} {
			state, session := monitorFixture(t)
			p := &session.stats.panel
			p.paused = paused
			var canvas gfx.Canvas
			canvas.SetReduceMotion(reduced)
			canvas.SetClock(func() time.Duration { return time.Second })
			canvas.BeginFrame()
			state.paintMonitorRefresh(&canvas, p, gfx.Rect{Width: 82, Height: 26}, true)
			canvas.EndFrame()
			delay, scheduled := canvas.NextFrame()
			if reduced || paused {
				if scheduled {
					t.Fatalf("idle animation keeps scheduling: reduced=%v paused=%v", reduced, paused)
				}
			} else if !scheduled || delay <= 0 || delay > time.Duration(p.interval)*time.Second/monitorClockSteps {
				t.Fatalf("clock cadence=%v/%v", delay, scheduled)
			}
			if len(canvas.Hits()) != 1 || !canvas.Hits()[0].Expandable {
				t.Fatal("refresh has duplicate or inaccessible hit targets")
			}
		}
	}
}

func TestMonitorTransientControlsDoNotFollowAnotherConnection(t *testing.T) {
	state, session := monitorFixture(t)
	state.Resize(gfx.Size{Width: 1600, Height: 900})
	state.Paint(&gfx.Canvas{}, state.viewport)
	state.monitorPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitMonitorDivider, Button: gfx.PointerButtonPrimary, X: state.monitorRect.X, Y: 100})
	state.Click(hitMonitorInterval)
	if !state.monitorMenu || !state.monitorResizing {
		t.Fatal("controls did not start")
	}
	c, _ := state.deck.Context(state.deck.ActiveWorkspace())
	beforeInterval := session.stats.panel.interval
	state.activeSessionID++ // equivalent to retargeting a split pane during a drag
	state.monitorPointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: 100, Y: 100})
	if state.monitorResizing {
		t.Fatal("divider drag followed another connection")
	}
	after, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if after.MonitorWidth != c.MonitorWidth {
		t.Fatal("stale drag changed the next pane")
	}
	state.monitorKey(gfx.KeyEvent{Key: gfx.KeyDown, Target: hitMonitorInterval})
	if state.monitorMenu {
		t.Fatal("refresh popup followed another connection")
	}
	state.activeSessionID = session.ID
	if session.stats.panel.interval != beforeInterval {
		t.Fatal("stale refresh selection changed a connection")
	}
}

func TestMonitorFailureStopsTheCountdownFrames(t *testing.T) {
	state, session := monitorFixture(t)
	var canvas gfx.Canvas
	canvas.BeginFrame()
	state.paintMonitorRefresh(&canvas, &session.stats.panel, gfx.Rect{Width: 82, Height: 26}, false)
	canvas.EndFrame()
	if _, scheduled := canvas.NextFrame(); scheduled {
		t.Fatal("failed sampling keeps animating")
	}
}

func TestMonitorStartsForAQuietWorkspaceOnActivation(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	fake.streamCommand = streamReadings(statsReadings...)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	id := state.deck.ActiveWorkspace()
	c, _ := state.deck.Context(id)
	c.MonitorOpen = true
	if err := state.deck.SetContext(id, c); err != nil {
		t.Fatal(err)
	}
	state.activeSection = sectionHosts
	state.activateWorkspace(id)
	session := state.contextSession()
	if session == nil || session.stats == nil || session.stats.worker == nil {
		t.Fatal("quiet workspace waits for unrelated terminal output to start its monitor")
	}
	state.closeMonitor()
}

// macOS has no per-core source, so its panel leaves the section out rather
// than showing an empty one, and the content height drops with it.
func TestMonitorLeavesOutTheCoresSectionWhereThereIsNoSource(t *testing.T) {
	var p monitorView
	with := monitorContentHeight(&p)
	p.noPerCore = true
	if without := monitorContentHeight(&p); with-without != monitorCoresHeight(&p)+monitorGap {
		t.Fatalf("content height %v with the section, %v without", with, without)
	}
}

func TestMonitorClickClosesAIWhenSpaceInsufficient(t *testing.T) {
	state, _ := monitorFixture(t)
	size := gfx.Size{Width: 1200, Height: 800}
	state.viewport = size
	state.setAIPanelOpen(true)
	if !state.aiPanelVisible() {
		t.Fatal("AI panel did not open")
	}

	state.Click(hitRemoteStats)

	id := state.deck.ActiveWorkspace()
	if !state.monitorShown(id) {
		t.Fatal("monitor did not open on hitRemoteStats click")
	}
	if state.aiPanelVisible() {
		t.Fatal("AI panel should close when monitor opens and space is insufficient for both")
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	term := state.workspacePixelBounds(size)
	if term.Width < minPaneWidth {
		t.Fatalf("terminal width too small: %v", term.Width)
	}
}

func TestAIClickClosesMonitorWhenSpaceInsufficient(t *testing.T) {
	state, _ := monitorFixture(t)
	size := gfx.Size{Width: 1200, Height: 800}
	state.viewport = size
	state.showMonitor()
	id := state.deck.ActiveWorkspace()
	if !state.monitorShown(id) {
		t.Fatal("monitor did not open")
	}

	state.toggleAIPanel()

	if !state.aiPanelVisible() {
		t.Fatal("AI panel did not open on toggle")
	}
	if state.monitorShown(id) {
		t.Fatal("monitor should close when AI opens and space is insufficient for both")
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	term := state.workspacePixelBounds(size)
	if term.Width < minPaneWidth {
		t.Fatalf("terminal width too small: %v", term.Width)
	}
}

func TestMonitorAndAICoexistOnWideScreen(t *testing.T) {
	state, _ := monitorFixture(t)
	size := gfx.Size{Width: 2000, Height: 900}
	state.viewport = size
	state.closeMonitor()
	state.setAIPanelOpen(true)
	if !state.aiPanelVisible() {
		t.Fatal("AI panel did not open")
	}

	state.Click(hitRemoteStats)

	id := state.deck.ActiveWorkspace()
	if !state.monitorShown(id) {
		t.Fatal("monitor did not open")
	}
	if !state.aiPanelVisible() {
		t.Fatal("AI panel should remain open alongside monitor on wide screens")
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	term := state.workspacePixelBounds(size)
	if term.Width < minPaneWidth {
		t.Fatalf("terminal width too small: %v", term.Width)
	}
	if state.aiReserve(id, size) <= 0 {
		t.Fatal("AI panel should be docked on wide screen")
	}
	if state.independentMonitorReserve(id, size) <= 0 {
		t.Fatal("monitor should be docked on wide screen")
	}
}

func TestNarrowingTheWindowClosesTheAssistantAndKeepsTheMonitorDrawer(t *testing.T) {
	state, _ := monitorFixture(t)
	view, err := headless.Open(app.Config{
		Title: "Panels", Width: 2000, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	state.Resize(gfx.Size{Width: 2000, Height: 900})
	state.setAIPanelOpen(true)
	id := state.deck.ActiveWorkspace()
	if !state.aiPanelVisible() || !state.monitorShown(id) {
		t.Fatal("both panels should be open on the wide window")
	}
	if state.monitorDrawer(state.viewport) || state.aiReserve(id, state.viewport) <= 0 {
		t.Fatal("both panels should dock on the wide window")
	}

	if err := view.Resize(760, 700); err != nil {
		t.Fatal(err)
	}
	if state.aiPanelVisible() {
		t.Fatal("the assistant should close when the narrow window cannot dock both panels")
	}
	if !state.monitorShown(id) || !state.monitorDrawer(state.viewport) {
		t.Fatal("the monitor should be the single drawer left after the resize")
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	snapshot := view.Snapshot()
	if _, ok := hitByIDInSnapshot(snapshot, hitAIDrawerDismiss); ok {
		t.Fatal("the closed assistant still painted its drawer")
	}
	if _, ok := hitByIDInSnapshot(snapshot, hitAIClose); ok {
		t.Fatal("the closed assistant still painted its controls")
	}
	if _, ok := hitByIDInSnapshot(snapshot, hitMonitorClose); !ok {
		t.Fatal("the monitor drawer is missing")
	}
}

func TestMonitorRemembersResizedWidthAcrossWorkspacesAndRestores(t *testing.T) {
	state, _ := monitorFixture(t)
	state.Paint(&gfx.Canvas{}, state.viewport)
	state.resizeMonitor(450)
	state.save()

	if state.monitorWidth != 450 {
		t.Fatalf("state.monitorWidth = %v, want 450", state.monitorWidth)
	}

	// Toggling monitor closed and open again in the same workspace remembers the width.
	state.closeMonitor()
	state.showMonitor()
	c, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if c.MonitorWidth != 450 {
		t.Fatalf("reopened monitor width = %v, want 450", c.MonitorWidth)
	}

	// Restoring the app remembers the adjusted width.
	restored := newTestState(t, state.options)
	if restored.monitorWidth != 450 {
		t.Fatalf("restored.monitorWidth = %v, want 450", restored.monitorWidth)
	}
}
