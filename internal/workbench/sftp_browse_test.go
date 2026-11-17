//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func TestSFTPBrowseMapsComponentsAcrossDifferentRoots(t *testing.T) {
	root := t.TempDir()
	roots := [2]string{root, "/srv/web"}
	for _, tc := range []struct {
		local       bool
		input, want string
		ok          bool
	}{
		{true, root, "/srv/web", true},
		{true, filepath.Join(root, "中文", "images"), "/srv/web/中文/images", true},
		{true, root + "-other", "", false},
		{true, filepath.Dir(root), "", false},
		{false, "/srv/web", root, true},
		{false, "/srv/web/中文/images", filepath.Join(root, "中文", "images"), true},
		{false, "/srv/web2/images", "", false},
		{false, "/srv/web/../other", "", false},
	} {
		t.Run(fmt.Sprintf("%v/%s", tc.local, tc.input), func(t *testing.T) {
			got, ok := browseCounterpart(tc.local, roots, tc.input)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("mapped to %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
	if runtime.GOOS == "windows" {
		for _, remote := range []string{"/srv/web/CON", "/srv/web/a:b", `/srv/web/a\b`, "/srv/web/bad?name", "/srv/web/trailing.", "/srv/web/trailing "} {
			if _, ok := browseCounterpart(false, roots, remote); ok {
				t.Fatal("unsafe Windows mapping", remote)
			}
		}
		if _, ok := browseCounterpart(true, [2]string{`C:\web`, "/srv/web"}, `D:\web\images`); ok {
			t.Fatal("cross-volume mapping accepted")
		}
	}
}

func TestSFTPBrowseExplicitNavigationMovesBothPanelsWithoutTransfers(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	w.click(hitSFTPBrowseSync)
	w.navigate(false, "/home/test/docs")
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() && w.view.lfolder.path == filepath.Join(dir, "docs") })
	if w.view.path != "/home/test/docs" || !w.browse.enabled {
		t.Fatal("remote navigation did not follow")
	}
	w.click(sftpWindowUp[0])
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() && w.view.path == "/home/test" })
	w.navigate(true, filepath.Join(dir, "docs"))
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() && w.view.path == "/home/test/docs" })
	reads := remote.readCount("/home/test/docs")
	for range 5 {
		w.refresh(false)
		dualWait(t, state, w, view, func() bool { return !w.view.busy })
	}
	if remote.readCount("/home/test/docs") != reads+5 {
		t.Fatal("listing refresh caused a navigation loop")
	}
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("synchronized browsing queued a transfer")
	}
	w.navigate(false, "/home")
	dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home" })
	if w.browse.enabled || w.view.lfolder.path != filepath.Join(dir, "docs") || len(w.notices.items) == 0 {
		t.Fatal("leaving the pair did not pause browsing")
	}
}

func TestSFTPBrowseFailedSourceAndMissingCounterpartDoNotCreateDirectories(t *testing.T) {
	for _, sourceFails := range []bool{true, false} {
		t.Run(fmt.Sprint(sourceFails), func(t *testing.T) {
			state, w, remote, host, dir := dualSFTPFixture(t, 1)
			view := host.windows[0].view
			w.toggleBrowseSync()
			if sourceFails {
				w.navigate(true, filepath.Join(dir, "missing"))
			} else {
				w.navigate(false, "/home/test/docs")
			}
			dualWait(t, state, w, view, func() bool { return !w.navigationBusy() || w.view.dialog == sftpDialogBrowseCreateDirectory })
			if !sourceFails {
				if w.view.dialog != sftpDialogBrowseCreateDirectory || !w.browse.enabled || len(w.notices.items) != 0 {
					t.Fatal("missing counterpart did not ask before pausing")
				}
				w.click(hitSFTPDialogCancel)
			}
			if w.browse.enabled || w.view.lfolder.path != dir {
				t.Fatal("failure changed local folder or kept sync enabled")
			}
			if sourceFails && (len(w.notices.items) != 1 || w.notices.items[0].kind != components.ToastError) || w.view.errorText != "" {
				t.Fatal("failure must use a global toast")
			}
			if _, err := os.Stat(filepath.Join(dir, "docs")); !os.IsNotExist(err) {
				t.Fatal("missing counterpart was created")
			}
			if sourceFails && w.view.path != "/home/test" {
				t.Fatal("failed source moved its counterpart")
			}
			if len(w.view.worker.Transfers(nil)) != 0 || len(remote.files) != 2 {
				t.Fatal("browsing changed files")
			}
		})
	}
}

