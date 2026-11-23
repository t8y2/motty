//go:build darwin || windows || linux

package workbench

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/remotestats"
	"motty/internal/workbench/icons"
)

func dashboardGPUFixture(t *testing.T) *State {
	s := dashboardNativeFixture(t)
	p := s.dashboardPanel()
	p.Hosts = p.Hosts[:2]
	s.hosts = s.hosts[:2]
	s.dashboard.engine.Set(*p, true)
	s.hosts[0].Label = "gpu-train-01"
	s.hosts[1].Label = "api-prod-02"
	paths := []string{"/", "/data/models", "/data/checkpoints", "/var/lib/docker", "/backup", "/logs", "/mnt/cache", "/mnt/archive"}
	disks := make([]remotestats.Disk, len(paths))
	for i, path := range paths {
		percent := []float64{61, 71, 87, 68, 56, 34, 42, 25}[i]
		total := uint64(1 << 40)
		used := uint64(float64(total) * percent / 100)
		disks[i] = remotestats.Disk{Path: path, Percent: percent, Total: total, Used: used, Available: total - used}
	}
	s.dashboard.engine.Details(p.Hosts[0], remotestats.Details{CPUCount: 16, Disks: disks}, false)
	s.dashboard.engine.Details(p.Hosts[1], remotestats.Details{CPUCount: 16, Disks: disks[:3]}, false)
	for n := 0; n <= 720; n++ {
		at := time.Date(2026, 11, 23, 10, 30, 0, 0, time.FixedZone("CST", 8*3600)).Add(time.Duration(n) * 5 * time.Second)
		phase := float64(n - 720)
		wave := math.Sin(phase*.17)*3 + math.Sin(phase*.049)*2
		sample := remotestats.Sample{
			CPUPercent: 27 + wave, MemoryTotalBytes: 64 << 30, MemoryUsedBytes: uint64((31 + wave/8) * (1 << 30)),
			ReceiveBytesPerSecond: uint64((100 + wave*6) * (1 << 10)), SendBytesPerSecond: uint64((47 + wave*2) * (1 << 10)),
			GPUs: []remotestats.GPU{
				{UUID: "gpu-a", Vendor: "NVIDIA", Index: 0, Name: "NVIDIA RTX 4090", UtilPercent: 74 + wave, MemoryUsedBytes: uint64((18 + wave/8) * (1 << 30)), MemoryTotalBytes: 24 << 30, TemperatureC: 67, PowerWatts: 285, PowerLimitWatts: 450, FanPercent: 62},
				{UUID: "gpu-b", Vendor: "NVIDIA", Index: 1, Name: "NVIDIA RTX 4090", UtilPercent: 31 + wave, MemoryUsedBytes: 8 << 30, MemoryTotalBytes: 24 << 30, TemperatureC: 54, PowerWatts: 132, PowerLimitWatts: 450, FanPercent: 45},
			},
		}
		s.dashboard.engine.Publish(p.Hosts[0], at, sample)
		sample.CPUPercent, sample.MemoryUsedBytes, sample.GPUs = 35+wave, uint64((29+wave/8)*(1<<30)), nil
		sample.ReceiveBytesPerSecond, sample.SendBytesPerSecond = uint64((75+wave*6)*(1<<10)), uint64((41+wave*2)*(1<<10))
		s.dashboard.engine.Publish(p.Hosts[1], at, sample)
	}
	return s
}

func TestDashboardDisplayReconfiguresCollectorWithoutReconnecting(t *testing.T) {
	s, conn, dialed := dashboardTestState(t)
	commands := make(chan string, 10)
	cancelled := make(chan struct{}, 10)
	readings := streamReadings(statsReadings...)
	conn.streamCommand = func(ctx context.Context, command string, limit int, line func([]byte) bool) error {
		commands <- command
		defer func() { cancelled <- struct{}{} }()
		return readings(ctx, command, limit, line)
	}
	createDashboard(t, s, "Demand")
	s.Click(hitDashboardToggle)
	waitDashboard(t, s, s.dashboard.active)
	nextCommand := func() string {
		t.Helper()
		select {
		case command := <-commands:
			return command
		case <-time.After(time.Second):
			t.Fatal("collector did not start")
			return ""
		}
	}
	if !strings.Contains(nextCommand(), "nvidia-smi") {
		t.Fatal("default selection stopped collecting GPU")
	}
	p := s.dashboardPanel()
	// A failed optional probe must not leave the revised collector blocked.
	s.dashboard.sources["host"].blocked = true
	s.toggleDashboardMetric(p, "host", "gpu")
	if script := nextCommand(); strings.Contains(script, "nvidia-smi") || strings.Contains(script, "rocm-smi") || strings.Contains(script, "/sys/class/drm/") {
		t.Fatal("hidden GPU still has a remote probe")
	}
	if *dialed != 1 || conn.canceled != 0 {
		t.Fatal("changing metrics reconnected or cancelled the host connection")
	}
	for _, metric := range dashboardMetricNames {
		if p.Shows("host", metric) {
			s.toggleDashboardMetric(p, "host", metric)
		}
	}
	if len(s.dashboard.sources) != 0 || len(s.dashboard.engine.Wanted()) != 0 {
		t.Fatal("empty selection still has an active collector")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("disabled collector was not cancelled")
	}
}

