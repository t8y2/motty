//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func unreadableLocalListing(t *testing.T, dir string) localListing {
	t.Helper()
	items := scopeDirEntries(t, dir)
	for _, item := range items {
		if !item.IsDir() {
			if err := os.Remove(filepath.Join(dir, item.Name())); err != nil {
				t.Fatal(err)
			}
			return scanLocalListing(dir, items)
		}
	}
	t.Fatal("fixture has no file to remove")
	return localListing{}
}

func nativeScopeWarning(t *testing.T, preview *headless.Session, english bool) gfx.Hit {
	t.Helper()
	for _, hit := range preview.Snapshot().Hits {
		if hit.Role != gfx.RoleStatus || hit.Hint == "" {
			continue
		}
		if english && !strings.Contains(hit.Hint, "Filtering covers loaded items only") || !english && !strings.Contains(hit.Hint, "筛选仅覆盖已加载项目") {
			continue
		}
		if hit.Label != hit.Hint || hit.Bounds.Width <= 0 || hit.Bounds.Height < 30 {
			t.Fatalf("incomplete or inaccessible native warning: %+v", hit)
		}
		loaded, bounded := false, false
		for _, text := range preview.Snapshot().Texts {
			if text.Bounds.Y < hit.Bounds.Y || text.Bounds.Y >= hit.Bounds.Y+hit.Bounds.Height {
				continue
			}
			loaded = loaded || english && text.Value == "Incomplete - 2 items loaded" || !english && text.Value == "目录不完整 - 已加载 2 项"
			bounded = bounded || english && text.Value == "Filtering covers loaded items only" || !english && text.Value == "筛选仅覆盖已加载项目"
		}
		if !loaded || !bounded {
			t.Fatal("native status hint exists but loaded count/filter scope is not visible")
		}
		return hit
	}
	t.Fatal("missing full native scope status label/hint")
	return gfx.Hit{}
}

func scopeScreenshot(t *testing.T, preview *headless.Session, name string) {
	t.Helper()
	if dir := os.Getenv("MOTTY_SCOPE_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalScopeNativeSidebarWarningAndFilter(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	state.applyLocalListing(view, unreadableLocalListing(t, dir), view.localSeq)
	view.lfolder.filterOpen = true
	view.status = "Transfer completed"
	preview, err := headless.Open(app.Config{
		Title: "Local coverage", Width: 1000, Height: 520, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: state.SFTPInput,
	}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	nativeScopeWarning(t, preview, true)
	dualClickID(t, preview, contextID(hitSFTPFilter))
	if err := preview.Type("missing"); err != nil {
		t.Fatal(err)
	}
	view.status = "Transfer completed"
	if err := preview.Resize(1000, 520); err != nil {
		t.Fatal(err)
	}
	dualText(t, preview, "No matches in loaded items")
	dualText(t, preview, "Transfer completed")
	nativeScopeWarning(t, preview, true)
	scopeScreenshot(t, preview, "sidebar-narrow-zero-match")
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceLight)
	if err := preview.Resize(1000, 520); err != nil {
		t.Fatal(err)
	}
	nativeScopeWarning(t, preview, false)
	scopeScreenshot(t, preview, "sidebar-chinese-light")
}

func TestLocalScopeNativeWindowWarningSurvivesCompactAndFeedback(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1.25)
	preview := host.windows[0].view
	state.applyLocalListing(w.view, unreadableLocalListing(t, dir), w.view.localSeq)
	dualFind(t, preview, w, true)
	dualReplaceFilter(t, preview, "missing")
	w.view.status = "Transfer completed"
	for _, height := range []int{600, 270} {
		if err := preview.Resize(640, height); err != nil {
			t.Fatal(err)
		}
		warning := nativeScopeWarning(t, preview, true)
		if warning.Bounds.Y < 0 || warning.Bounds.Y+warning.Bounds.Height > float32(height) {
			t.Fatalf("warning escaped compact window: %+v", warning.Bounds)
		}
		dualText(t, preview, "No matches in loaded items")
		dualText(t, preview, "Transfer completed")
		if height == 270 {
			for _, text := range preview.Snapshot().Texts {
				if text.Value == "0 of 2 items" {
					t.Fatal("fixture did not exercise the compact pane's hidden ordinary count")
				}
			}
		}
		scopeScreenshot(t, preview, fmt.Sprintf("window-narrow-height-%d", height))
	}
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceLight)
	w.window.Invalidate()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	nativeScopeWarning(t, preview, false)
	scopeScreenshot(t, preview, "window-chinese-light-compact")
}