func TestSFTPBrowseMissingServerCounterpartUsesFreshListingAndPauses(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.view.worker.Close()
	remote := &workbenchSFTPRemote{cwd: "/home/test", entries: map[string][]os.FileInfo{}, files: map[string][]byte{}}
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return pathErrorRemote{remote, os.ErrPermission}, nil }, func() { w.window.Invalidate() })
	w.view.remember("/home/test/missing", nil)
	os.Mkdir(filepath.Join(dir, "missing"), 0700)
	w.toggleBrowseSync()
	w.navigate(true, filepath.Join(dir, "missing"))
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.browse.enabled || w.view.path != "/home/test" || len(w.notices.items) != 1 || !strings.Contains(w.notices.items[0].detail, "missing") {
		t.Fatal("cached directory hid a failed counterpart")
	}
}

func TestSFTPBrowseSlowNavigationCannotRecurseOrFollowAfterDisabling(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	os.Mkdir(filepath.Join(dir, "docs"), 0700)
	remote.mu.Lock()
	remote.readDelay = 60 * time.Millisecond
	remote.mu.Unlock()
	w.toggleBrowseSync()
	w.navigate(false, "/home/test/docs")
	w.navigate(true, filepath.Join(dir, "tree"))
	if w.view.lfolder.path != dir {
		t.Fatal("overlapping navigation changed the source of a pending pair")
	}
	w.toggleBrowseSync()
	dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })
	if w.view.lfolder.path != dir || w.browse.enabled || w.browse.pending != nil {
		t.Fatal("late listing restarted disabled browsing")
	}
}

func TestSFTPBrowsePairedBookmarkRestoresBothPathsAndSurvivesRestart(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.session.host.CatalogID = "bookmark-pair"
	w.toggleBrowseSync()
	w.click(hitSFTPLocationProfiles)
	w.bookmark.fields[0] = *text.NewEditor("我的项目")
	w.click(hitSFTPBookmarkSave)
	marks := state.sftpHostBookmarks(&w.session)
	if len(marks) != 1 || marks[0].local != dir || !w.bookmark.open {
		t.Fatal("pair was not saved", marks)
	}
	reopened := newTestState(t, state.options)
	if !slices.Equal(reopened.sftpBookmarks, marks) {
		t.Fatal("pair did not survive restart", reopened.sftpBookmarks)
	}
	w.click(hitSFTPBookmarkCancel)
	w.toggleBrowseSync()
	w.navigate(true, filepath.Join(dir, "tree"))
	w.navigate(false, "/home/test/docs")
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	w.openPairedBookmark(marks[0])
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.browse.enabled {
		t.Fatal("opening a profile changed the independent browsing toggle")
	}
	w.toggleBrowseSync()
	if w.view.lfolder.path != dir || w.view.path != "/home/test" || w.browse.roots != [2]string{dir, "/home/test"} {
		t.Fatal("pair did not open both paths")
	}
	w.editBookmark(&marks[0])
	w.bookmark.fields[0] = *text.NewEditor("取消的修改")
	w.click(hitSFTPBookmarkCancel)
	if state.sftpHostBookmarks(&w.session)[0] != marks[0] {
		t.Fatal("cancel edited the bookmark")
	}
	w.removeWindowBookmark(marks[0])
	if len(state.sftpHostBookmarks(&w.session)) != 0 {
		t.Fatal("pair was not removed")
	}
}

