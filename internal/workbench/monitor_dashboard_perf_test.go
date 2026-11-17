package workbench

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
)

func dashboardPerformanceFixture(t testing.TB, points int) *State {
	t.Helper()
	s := newTestState(t, testOptions(t))
	s.activeSection = sectionDashboard
	s.hosts = []Host{{ID: 1, CatalogID: "perf-host", Label: "perf-host", Address: "192.0.2.1", Port: 22, System: sshconn.SystemUbuntu}}
	s.syncVisible()
	p := monitoring.Panel{ID: "perf", Name: "Performance", Hosts: []string{"perf-host"}, Interval: 2, Retention: 60}
	s.dashboard.panels = []monitoring.Panel{p}
	s.dashboard.active = p.ID
	s.dashboard.running = map[string]bool{p.ID: true}
	s.dashboard.engine.Set(p, true)
	s.dashboard.engine.Details("perf-host", remotestats.Details{CPUCount: 4, Disks: dockerDiskFixture(64)}, false)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for n := range points {
		v := math.Sin(float64(n)*.031)*5 + math.Sin(float64(n)*.19)*2
		s.dashboard.engine.Publish("perf-host", start.Add(time.Duration(n)*2*time.Second), remotestats.Sample{CPUPercent: 20 + v, MemoryTotalBytes: 8 << 30, MemoryUsedBytes: 2 << 30, ReceiveBytesPerSecond: uint64(128*1024 + v*8000), SendBytesPerSecond: uint64(54*1024 + v*2000), NetworkInterface: "eth0"})
	}
	return s
}

func TestDashboardChartCacheKeepsRenderingAndAccessibilityCurrent(t *testing.T) {
	s := dashboardPerformanceFixture(t, 61)
	p := s.dashboardPanel()
	h := s.hosts[0]
	r := s.dashboardRow(p, h.CatalogID, 1400)
	bounds := gfx.Rect{X: 30, Y: 50, Width: 420, Height: 140}
	scale := float32(1)
	focus := gfx.ID{}
	paint := func(c *gfx.Canvas) {
		c.Reset()
		c.SetTree(&tree)
		c.SetPixelScale(scale)
		c.SetInteraction(gfx.ID{}, gfx.ID{}, focus, !focus.IsZero())
		c.Clip(gfx.Rect{X: 20, Y: 70, Width: 600, Height: 300})
		s.paintDashboardChart(c, p, h, r, bounds, 2, s.messages.Dashboard.Network)
		c.Unclip()
	}
	var cached gfx.Canvas
	paint(&cached)
	for _, change := range []struct {
		name  string
		apply func()
	}{
		{"unchanged", func() {}},
		{"scroll", func() { bounds.Y -= 20 }},
		{"resize", func() { bounds.Width -= 65 }},
		{"theme", func() { s.setAppearance(s.appearance.toggled()) }},
		{"language", func() { s.setLanguage(i18n.Chinese) }},
		{"scale", func() { scale = 1.5 }},
		{"focus", func() { focus = r.chartIDs[2] }},
		{"focused-language", func() { s.setLanguage(i18n.English) }},
		{"blur", func() { focus = gfx.ID{} }},
		{"sample", func() {
			s.dashboard.engine.Publish(h.CatalogID, r.snapshot.Updated.Add(2*time.Second), remotestats.Sample{CPUPercent: 90, MemoryTotalBytes: 8 << 30, MemoryUsedBytes: 5 << 30, ReceiveBytesPerSecond: 1 << 20, SendBytesPerSecond: 1 << 19, NetworkInterface: "eth0"})
			r = s.dashboardRow(p, h.CatalogID, 1400)
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			change.apply()
			paint(&cached)
			r.charts[2] = dashboardChart{}
			var fresh gfx.Canvas
			paint(&fresh)
			if !reflect.DeepEqual(cached.Triangles(), fresh.Triangles()) || !reflect.DeepEqual(cached.Rectangles(), fresh.Rectangles()) || !reflect.DeepEqual(cached.Texts(), fresh.Texts()) || !reflect.DeepEqual(cached.Hits(), fresh.Hits()) {
				t.Fatal("cached chart differs from a fresh chart")
			}
			if len(cached.Hits()) != 1 || cached.Hits()[0].ID != r.chartIDs[2] {
				t.Fatal("cached chart lost its hit identity")
			}
			if !focus.IsZero() && cached.Hits()[0].Value == "" {
				t.Fatal("focused chart lost its accessible sample description")
			}
		})
	}
}

// A host with dozens of mounts only paints the rows in view. Scrolling to the
// end must still reach the last one, and skipping rows must not change how tall
// the page is or how far it scrolls.
func TestDashboardPaintsOnlyVisibleMountsAndReachesTheLast(t *testing.T) {
	for _, view := range []struct {
		name  string
		width float32
		page  int
	}{{"overview-wide", 1800, 0}, {"overview-narrow", 700, 0}, {"device-disks", 1800, 1}} {
		width := view.width
		t.Run(view.name, func(t *testing.T) {
			s := dashboardPerformanceFixture(t, 61)
			s.dashboardRow(s.dashboardPanel(), "perf-host", int(width)).page = view.page
			size := gfx.Size{Width: width, Height: 1000}
			var c gfx.Canvas
			s.Paint(&c, size)
			content := s.dashboard.content
			if got := paintedDockerDisks(&c); got == 0 || got >= 64 || hasPaintedText(&c, "/last") {
				t.Fatalf("at the top painted %d of 64 mounts, last painted=%v", got, hasPaintedText(&c, "/last"))
			}

			s.dashboard.scroll = 1e6
			c.Reset()
			s.Paint(&c, size)
			if !hasPaintedText(&c, "/last") {
				t.Fatal("the last mount is not reachable by scrolling to the end")
			}
			if s.dashboard.content != content {
				t.Fatalf("content height changed with scrolling: %v then %v", content, s.dashboard.content)
			}
			if want := content - s.dashboard.body.Height; s.dashboard.scroll != want {
				t.Fatalf("scrolled to %v, want the end of the content at %v", s.dashboard.scroll, want)
			}
		})
	}
}

func BenchmarkDashboardPaint(b *testing.B) {
	for _, points := range []int{2, 1801} {
		b.Run(fmt.Sprint(points), func(b *testing.B) {
			s := dashboardPerformanceFixture(b, points)
			var c gfx.Canvas
			size := gfx.Size{Width: 1800, Height: 1000}
			s.Paint(&c, size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				c.Reset()
				s.Paint(&c, size)
			}
			b.ReportMetric(float64(len(c.Triangles())), "triangles")
		})
	}
}

func BenchmarkDashboardDriver(b *testing.B) {
	for _, scenario := range []struct {
		name   string
		points int
		update bool
	}{{"2", 2, false}, {"1801", 1801, false}, {"1801-update", 1801, true}} {
		b.Run(scenario.name, func(b *testing.B) {
			s := dashboardPerformanceFixture(b, scenario.points)
			var c gfx.Canvas
			driver, err := app.NewDriver(app.Config{Title: "Performance", Width: 1800, Height: 1000, Tree: Tree(), Paint: s.Paint}, &c)
			if err != nil {
				b.Fatal(err)
			}
			defer driver.Close()
			r := s.dashboardRow(s.dashboardPanel(), "perf-host", int(s.dashboard.body.Width))
			next, sample := r.snapshot.Updated, r.snapshot.Sample
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if scenario.update {
					next = next.Add(2 * time.Second)
					s.dashboard.engine.Publish("perf-host", next, sample)
				}
				if err := driver.Resize(1800, 1000); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(c.Triangles())), "triangles")
		})
	}
}
