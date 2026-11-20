package workbench

import (
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
	"motty/internal/workbench/icons"
)

// A symbolic link is drawn as what it points at, not as the chain the link
// itself is: a directory of shortcuts still reads by kind.
func TestSymlinkIconsFollowTheTargetType(t *testing.T) {
	cases := []struct {
		name  string
		entry sftpclient.Entry
		icon  *gfx.Icon
		mono  bool
	}{
		{"folder", sftpclient.Entry{Name: "tomcat", Mode: os.ModeSymlink | 0o777, LinkTarget: "/opt/tomcat", LinkDir: true}, &icons.FolderSolid, false},
		{"target extension", sftpclient.Entry{Name: "notes", Mode: os.ModeSymlink | 0o777, LinkTarget: "/srv/notes.md"}, &icons.MitMarkdown, false},
		{"target has no type", sftpclient.Entry{Name: "notes.txt", Mode: os.ModeSymlink | 0o777, LinkTarget: "/srv/data"}, &icons.File, true},
		{"broken", sftpclient.Entry{Name: "notes.txt", Mode: os.ModeSymlink | 0o777, LinkTarget: "/srv/gone", LinkBroken: true}, &icons.Link, true},
		{"unresolved", sftpclient.Entry{Name: "notes.txt", Mode: os.ModeSymlink | 0o777}, &icons.Link, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			icon, mono := sftpFileIcon(tc.entry)
			if icon != tc.icon || mono != tc.mono {
				t.Fatalf("icon %p mono=%v, want %p mono=%v", icon, mono, tc.icon, tc.mono)
			}
		})
	}
}

// The system file icon follows the target's name too, so a link called "notes"
// to "readme.md" asks for the same icon as a plain readme.md.
func TestSymlinkTypeIconFollowsTheTargetName(t *testing.T) {
	link := sftpclient.Entry{Name: "notes", Mode: os.ModeSymlink | 0o777, LinkTarget: "/srv/readme.md"}
	if got := sftpTypeIconName(link); got != "readme.md" {
		t.Fatalf("type icon name %q, want readme.md", got)
	}
	target := sftpclient.Entry{Name: "readme.md", Mode: os.ModeSymlink | 0o777, LinkTarget: "/srv/data"}
	if sftpTypeIconName(target) != "data" {
		t.Fatalf("a target name without a type took the link's name: %q", sftpTypeIconName(target))
	}
	broken := sftpclient.Entry{Name: "readme.md", Mode: os.ModeSymlink | 0o777, LinkBroken: true}
	if sftpTypeIconName(broken) != "readme.md" {
		t.Fatalf("broken link asked for %q", sftpTypeIconName(broken))
	}
}

// The shortcut arrow belongs on a link whose target icon is drawn; the link
// glyph already marks a broken or unresolved one.
func TestShortcutArrowMarksResolvedLinksOnly(t *testing.T) {
	cases := []struct {
		name  string
		entry sftpclient.Entry
		want  bool
	}{
		{"folder", sftpclient.Entry{Name: "docs", Mode: os.ModeSymlink | 0o777, LinkTarget: "/srv/docs", LinkDir: true}, true},
		{"file", sftpclient.Entry{Name: "readme", Mode: os.ModeSymlink | 0o777, LinkTarget: "/srv/readme.md"}, true},
		{"broken", sftpclient.Entry{Name: "readme", Mode: os.ModeSymlink | 0o777, LinkBroken: true}, false},
		{"unresolved", sftpclient.Entry{Name: "readme", Mode: os.ModeSymlink | 0o777}, false},
		{"plain file", sftpclient.Entry{Name: "readme.md", Mode: 0o644}, false},
	}
	for _, tc := range cases {
		row := sftpRow{entry: tc.entry}
		if got := row.showsShortcut(); got != tc.want {
			t.Errorf("%s: showsShortcut=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// The list paints the shortcut mark on a link row only, inside its icon: the
// target's icon alone would no longer say that the row is a link.
func TestALinkRowPaintsTheShortcutMarkOverItsTargetIcon(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	remote.mu.Lock()
	remote.entries["/home/test"] = append(remote.entries["/home/test"],
		workbenchFileInfo{name: "link", mode: os.ModeSymlink | 0o777},
		workbenchFileInfo{name: "plain.txt", mode: 0o644})
	remote.links = map[string]string{"/home/test/link": "/home/test/docs"}
	remote.mu.Unlock()
	state.listSFTP(session.files, "/home/test")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == 4 })

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	rows := map[string]gfx.Rect{}
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleOption && (hit.Label == "link" || hit.Label == "plain.txt") {
			rows[hit.Label] = hit.Bounds
		}
	}
	if len(rows) != 2 {
		t.Fatalf("painted rows %v, want the link and the plain file", rows)
	}
	badges := func(row gfx.Rect) int {
		count := 0
		for _, rect := range canvas.Rectangles() {
			if rect.Bounds.Width == sftpShortcutSize && rect.Bounds.Height == sftpShortcutSize &&
				rect.Radius == 2 && rect.Color == state.pal.text &&
				rectContains(row, rect.Bounds) &&
				rect.Bounds.X+rect.Bounds.Width <= row.X+8+sftpColumnIconWidth {
				count++
			}
		}
		return count
	}
	if got := badges(rows["link"]); got != 1 {
		t.Fatalf("the link row paints %d shortcut marks, want 1", got)
	}
	if got := badges(rows["plain.txt"]); got != 0 {
		t.Fatalf("the plain file paints %d shortcut marks, want 0", got)
	}
}