func TestSFTPBrowsePathInputUsesTheSamePairAndBookmarkValidationKeepsTheDraft(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	os.Mkdir(filepath.Join(dir, "docs"), 0700)
	w.toggleBrowseSync()
	w.view.local = true
	*w.folder(true).pathInput() = *text.NewEditor("docs")
	w.owner.submitSFTPPath(w.view)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() && w.view.path == "/home/test/docs" })
	w.view.local = false
	*w.folder(false).pathInput() = *text.NewEditor("..")
	w.owner.submitSFTPPath(w.view)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() && w.view.lfolder.path == dir })
	w.session.host.CatalogID = "draft"
	w.editBookmark(nil)
	w.bookmark.fields[0] = *text.NewEditor("项目")
	w.bookmark.fields[2] = *text.NewEditor("relative/path")
	w.saveBookmark()
	if !w.bookmark.open || w.bookmark.errorText == "" || len(state.sftpHostBookmarks(&w.session)) != 0 {
		t.Fatal("invalid bookmark was saved")
	}
	w.bookmark.fields[2] = *text.NewEditor("/home/test")
	w.bookmark.fields[1] = text.Editor{}
	w.saveBookmark()
	marks := state.sftpHostBookmarks(&w.session)
	if len(marks) != 1 || marks[0].local != "" {
		t.Fatal("remote-only bookmark kept paired browsing")
	}
	if len(w.placeBrowser().directoryFavorites()) != 0 {
		t.Fatal("a named profile leaked into the directory favorites")
	}
}

func TestSFTPBrowseBookmarkNativeFieldsAndToggleAtDifferentScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, w, _, host, _ := dualSFTPFixture(t, scale)
			state.setLanguage(i18n.Chinese)
			view := host.windows[0].view
			w.session.host.CatalogID = "native-bookmark"
			click := func(id gfx.ID) {
				t.Helper()
				w.window.Invalidate()
				view.Driver().Pump()
				r := dualHit(t, view, id)
				if err := view.Click(r.X+r.Width/2, r.Y+r.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			click(hitSFTPBrowseSync)
			if !w.browse.enabled {
				t.Fatal("native toggle did not enable browsing")
			}
			click(hitSFTPLocationProfiles)
			if !w.bookmark.open {
				t.Fatal("profiles icon did not open bookmark dialog")
			}
			foundLabel := false
			for _, label := range view.Snapshot().Texts {
				foundLabel = foundLabel || label.Value == state.messages.SFTPWindow.ProfileSite
			}
			if !foundLabel {
				t.Fatal("site profile tab has no visible label")
			}
			for i := range w.bookmark.fields {
				if input := w.textInput(app.TextInputContext{ID: hitSFTPBookmarkFields.At(i)}); input == nil || input.Editor != &w.bookmark.fields[i] {
					t.Fatal("missing native bookmark input", i)
				}
			}
			originalName := w.bookmark.fields[0].String()
			click(hitSFTPProfileScope)
			if !w.bookmark.scopeOpen {
				t.Fatal("scope select did not open")
			}
			view.Keys("down")
			view.Keys("enter")
			if !w.bookmark.shared || w.bookmark.scopeOpen || w.bookmark.fields[0].String() != originalName {
				t.Fatal("scope selection discarded the profile draft")
			}
			click(hitSFTPProfileScope)
			view.Keys("escape")
			if w.bookmark.scopeOpen || !w.bookmark.open {
				t.Fatal("Escape closed the manager instead of its scope popup")
			}
			click(hitSFTPProfileScope)
			view.Keys("up")
			view.Keys("enter")
			if w.bookmark.shared {
				t.Fatal("scope select did not return to this connection")
			}
			click(hitSFTPBookmarkFields.At(0))
			modifier := gfx.ModifierControl
			if runtime.GOOS == "darwin" {
				modifier = gfx.ModifierMeta
			}
			view.Driver().ShortcutChord(modifier, 'a')
			if err := view.Type("验收项目"); err != nil {
				t.Fatal(err)
			}
			if dir := os.Getenv("MOTTY_BROWSE_SCREENSHOTS"); dir != "" && scale == 1 {
				os.MkdirAll(dir, 0700)
				for _, light := range []bool{true, false} {
					if light {
						state.setAppearance(appearanceLight)
					} else {
						state.setAppearance(appearanceDark)
					}
					w.window.Invalidate()
					view.Rest()
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("bookmark-light-%v.png", light))); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := view.Keys("enter"); err != nil {
				t.Fatal(err)
			}
			if !w.bookmark.open || len(state.sftpHostBookmarks(&w.session)) != 1 || state.sftpHostBookmarks(&w.session)[0].name != "验收项目" {
				t.Fatal("native Enter did not save bookmark")
			}
			if dir := os.Getenv("MOTTY_BROWSE_SCREENSHOTS"); dir != "" && scale == 1 {
				click(hitSFTPBookmarkSave)
				for _, light := range []bool{true, false} {
					if light {
						state.setAppearance(appearanceLight)
					} else {
						state.setAppearance(appearanceDark)
					}
					w.window.Invalidate()
					view.Rest()
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("profiles-saved-light-%v.png", light))); err != nil {
						t.Fatal(err)
					}
				}
			}
			view.Keys("escape")
			click(hitSFTPBookmarkToggle)
			click(hitSFTPBookmarkList)
			if !w.view.bookmarksOpen {
				t.Fatal("bookmarks menu did not open")
			}
			if dir := os.Getenv("MOTTY_BROWSE_SCREENSHOTS"); dir != "" && scale == 1 {
				w.window.Invalidate()
				view.Rest()
				view.Screenshot(filepath.Join(dir, "bookmarks-menu.png"))
			}
			view.Keys("escape")
			click(hitSFTPLocationProfiles)
			click(hitSFTPProfileItems.At(0))
			if !w.bookmark.open || w.bookmark.fields[0].String() != "验收项目" {
				t.Fatal("edit action lost bookmark")
			}
			view.Keys("escape")
			if w.bookmark.open {
				t.Fatal("Escape did not dismiss bookmark editor")
			}
			if dir := os.Getenv("MOTTY_BROWSE_SCREENSHOTS"); dir != "" && scale == 1 {
				state.sftpBookmarks = append(state.sftpBookmarks, sftpBookmark{shared: true, name: "通用日志", local: w.view.lfolder.path, path: "/var/log"})
				w.editBookmark(&state.sftpBookmarks[0])
				w.window.Invalidate()
				view.Driver().Pump()
				click(hitSFTPBookmarkSave)
				for _, light := range []bool{true, false} {
					if light {
						state.setAppearance(appearanceLight)
					} else {
						state.setAppearance(appearanceDark)
					}
					w.window.Invalidate()
					view.Rest()
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("compact-profiles-light-%v.png", light))); err != nil {
						t.Fatal(err)
					}
				}
				click(hitSFTPProfileScope)
				view.Rest()
				view.Screenshot(filepath.Join(dir, "scope-select.png"))
				view.Keys("escape")
				view.Keys("escape")
				w.window.Invalidate()
				view.Rest()
				view.Screenshot(filepath.Join(dir, "compact-file-window.png"))
			}
		})
	}
}

