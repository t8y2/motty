//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/remotestats"
)

func dockerDiskFixture(count int) []remotestats.Disk {
	disks := make([]remotestats.Disk, count)
	for i := range disks {
		path := fmt.Sprintf("/var/lib/docker/volumes/%03d/_data", i)
		if i == count-1 {
			path = "/last"
		}
		disks[i] = remotestats.Disk{
			Path:    path,
			Used:    uint64(i+1) << 30,
			Total:   100 << 30,
			Percent: float64(i + 1),
		}
	}
	return disks
}

func paintedDockerDisks(canvas *ui.Canvas) int {
	count := 0
	for _, text := range canvas.Texts() {
		if strings.HasPrefix(text.Value, "/var/lib") {
			count++
		}
	}
	return count
}

func TestMonitorManyDockerMountsPaintOnlyVisibleRows(t *testing.T) {
	state, session := monitorFixture(t)
	p := &session.stats.panel
	p.page = 0
	p.data.Disks = dockerDiskFixture(64)
	state.formatMonitor(session.stats)

	size := ui.Size{Width: 1280, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if monitorDiskCount(p) != 3 {
		t.Fatalf("collapsed disk rows = %d, want 3", monitorDiskCount(p))
	}

	if !state.clickMonitor(hitMonitorDisks) || !p.disksOpen {
		t.Fatal("More did not expand the disk list")
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	topVisible := paintedDockerDisks(&canvas)
	if topVisible >= len(p.disks) {
		t.Fatalf("visible disk rows at the top = %d, want fewer than %d", topVisible, len(p.disks))
	}

	area := state.monitorScrollArea(p).Geometry()
	if area.MaxOffsetX != 0 || area.HorizontalTrack != (ui.Rect{}) || area.HorizontalThumb != (ui.Rect{}) {
		t.Fatalf("expanded disks added horizontal scrolling: %+v", area)
	}
	viewport := area.Viewport
	diskSectionTop := float32(8 + monitorTileHeight + monitorLowerTileHeight + monitorGap*3 +
		monitorSummaryHeight + monitorCoresHeight(p) + monitorGap)
	p.scroll = diskSectionTop
	canvas.Reset()
	state.Paint(&canvas, size)
	middleVisible := paintedDockerDisks(&canvas)
	if middleVisible == 0 || middleVisible >= len(p.disks) {
		t.Fatalf("visible disk rows after scrolling = %d, want a bounded subset of %d", middleVisible, len(p.disks))
	}

	p.scroll = max(p.contentHeight-viewport.Height, 0)
	canvas.Reset()
	state.Paint(&canvas, size)
	bottomVisible := paintedDockerDisks(&canvas)
	if bottomVisible == 0 || bottomVisible >= len(p.disks) {
		t.Fatalf("visible disk rows at the bottom = %d, want a bounded subset of %d", bottomVisible, len(p.disks))
	}
	if !hasPaintedText(&canvas, "/last") {
		t.Fatal("the final Docker mount is not reachable by scrolling")
	}
}

func hasPaintedText(canvas *ui.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return true
		}
	}
	return false
}

func BenchmarkPaintMonitorManyDisksExpanded(b *testing.B) {
	state, session := monitorFixture(b)
	p := &session.stats.panel
	p.page = 0
	p.disksOpen = true
	p.data.Disks = dockerDiskFixture(64)
	state.formatMonitor(session.stats)
	size := ui.Size{Width: 1280, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}
