package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// iconAnswers stands in for the system: it answers a bundle by name and counts
// what it was asked.
type iconAnswers struct {
	mu     sync.Mutex
	asked  []string
	failed map[string]error
}

func (answers *iconAnswers) icon(path string, pixels int) (gfx.Bitmap, error) {
	answers.mu.Lock()
	defer answers.mu.Unlock()
	answers.asked = append(answers.asked, filepath.Base(path))
	if err := answers.failed[filepath.Base(path)]; err != nil {
		return gfx.Bitmap{}, err
	}
	return gfx.Bitmap{Pix: make([]byte, pixels*pixels*4), Width: pixels, Height: pixels, Stride: pixels * 4}, nil
}

func (answers *iconAnswers) askedCount() int {
	answers.mu.Lock()
	defer answers.mu.Unlock()
	return len(answers.asked)
}

func appIconFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"Terminal.app", "Broken.app", "Chrome Apps.localized", "plain"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "notabundle.app"), []byte("a file"), 0o644)
	os.Symlink(filepath.Join(dir, "Terminal.app"), filepath.Join(dir, "Terminal link.app"))
	return dir
}

func waitAppIcons(t *testing.T, state *State, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the application icons did not arrive")
}

func localRow(t *testing.T, view *sftpView, name string) *sftpRow {
	t.Helper()
	for index := range view.lfolder.allRows {
		if view.lfolder.allRows[index].entry.Name == name {
			return &view.lfolder.allRows[index]
		}
	}
	t.Fatalf("no local row %q", name)
	return nil
}

// An application bundle in the file list is shown with the system's icon for it,
// not as the folder it is on disk; everything that merely looks like one is not.
func TestLocalListShowsApplicationBundlesWithTheirSystemIcon(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	answers := &iconAnswers{failed: map[string]error{"Broken.app": errors.New("no icon")}}
	state.options.FileIcon = answers.icon
	view := openLocalPane(t, state, session, appIconFixture(t))
	terminal := localRow(t, view, "Terminal.app")
	waitAppIcons(t, state, func() bool { return terminal.appIcon.ready() })
	if terminal.appIcon.bitmap.ID == 0 {
		t.Fatal("the icon has no ID, so the renderer would upload it every frame")
	}
	for _, name := range []string{"Chrome Apps.localized", "plain", "notabundle.app", "Terminal link.app"} {
		if localRow(t, view, name).appIcon != nil {
			t.Errorf("%s was given an application icon", name)
		}
	}
	if localRow(t, view, "Broken.app").appIcon.ready() {
		t.Error("a bundle the system had no icon for shows a picture")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if got := len(canvas.Images()); got != 1 {
		t.Fatalf("pictures on the page = %d, want the one application icon", got)
	}
}

// A refresh of a folder whose bundles have not changed keeps their icons on
// screen instead of drawing the glyph again and asking the system again.
func TestRefreshKeepsApplicationIconsOfUnchangedBundles(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	answers := &iconAnswers{}
	state.options.FileIcon = answers.icon
	dir := appIconFixture(t)
	view := openLocalPane(t, state, session, dir)
	waitAppIcons(t, state, func() bool {
		return localRow(t, view, "Terminal.app").appIcon.ready() && localRow(t, view, "Broken.app").appIcon.ready()
	})
	asked := answers.askedCount()
	if err := state.listLocal(view, dir); err != nil {
		t.Fatal(err)
	}
	if !localRow(t, view, "Terminal.app").appIcon.ready() {
		t.Fatal("the refreshed list drew the glyph until the system answered again")
	}
	state.Wake()
	if got := answers.askedCount(); got != asked {
		t.Fatalf("the system was asked %d more times for bundles that did not change", got-asked)
	}
}

// A system with no icons to give is asked once, not once per bundle.
func TestUnsupportedSystemIsAskedForAnApplicationIconOnce(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	answers := &iconAnswers{failed: map[string]error{"Terminal.app": app.ErrUnsupported, "Broken.app": app.ErrUnsupported}}
	state.options.FileIcon = answers.icon
	view := openLocalPane(t, state, session, appIconFixture(t))
	waitAppIcons(t, state, func() bool { return answers.askedCount() > 0 })
	time.Sleep(20 * time.Millisecond)
	state.Wake()
	if got := answers.askedCount(); got != 1 {
		t.Fatalf("asked %d times, want 1", got)
	}
	if localRow(t, view, "Terminal.app").appIcon.ready() {
		t.Fatal("an unsupported system produced an icon")
	}
}

// A server's disk has no system icon to ask for: a bundle there is a program
// glyph, not a folder.
func TestApplicationBundleWithoutASystemIconGetsTheProgramGlyph(t *testing.T) {
	bundle, _ := sftpFileIcon("Safari.app", os.ModeDir|0o755)
	folder, _ := sftpFileIcon("Safari", os.ModeDir|0o755)
	program, _ := sftpFileIcon("setup.exe", 0o755)
	if bundle != program || bundle == folder {
		t.Fatalf("bundle glyph %p, folder %p, program %p", bundle, folder, program)
	}
	if link, _ := sftpFileIcon("Safari.app", os.ModeSymlink|os.ModeDir); link == bundle {
		t.Fatal("a link to a bundle is drawn as the bundle")
	}
}