func TestSFTPLocationProfilesAreConfiguredBeforeOpeningAndHaveSeparateScopes(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.session.host.CatalogID = "project-a"
	w.click(hitSFTPLocationProfiles)
	w.bookmark.fields[0] = *text.NewEditor("服务器文档")
	w.bookmark.fields[1] = *text.NewEditor(filepath.Join(dir, "tree"))
	w.bookmark.fields[2] = *text.NewEditor("/home/test/docs")
	w.click(hitSFTPBookmarkSave)
	if !w.bookmark.open || w.view.lfolder.path != dir || w.view.path != "/home/test" {
		t.Fatal("saving a profile navigated the panels")
	}
	if len(w.visibleProfiles()) != 1 {
		t.Fatal("site profile not in the site list")
	}
	w.click(hitSFTPProfileShared)
	if len(w.visibleProfiles()) != 1 || w.bookmark.fields[0].String() != "服务器文档" || !w.bookmark.editing {
		t.Fatal("scope selector changed the list or discarded the edited profile")
	}
	w.click(hitSFTPProfileNew)
	w.bookmark.fields[0] = *text.NewEditor("共享文档")
	w.click(hitSFTPBookmarkSave)
	if len(w.visibleProfiles()) != 2 || !w.visibleProfiles()[1].shared || w.visibleProfiles()[0].shared {
		t.Fatal("shared profile not saved")
	}
	other := w.session
	other.host.CatalogID = "project-b"
	if profiles := state.sftpHostBookmarks(&other); len(profiles) != 1 || profiles[0].name != "共享文档" {
		t.Fatal("scope isolation failed", profiles)
	}
	reopened := newTestState(t, state.options)
	if profiles := reopened.sftpHostBookmarks(&other); len(profiles) != 1 || !profiles[0].shared {
		t.Fatal("shared profile did not survive a restart")
	}
	w.click(hitSFTPProfileSite)
	w.click(hitSFTPProfileItems.At(0))
	if w.bookmark.fields[1].String() != filepath.Join(dir, "tree") || w.bookmark.fields[2].String() != "/home/test/docs" {
		t.Fatal("choosing a profile did not fill both configured paths")
	}
	w.click(hitSFTPProfileOpen)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.bookmark.open || w.view.lfolder.path != filepath.Join(dir, "tree") || w.view.path != "/home/test/docs" || w.browse.enabled {
		t.Fatal("Open did not use the configured pair independently of synchronization")
	}
	w.toggleBrowseSync()
	if !w.browse.enabled || w.browse.roots != [2]string{filepath.Join(dir, "tree"), "/home/test/docs"} {
		t.Fatal("sync did not adopt configured paths")
	}
}