func TestDashboardGPUMissingReadingsAndResponsiveLayout(t *testing.T) {
	s := dashboardGPUFixture(t)
	p := s.dashboardPanel()
	view, err := headless.Open(s.WindowConfig(app.Config{Title: "Motty", Width: 1600, Height: 1100, Tree: Tree(), Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key, OnShortcut: s.Shortcut, OnScroll: s.Scroll, OnResize: s.Resize}), nil, headless.Options{Scale: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	refresh := func(w int) {
		t.Helper()
		if err := view.Resize(w, 1100); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	r := s.dashboardRow(p, p.Hosts[0], int(s.dashboard.body.Width))
	r.page, r.gpuKey = 3, "gpu-b"
	latest := r.snapshot.Sample
	at := r.snapshot.Updated
	missing := latest
	missing.GPUs = nil
	s.dashboard.engine.Publish(p.Hosts[0], at.Add(5*time.Second), missing)
	refresh(1600)
	if _, present := r.gpu(); present || r.gpuKey != "gpu-b" || !view.HasText(s.messages.Monitor.GPUMetricsUnavailable) {
		t.Fatal("missing GPU reading became a fabricated zero or lost the selected device")
	}
	// A later read may enumerate cards in a different order.
	latest.GPUs = []remotestats.GPU{latest.GPUs[1], latest.GPUs[0]}
	s.dashboard.engine.Publish(p.Hosts[0], at.Add(10*time.Second), latest)
	refresh(1600)
	if g, present := r.gpu(); !present || g.UUID != "gpu-b" || g.UtilPercent != 31 {
		t.Fatal("GPU recovery switched physical devices")
	}
	r.page = 0
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		s.setLanguage(language)
		for _, viewport := range []struct{ width, disks int }{{800, 1}, {1024, 2}, {1600, 3}, {1920, 4}} {
			refresh(viewport.width)
			if cols := dashboardDiskColumns(s.dashboard.body.Width, len(r.disks)); cols != viewport.disks {
				t.Fatalf("%dpx: got %d disk columns, want %d", viewport.width, cols, viewport.disks)
			}
			var display, terminal gfx.Rect
			for _, hit := range view.Snapshot().Hits {
				switch hit.ID {
				case hitDashboardDisplay.At(int(s.hosts[0].ID)):
					display = hit.Bounds
				case hitDashboardTerminalList.At(int(s.hosts[0].ID)):
					terminal = hit.Bounds
				}
			}
			if display.Width != 32 || terminal.Width == 0 || display.X+display.Width > terminal.X || terminal.X+terminal.Width > float32(viewport.width) {
				t.Fatalf("%s/%dpx: display settings clipped or overlapped terminal: %v / %v", language, viewport.width, display, terminal)
			}
			if dir := os.Getenv("MOTTY_GPU_SCREENSHOTS"); dir != "" && language == i18n.English && viewport.width == 800 {
				if err := view.Screenshot(filepath.Join(dir, "narrow-english.png")); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// Removed hosts retain their history but have no actionable host identity.
	s.hosts = nil
	refresh(1600)
	for _, hit := range view.Snapshot().Hits {
		if hit.ID.Scope() == hitDashboardDisplay || hit.ID.Scope() == hitDashboardGPUSelect {
			t.Fatal("removed hosts share an actionable zero-ID display/GPU control")
		}
	}
}

func TestDashboardGPUViewsAndDisplayPreferences(t *testing.T) {
	s := dashboardGPUFixture(t)
	p := s.dashboardPanel()
	view, err := headless.Open(s.WindowConfig(app.Config{Title: "Motty", Width: 1600, Height: 1100, Tree: Tree(), Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key, OnShortcut: s.Shortcut, OnScroll: s.Scroll, OnResize: s.Resize}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	rest := func() {
		t.Helper()
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	shot := func(name string) {
		t.Helper()
		rest()
		if dir := os.Getenv("MOTTY_GPU_SCREENSHOTS"); dir != "" {
			if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil {
				t.Fatal(err)
			}
		}
	}
	first := s.dashboardRow(p, p.Hosts[0], int(s.dashboard.body.Width))
	if len(first.snapshot.GPUs) != 2 || first.graphCount != 4 {
		t.Fatal("collector's GPU readings did not reach dashboard")
	}
	if options := s.dashboardViewOptions(s.hosts[0].ID, false); options[1].Label != "GPU" || !reflect.DeepEqual(options[1].Icon, icons.Cpu) {
		t.Fatal("GPU view/icon absent")
	}
	if options := s.dashboardViewOptions(s.hosts[1].ID, false); !options[3].Disabled {
		t.Fatal("GPU-free host acquired a GPU view")
	}
	shot("detailed-overview")
	if control, ok := view.Hit(s.messages.Monitor.GPU); !ok || control.Role != gfx.RoleCombobox {
		t.Fatal("GPU selector is not reachable")
	}
	if err := view.ClickLabel(s.messages.Monitor.GPU); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel("GPU1"); err != nil {
		t.Fatal(err)
	}
	rest()
	if first.gpuKey != "gpu-b" {
		t.Fatal("native device selector did not change the chart")
	}
	if err := view.ClickLabel(s.messages.Monitor.GPU); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel("GPU0"); err != nil {
		t.Fatal(err)
	}
	rest()
	// Selection reconciles collection without rewriting already retained history.
	before, _ := s.dashboard.engine.Read(p.ID, p.Hosts[0], 100, 0)
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == hitDashboardDisplay.At(int(s.hosts[0].ID)) {
			if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	rest()
	if !first.infoOpen {
		t.Fatal("gear did not open the display menu")
	}
	shot("display-menu")
	if err := view.ClickLabel(s.messages.Dashboard.Network); err != nil {
		t.Fatal(err)
	}
	rest()
	if p.Shows(p.Hosts[0], "network") || !p.Shows(p.Hosts[1], "network") {
		t.Fatal("native menu failed to isolate the host's choice")
	}
	after, _ := s.dashboard.engine.Read(p.ID, p.Hosts[0], 100, 0)
	if before.Version == after.Version || before.Updated != after.Updated || len(before.Points) != len(after.Points) {
		t.Fatal("selection did not reconcile collection or discarded retained history")
	}
	s.toggleDashboardMetric(p, p.Hosts[0], "network")
	s.Click(hitDashboardGPUView.At(int(s.hosts[0].ID) * 2))
	if err := view.Resize(1600, 1100); err != nil {
		t.Fatal(err)
	}
	shot("detailed-gpu")
	s.Click(hitDashboardGPUSelect.At(int(s.hosts[0].ID)*remotestats.MaxGPUs + 1))
	rest()
	if first.gpuKey != "gpu-b" || first.gpuValues[0][len(first.gpuValues[0])-1] != 31 {
		t.Fatal("device selection mixed GPU histories")
	}
	first.gpuKey = "gpu-a"
	first.gpuPrepared = ""
	if err := view.ClickLabel(s.messages.Dashboard.Simple); err != nil {
		t.Fatal(err)
	}
	rest()
	gear, ok := view.Hit(s.messages.Dashboard.Display)
	if !ok {
		t.Fatal("simple gear absent")
	}
	terminal, ok := view.Hit(s.messages.Dashboard.Terminal)
	if !ok || gear.Bounds.X != terminal.Bounds.X || gear.Bounds.Y >= terminal.Bounds.Y {
		t.Fatal("gear is not above terminal")
	}
	shot("simple-overview")
	if !view.HasText("/ · 8 个挂载点") {
		t.Fatal("simple disk summary lost the mount identity and count")
	}
	s.Click(hitDashboardGPUView.At(int(s.hosts[0].ID)*2 + 1))
	if err := view.Resize(1600, 1100); err != nil {
		t.Fatal(err)
	}
	shot("simple-gpu")
	if !view.HasText("285 / 450 W") || !view.HasText("132 / 450 W") {
		t.Fatal("simple GPU table omitted physical devices")
	}
	s.toggleDashboardMetric(p, p.Hosts[0], "network")
	saved := s.snapshot()
	path := filepath.Join(t.TempDir(), "display-preferences.db")
	if err := saveState(path, saved); err != nil {
		t.Fatal(err)
	}
	saved, _, err = loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	restored := newTestState(t, testOptions(t))
	restored.restoreLocal(saved)
	if restored.dashboardPanel() == nil || !reflect.DeepEqual(restored.dashboardPanel().Display, p.Display) {
		t.Fatal("display preferences were not restored")
	}
	restoredDisplay := restored.dashboardPanel().Clone().Display
	for _, metric := range dashboardMetricNames {
		if p.Shows(p.Hosts[0], metric) {
			s.toggleDashboardMetric(p, p.Hosts[0], metric)
		}
	}
	first.simplePage = 0
	if err := view.Resize(1600, 1100); err != nil {
		t.Fatal(err)
	}
	rest()
	if !view.HasText(s.messages.Dashboard.NoDisplay) {
		t.Fatal("explicit empty overview reverted to defaults")
	}
	if !reflect.DeepEqual(restored.dashboardPanel().Display, restoredDisplay) || !reflect.DeepEqual(saved.MonitorPanels[0].Display, restoredDisplay) {
		t.Fatal("changing display preferences mutated a saved snapshot or another restored state")
	}
}
