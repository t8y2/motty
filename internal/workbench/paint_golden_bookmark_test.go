package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The SFTP window's bookmark dialog, as the window driver paints it. Text that names a
// temporary directory or a time is normalized, as it differs every run.
var tempNames = regexp.MustCompile(`Test[A-Za-z0-9]+[0-9]{6,}|/[^" ]*/(T|var/folders)[^" ]*|[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}`)

func TestPaintGoldenBookmarkDialog(t *testing.T) {
	pinPaintEnvironment(t)
	for _, scale := range []float64{1, 2} {
		name := fmt.Sprintf("bookmark-scale%v", scale)
		t.Run(name, func(t *testing.T) {
			_, w, _, host, localDir := dualSFTPReferenceFixture(t, scale)
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
			// The fixture's own folder differs on every run and names a
			// different shape of path on Windows; replacing it first leaves the
			// rest of the regex for the other temporary names.
			normalize := func(text string) string {
				// A folder title prints the folder's own name, not its path.
				if text == filepath.Base(localDir) {
					text = "LOCAL"
				}
				text = strings.ReplaceAll(text, localDir, "LOCAL")
				text = strings.ReplaceAll(text, filepath.ToSlash(localDir), "LOCAL")
				return tempNames.ReplaceAllString(text, "TMP")
			}
			snap := view.Snapshot()
			for _, p := range snap.Primitives {
				// The dialog moves the keyboard into its first field, and the
				// reference editor paints its caret as a transparent hairline
				// there; a platform editor draws the caret natively instead and
				// the snapshot never sees it. It paints nothing, so it is left
				// out rather than making the snapshot depend on the editor.
				if p.Color.Alpha == 0 && p.Bounds.Width <= 1 && p.Bounds.Height <= 24 {
					continue
				}
				if p.Bounds.Width > 0 && p.Bounds.Height > 0 {
					fmt.Fprintf(&b, "prim %v %v %v\n", p.Kind, p.Bounds, p.Color)
				}
			}
			for _, x := range snap.Texts {
				fmt.Fprintf(&b, "text %q %v\n", normalize(x.Value), x.Bounds)
			}
			for _, h := range snap.Hits {
				// The window buttons are the platform's chrome, drawn at the
				// strip's ends: macOS has none of its own, Linux and Windows do.
				// The strip's drag region stays; the dialog's page, not the
				// chrome, is what this snapshot records.
				if h.Role == gfx.RoleButton && h.Bounds.Y+h.Bounds.Height <= TitlebarHeight {
					continue
				}
				fmt.Fprintf(&b, "hit %q %v %v %v\n", normalize(h.Label), h.Role, h.Bounds, h.Disabled)
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
			if diff := firstDifference(string(want), got); diff != "" {
				t.Errorf("the bookmark dialog paints differently from its golden\n%s", diff)
			}
		})
	}
}