func TestSFTPBrowseDisableDuringCounterpartReadDoesNotApplyTheLateResponse(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	os.Mkdir(filepath.Join(dir, "docs"), 0700)
	remote.mu.Lock()
	remote.readDelay = 80 * time.Millisecond
	remote.mu.Unlock()
	w.toggleBrowseSync()
	w.navigate(true, filepath.Join(dir, "docs"))
	if w.browse.pending == nil || !w.browse.pending.second || !w.view.busy {
		t.Fatal("counterpart request was not pending")
	}
	w.toggleBrowseSync()
	dualWait(t, state, w, view, func() bool { return !w.view.busy })
	if w.view.path != "/home/test" || w.view.lfolder.path != filepath.Join(dir, "docs") || w.browse.enabled || w.view.abandonedPath != "" {
		t.Fatal("disabled synchronization applied a late peer listing")
	}
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("navigation touched transfers")
	}
}

func TestSFTPBrowseCancelLocalCounterpartRetiresLoadingAndRejectsLateRows(t *testing.T) {
	state, w, _, _, dir := dualSFTPFixture(t, 1)
	destination := filepath.Join(dir, "docs")
	w.toggleBrowseSync()
	w.browse.pending = &sftpBrowseRequest{paths: [2]string{destination, "/home/test/docs"}, side: 0, second: true}
	w.pendingLocal = destination
	seq := w.view.localSeq
	w.view.startListingStatus(state.messages.SFTPLoading)
	w.view.localListingStatus = true
	w.toggleBrowseSync()
	state.applyLocalListing(w.view, localListing{dir: destination}, seq)
	if w.view.localListingStatus || w.view.listingStatus != "" || w.view.status == state.messages.SFTPLoading || w.pendingLocal != "" || w.view.lfolder.path != dir {
		t.Fatal("canceled local counterpart retained loading or applied a late listing")
	}
}

func TestSFTPLocationProfilePreservesForeignPlatformPathsOnRestore(t *testing.T) {
	state, w, _, _, dir := dualSFTPFixture(t, 1)
	foreign := `C:\Projects\web`
	if runtime.GOOS == "windows" {
		foreign = "/home/developer/web"
	}
	state.restoreSFTPSettings(savedState{SFTPBookmarks: []savedBookmark{{Host: "profile-host", Path: "/srv/web", Name: "跨平台项目", Local: foreign}}})
	saved := state.snapshot()
	if len(saved.SFTPBookmarks) != 1 || saved.SFTPBookmarks[0].Local != foreign {
		t.Fatal("restoring shared configuration erased a foreign local path")
	}
	w.openPairedBookmark(state.sftpBookmarks[0])
	if w.view.lfolder.path != dir || len(w.notices.items) == 0 || w.browse.pending != nil {
		t.Fatal("foreign local path was opened without native validation")
	}
	for _, path := range []string{`D:\OneDrive\文档`, `\\server\share\project`, "/home/developer/web"} {
		if !validSavedBookmarkLocal(path) {
			t.Fatal("portable absolute path rejected", path)
		}
	}
	for _, path := range []string{"relative/path", "C:relative", "bad\x00path", `\\server`} {
		if validSavedBookmarkLocal(path) {
			t.Fatal("invalid saved path accepted", path)
		}
	}
}

