package workbench

import (
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"motty/internal/sftpclient"
)

func TestSFTPTypeIconsLoadOneSystemIconPerExtension(t *testing.T) {
	state := newTestState(t, testOptions(t))
	answers := &iconAnswers{failed: map[string]error{}}
	state.options.FileIcon = answers.icon
	rows := prepareSFTPRows(nil, []sftpclient.Entry{
		{Name: "a.nc", Mode: 0o644},
		{Name: "b.nc", Mode: 0o644},
		{Name: "c.py", Mode: 0o644},
		{Name: "README", Mode: 0o644},
		{Name: ".bashrc", Mode: 0o644},
		{Name: "folder", Mode: os.ModeDir | 0o755},
	})
	state.loadTypeIcons(nil, rows)
	waitAppIcons(t, state, func() bool {
		return rows[0].typeIcon.ready() && rows[2].typeIcon.ready()
	})
	if rows[0].typeIcon != rows[1].typeIcon {
		t.Fatal("two files of the same extension got different icon slots")
	}
	if rows[0].typeIcon.bitmap.ID == 0 {
		t.Fatal("the type icon has no ID, so the renderer would upload it every frame")
	}
	for _, index := range []int{3, 4, 5} {
		if rows[index].typeIcon != nil {
			t.Fatalf("row %d (%q) was given a type icon", index, rows[index].entry.Name)
		}
	}
	answers.mu.Lock()
	asked := append([]string(nil), answers.asked...)
	answers.mu.Unlock()
	if len(asked) != 2 {
		t.Fatalf("system icon questions = %v, want one per extension", asked)
	}
	for _, name := range asked {
		if !strings.HasSuffix(name, ".nc") && !strings.HasSuffix(name, ".py") {
			t.Fatalf("unexpected system icon question %q", name)
		}
	}
}

func TestSFTPTypeIconsAreNotAskedForOnAPlatformWithoutThem(t *testing.T) {
	state := newTestState(t, testOptions(t))
	calls := 0
	state.options.FileIcon = func(string, int) (gfx.Bitmap, error) {
		calls++
		return gfx.Bitmap{}, app.ErrUnsupported
	}
	rows := prepareSFTPRows(nil, []sftpclient.Entry{
		{Name: "a.nc", Mode: 0o644},
		{Name: "b.py", Mode: 0o644},
	})
	state.loadTypeIcons(nil, rows)
	waitAppIcons(t, state, func() bool {
		return rows[0].typeIcon.failed && rows[1].typeIcon.failed
	})
	if calls != 1 {
		t.Fatalf("unsupported platform was asked %d times, want one answer for the group", calls)
	}
	state.loadTypeIcons(nil, rows)
	state.Wake()
	if calls != 1 {
		t.Fatalf("a failed extension was asked again: %d calls", calls)
	}
}
