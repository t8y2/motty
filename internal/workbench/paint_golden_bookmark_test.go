package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The SFTP window's bookmark dialog, as the window driver paints it. Text that names a
// temporary directory or a time is normalized, as it differs every run.
var tempNames = regexp.MustCompile(`Test[A-Za-z0-9]+[0-9]{6,}|/[^" ]*/(T|var/folders)[^" ]*|[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}`)

func TestPaintGoldenBookmarkDialog(t *testing.T) {
	for _, scale := range []float64{1, 2} {
		name := fmt.Sprintf("bookmark-scale%v", scale)
		t.Run(name, func(t *testing.T) {
			_, w, _, host, _ := dualSFTPFixture(t, scale)
			view := host.windows[0].view
			w.session.host.CatalogID = "native-bookmark"
			w.window.Invalidate()
			view.Driver().Pump()
			r := dualHit(t, view, hitSFTPLocationProfiles)
			if err := view.Click(r.X+r.Width/2, r.Y+r.Height/2); err != nil {
				t.Fatal(err)
			}
			if !w.bookmark.open {
				t.Fatal("the bookmark dialog did not open")
			}
			var b strings.Builder
			snap := view.Snapshot()
			for _, p := range snap.Primitives {
				if p.Bounds.Width > 0 && p.Bounds.Height > 0 {
					fmt.Fprintf(&b, "prim %v %v %v\n", p.Kind, p.Bounds, p.Color)
				}
			}
			for _, x := range snap.Texts {
				fmt.Fprintf(&b, "text %q %v\n", tempNames.ReplaceAllString(x.Value, "TMP"), x.Bounds)
			}
			for _, h := range snap.Hits {
				fmt.Fprintf(&b, "hit %q %v %v %v\n", tempNames.ReplaceAllString(h.Label, "TMP"), h.Role, h.Bounds, h.Disabled)
			}
			got := b.String()
			path := filepath.Join("testdata", "paint", name+".txt")
			if os.Getenv("UPDATE_PAINT_GOLDEN") != "" {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no golden: run with UPDATE_PAINT_GOLDEN=1")
			}
			if got != string(want) {
				t.Errorf("the bookmark dialog paints differently from its golden\n%s", firstDifference(string(want), got))
			}
		})
	}
}