func TestSFTPBrowseHeaderIconsAndPreservedRootsRemainVisible(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	os.Mkdir(filepath.Join(dir, "docs"), 0700)
	w.toggleBrowseSync()
	w.navigate(true, filepath.Join(dir, "docs"))
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	w.toggleBrowseSync()
	w.toggleBrowseSync()
	if w.browse.roots != [2]string{dir, "/home/test"} {
		t.Fatal("re-enabling narrowed the configured roots")
	}
	w.window.Invalidate()
	view.Driver().Pump()
	view.Rest()
	up, profiles, syncIcon := dualHit(t, view, sftpWindowUp[0]), dualHit(t, view, hitSFTPLocationProfiles), dualHit(t, view, hitSFTPBrowseSync)
	// Both actions share the local toolbar row and stay in the local half.
	if profiles.Y != up.Y || syncIcon.Y != up.Y || syncIcon.X <= profiles.X || syncIcon.X+syncIcon.Width > w.ui.list[0].X+w.ui.list[0].Width+8 {
		t.Fatal("actions left the local header row", up, profiles, syncIcon)
	}
	foundLabel, foundHint := false, false
	for _, label := range view.Snapshot().Texts {
		foundLabel = foundLabel || label.Value == state.messages.SFTPWindow.BrowseRootLabel
	}
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == hitSFTPBrowseSync {
			foundHint = strings.Contains(hit.Label, dir) && strings.Contains(hit.Label, "/home/test")
		}
	}
	if !foundLabel || !foundHint {
		t.Fatal("active synchronization has no visible scope or full-path tooltip")
	}
	w.click(sftpWindowUp[0])
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.view.lfolder.path != dir || w.view.path != "/home/test" {
		t.Fatal("parent navigation did not respect configured roots")
	}
}

func TestSFTPBrowseIntegratesWithIndependentFiltersAndHeaderMenus(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1.25)
	view := host.windows[0].view
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	w.refresh(true)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	w.click(hitSFTPBrowseSync)
	reads, localSeq := remote.readCount(w.view.path), w.view.localSeq
	dualFind(t, view, w, true)
	dualReplaceFilter(t, view, "docs")
	dualFind(t, view, w, false)
	dualReplaceFilter(t, view, "notes")
	if remote.readCount(w.view.path) != reads || w.view.localSeq != localSeq || !w.browse.enabled {
		t.Fatal("filtering performed paired navigation")
	}
	// Explicit paths still navigate when a counterpart is filtered out.
	dualClickID(t, view, sftpWindowPath[0])
	if _, err := view.Driver().ShortcutChord(keymap.Primary, 'a'); err != nil {
		t.Fatal(err)
	}
	if err := view.Type(filepath.Join(dir, "docs")); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.view.path != "/home/test/docs" || w.view.lfolder.path != filepath.Join(dir, "docs") || !w.browse.enabled {
		t.Fatal("paired path navigation lost its roots with active filters")
	}
	if err := view.Resize(640, 600); err != nil {
		t.Fatal(err)
	}
	dualClickID(t, view, sftpWindowMore[0])
	dualHit(t, view, sftpWindowNewFolder[0])
	dualHit(t, view, sftpWindowFilterToggle[0])
	dualHit(t, view, sftpWindowHeaderHidden[0])
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	w.view.lfolder.showHidden = true
	w.window.Invalidate()
	view.Driver().Pump()
	view.Rest()
	for _, id := range []gfx.ID{sftpWindowUp[0], sftpWindowRefresh[0], sftpWindowHidden[0], hitSFTPLocationProfiles, hitSFTPBrowseSync, sftpWindowMore[0]} {
		bounds := dualHit(t, view, id)
		if bounds.X < w.ui.list[0].X-2 || bounds.X+bounds.Width > w.ui.list[0].X+w.ui.list[0].Width+2 {
			t.Fatal("local toolbar overflowed a narrow pane", id, bounds)
		}
	}
	w.activeLocal, w.view.local = false, false
	dualClickID(t, view, hitSFTPLocationProfiles)
	if !w.bookmark.open || !w.activeLocal || view.Snapshot().Focused != hitSFTPBookmarkFields.At(1) || w.headerMenuOpen() {
		t.Fatal("local profiles action did not move focus away from the remote pane")
	}
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("filtering or paired navigation queued a transfer")
	}
}