func TestLocalScopeWindowWarningAddsNoSteadyPaintAllocations(t *testing.T) {
	if raceDetector {
		t.Skip("allocation counts are not comparable under the race detector")
	}
	state, w, _, _, dir := dualSFTPFixture(t, 1)
	partial := unreadableLocalListing(t, dir)
	complete := scanLocalListing(dir, scopeDirEntries(t, dir))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1200, Height: 800}
	measure := func(listing localListing) float64 {
		state.applyLocalListing(w.view, listing, w.view.localSeq)
		w.paint(&canvas, size)
		return testing.AllocsPerRun(50, func() {
			canvas.Reset()
			w.paint(&canvas, size)
		})
	}
	baseline := measure(complete)
	if covered := measure(partial); covered > baseline {
		t.Fatalf("warning increased steady paint allocations: complete=%g incomplete=%g", baseline, covered)
	} else {
		t.Logf("window allocations complete=%g incomplete=%g", baseline, covered)
	}
}

func hiddenPartialScope(t *testing.T) localListing {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{".hidden", "lost"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	markLocalHidden(t, filepath.Join(dir, ".hidden"))
	items := scopeDirEntries(t, dir)
	if err := os.Remove(filepath.Join(dir, "lost")); err != nil {
		t.Fatal(err)
	}
	return scanLocalListing(dir, items)
}

func assertHiddenScopeEmpty(t *testing.T, preview *headless.Session) {
	t.Helper()
	dualText(t, preview, "No visible loaded items")
	for _, text := range preview.Snapshot().Texts {
		if text.Value == "No readable items loaded" {
			t.Fatal("readable but hidden row was described as unreadable")
		}
	}
	for _, hit := range preview.Snapshot().Hits {
		if hit.Role == gfx.RoleStatus && hit.Label == hit.Hint && strings.Contains(hit.Hint, "1 items loaded") && strings.Contains(hit.Hint, "1 unreadable") {
			return
		}
	}
	t.Fatal("hidden-only loaded scope lost its full semantic count/cause")
}

func TestLocalScopeReviewNativeHiddenLoadedItemsRemainReadable(t *testing.T) {
	t.Run("sidebar", func(t *testing.T) {
		state, session, _, _ := openTestSFTP(t)
		partial := hiddenPartialScope(t)
		view := openLocalPane(t, state, session, partial.dir)
		state.applyLocalListing(view, partial, view.localSeq)
		preview, err := headless.Open(app.Config{
			Title: "Hidden loaded scope", Width: 1000, Height: 520, Tree: Tree(),
			Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		}, nil, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer preview.Close()
		assertHiddenScopeEmpty(t, preview)
		dualClickID(t, preview, contextID(hitSFTPHeaderMore))
		dualClickID(t, preview, contextID(hitSFTPHeaderHidden))
		if len(view.lfolder.rows) != 1 || view.lfolder.rows[0].entry.Name != ".hidden" {
			t.Fatal("show hidden did not restore the readable loaded row")
		}
		dualText(t, preview, ".hidden")
		for _, text := range preview.Snapshot().Texts {
			if text.Value == "No visible loaded items" {
				t.Fatal("sidebar kept its empty message after revealing a loaded row")
			}
		}
	})
	t.Run("window", func(t *testing.T) {
		state, w, _, host, _ := dualSFTPFixture(t, 1)
		preview := host.windows[0].view
		state.applyLocalListing(w.view, hiddenPartialScope(t), w.view.localSeq)
		if err := preview.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		assertHiddenScopeEmpty(t, preview)
		dualClickID(t, preview, sftpWindowMore[0])
		dualClickID(t, preview, sftpWindowHeaderHidden[0])
		if len(w.view.lfolder.rows) != 1 || w.view.lfolder.rows[0].entry.Name != ".hidden" {
			t.Fatal("window show hidden did not restore the readable loaded row")
		}
		dualText(t, preview, ".hidden")
		for _, text := range preview.Snapshot().Texts {
			if text.Value == "No visible loaded items" {
				t.Fatal("window kept its empty message after revealing a loaded row")
			}
		}
	})
}

// A query that only matches hidden rows is not "no matches": the folder's own
// filter is what keeps them out, and the empty state has to say so.
func TestLocalScopeFilterMatchThatIsHiddenIsNotReportedAsAbsent(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	partial := hiddenPartialScope(t)
	view := openLocalPane(t, state, session, partial.dir)
	state.applyLocalListing(view, partial, view.localSeq)
	view.lfolder.filterInputEditor().Insert("hidden")
	state.applySFTPFilter(view, &view.lfolder, false)
	if len(view.lfolder.rows) != 0 {
		t.Fatalf("a hidden match stayed visible: %d rows", len(view.lfolder.rows))
	}
	if got := view.lfolder.coverageEmpty; got != state.messages.Tools.FilesLoadedMatchesHidden {
		t.Fatalf("empty state = %q, want the hidden-match message", got)
	}
	preview, err := headless.Open(app.Config{
		Title: "Hidden match", Width: 1000, Height: 520, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	dualText(t, preview, state.messages.Tools.FilesLoadedMatchesHidden)
}