func TestSFTPProfileScopeChangesPersistWithoutDuplicatingOrOverfilling(t *testing.T) {
	state, w, _, _, dir := dualSFTPFixture(t, 1)
	w.session.host.CatalogID = "scope-edit"
	profile := sftpBookmark{host: "scope-edit", name: "项目", local: dir, path: "/home/test"}
	state.sftpBookmarks = []sftpBookmark{profile}
	w.editBookmark(&profile)
	w.click(hitSFTPProfileShared)
	w.click(hitSFTPBookmarkSave)
	if len(state.sftpBookmarks) != 1 || !state.sftpBookmarks[0].shared || state.sftpBookmarks[0].host != "" {
		t.Fatal("scope edit did not update the original record")
	}
	other := w.session
	other.host.CatalogID = "other"
	if len(state.sftpHostBookmarks(&other)) != 1 {
		t.Fatal("all-connections profile was unavailable to another host")
	}
	w.click(hitSFTPProfileSite)
	w.click(hitSFTPBookmarkSave)
	if len(state.sftpHostBookmarks(&other)) != 0 || state.sftpBookmarks[0].shared {
		t.Fatal("scope edit did not return the profile to this connection")
	}
	for i := range maxBookmarksPerHost {
		state.sftpBookmarks = append(state.sftpBookmarks, sftpBookmark{shared: true, name: fmt.Sprintf("共享%02d", i), path: "/tmp"})
	}
	w.click(hitSFTPProfileShared)
	w.click(hitSFTPBookmarkSave)
	if w.bookmark.errorText == "" || state.sftpBookmarks[0].shared {
		t.Fatal("moving scopes bypassed the destination limit")
	}
}

// The pane header decides whether the profile menu can open while it paints,
// so it counts instead of building the list. Both must name the same profiles,
// and the count must not allocate.
func TestSFTPVisibleProfileCountMatchesTheListWithoutAllocating(t *testing.T) {
	state, w, _, _, dir := dualSFTPFixture(t, 1)
	w.session.host.CatalogID = "count-profiles"
	state.sftpBookmarks = []sftpBookmark{
		{host: "count-profiles", name: "项目", local: dir, path: "/home/test"},
		{host: "count-profiles", path: "/home/test"},           // nothing names it, so it is not offered
		{host: "another-host", name: "别处", path: "/home/test"}, // another connection's own profile
		{shared: true, name: "共享", path: "/var/log"},
	}
	offered := w.visibleProfiles()
	if count := w.visibleProfileCount(); count != len(offered) {
		t.Fatalf("counted %d profile(s), listed %d", count, len(offered))
	}
	if len(offered) != 2 {
		t.Fatalf("offered profiles = %+v, want the named one and the shared one", offered)
	}
	if allocs := testing.AllocsPerRun(50, func() { _ = w.visibleProfileCount() }); allocs != 0 {
		t.Errorf("counting the profiles allocates %v times", allocs)
	}
}

func TestSFTPLocationProfileListScrollSelectionRenameAndDeletion(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.session.host.CatalogID = "many-profiles"
	for i := range 30 {
		state.sftpBookmarks = append(state.sftpBookmarks, sftpBookmark{host: "many-profiles", name: fmt.Sprintf("项目 %02d", i), local: dir, path: "/home/test"})
	}
	w.editBookmark(nil)
	w.window.Invalidate()
	view.Driver().Pump()
	view.Rest()
	if w.bookmark.area.MaxOffsetY <= 0 {
		t.Fatal("profiles did not scroll")
	}
	view.Scroll(gfx.ScrollEvent{X: w.bookmark.list.X + 10, Y: w.bookmark.list.Y + 10, DeltaY: 10000})
	w.window.Invalidate()
	view.Driver().Pump()
	view.Rest()
	r := dualHit(t, view, hitSFTPProfileItems.At(29))
	view.Click(r.X+20, r.Y+r.Height/2)
	if w.bookmark.fields[0].String() != "项目 29" {
		t.Fatal("scrolling did not keep profile identity")
	}
	w.bookmark.fields[0] = *text.NewEditor("改名后的项目")
	w.click(hitSFTPBookmarkSave)
	if len(w.visibleProfiles()) != 30 || w.visibleProfiles()[29].name != "改名后的项目" {
		t.Fatal("renaming duplicated or lost profile")
	}
	w.click(hitSFTPProfileDelete)
	if len(w.visibleProfiles()) != 29 {
		t.Fatal("selected profile was not deleted")
	}
}
