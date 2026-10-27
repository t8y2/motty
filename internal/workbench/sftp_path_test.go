package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func TestSFTPPathSettingPreservesBreadcrumbsByDefaultAndSavesTheChoice(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	paint := func() *ui.Canvas {
		canvas := &ui.Canvas{}
		state.Paint(canvas, ui.Size{Width: 1400, Height: 900})
		return canvas
	}
	if _, ok := hitByID(paint(), contextID(hitSFTPPath)); ok || state.EditorFor(hitSFTPPath) != nil {
		t.Fatal("path input is enabled by default")
	}
	if _, ok := hitByID(paint(), contextID(hitSFTPBreadcrumbList.At(1))); !ok {
		t.Fatal("default breadcrumbs are missing")
	}
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsFiles)
	for _, id := range []ui.ID{hitSettingsPathBreadcrumbs, hitSettingsPathInput} {
		if hit, ok := hitByID(paint(), id); !ok || hit.Role != ui.RoleRadio || hit.Selected != (id == hitSettingsPathBreadcrumbs) {
			t.Fatalf("path style option = %+v, present = %v", hit, ok)
		}
	}
	state.Click(hitSettingsPathInput)
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || !saved.SFTPPathInput {
		t.Fatalf("saved path input = %v, error = %v", saved.SFTPPathInput, err)
	}
	restarted := newTestState(t, state.options)
	if !restarted.sftpPathInput {
		t.Fatal("path input choice was not restored")
	}
	state.ActivatePane(session.ID)
	state.Click(hitSFTPOpen)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path != "" })
	canvas := paint()
	if hit, ok := hitByID(canvas, contextID(hitSFTPPath)); !ok || hit.Role != ui.RoleTextField || hit.Value != session.files.path {
		t.Fatalf("path input hit = %+v, present = %v", hit, ok)
	}
	if _, ok := hitByID(canvas, contextID(hitSFTPBreadcrumbList.At(1))); ok {
		t.Fatal("input mode still paints breadcrumbs")
	}
	state.Click(hitSettingsPathBreadcrumbs)
	saved, _, err = loadState(state.options.StatePath)
	if err != nil || saved.SFTPPathInput {
		t.Fatalf("disabled path input = %v, error = %v", saved.SFTPPathInput, err)
	}
}

func TestSFTPPathEnterNavigatesRelativeAndAbsoluteRemoteDirectories(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.sftpPathInput = true
	for _, step := range []struct{ input, want string }{
		{"docs", "/home/test/docs"},
		{"..", "/home/test"},
		{"/home/test/docs/../docs", "/home/test/docs"},
		{"/home/test", "/home/test"},
	} {
		editor := state.EditorFor(contextID(hitSFTPPath))
		if editor == nil {
			t.Fatal("mounted path input has no editor")
		}
		*editor = *text.NewEditor(step.input)
		result := state.Key(ui.KeyEvent{Target: contextID(hitSFTPPath), Key: ui.KeyEnter})
		if !result.Handled {
			t.Fatal("Enter was not handled")
		}
		waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == step.want })
		if got := state.EditorFor(hitSFTPPath).String(); got != step.want {
			t.Fatalf("input after navigating %q = %q, want %q", step.input, got, step.want)
		}
	}
}

func TestSFTPPathKeepsDraftAcrossRefreshAndSeparatesLocalAndRemote(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.sftpPathInput = true
	view := session.files
	*state.EditorFor(hitSFTPPath) = *text.NewEditor("unfinished draft")
	state.showSFTPListing(view, view.path, nil)
	if got := state.EditorFor(hitSFTPPath).String(); got != "unfinished draft" {
		t.Fatalf("refresh overwrote draft with %q", got)
	}
	openLocalPane(t, state, session, localFixture(t))
	*state.EditorFor(hitSFTPPath) = *text.NewEditor("local draft")
	state.Click(hitSFTPPaneToggle)
	if got := state.EditorFor(hitSFTPPath).String(); got != "unfinished draft" {
		t.Fatalf("remote draft = %q", got)
	}
	state.Click(hitSFTPPaneToggle)
	if got := state.EditorFor(hitSFTPPath).String(); got != "local draft" {
		t.Fatalf("local draft = %q", got)
	}
}

func TestSFTPPathLocalNavigationAndErrorsLeaveTheCurrentFolderIntact(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.sftpPathInput = true
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	for _, step := range []struct{ input, want string }{
		{"photos", filepath.Join(dir, "photos")},
		{dir, dir},
	} {
		*state.EditorFor(hitSFTPPath) = *text.NewEditor(step.input)
		state.Key(ui.KeyEvent{Target: contextID(hitSFTPPath), Key: ui.KeyEnter})
		if view.lfolder.path != step.want || state.EditorFor(hitSFTPPath).String() != step.want {
			t.Fatalf("local path = %q after %q, want %q", view.lfolder.path, step.input, step.want)
		}
	}
	for _, invalid := range []string{"missing-folder", "report.txt", ""} {
		*state.EditorFor(hitSFTPPath) = *text.NewEditor(invalid)
		state.Key(ui.KeyEvent{Target: hitSFTPPath, Key: ui.KeyEnter})
		if view.lfolder.path != dir || view.errorText != "" || state.EditorFor(hitSFTPPath).String() != invalid || len(state.fileNotices.items) == 0 {
			t.Fatalf("invalid %q: path = %q, error = %q", invalid, view.lfolder.path, view.errorText)
		}
	}
	state.Key(ui.KeyEvent{Target: hitSFTPPath, Key: ui.KeyEscape})
	if state.EditorFor(hitSFTPPath).String() != dir || session.files == nil {
		t.Fatal("Escape did not reset the path while keeping the panel open")
	}
}

func TestSFTPPathStyleSelectorSupportsKeyboardAndSearch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsFiles)
	result := state.Key(ui.KeyEvent{Target: hitSettingsPathBreadcrumbs, Key: ui.KeyRight})
	if !result.Handled || !state.sftpPathInput || result.Focus != hitSettingsPathInput {
		t.Fatalf("right arrow = %+v, input = %v", result, state.sftpPathInput)
	}
	state.Click(hitSettingsPathInput) // selecting the same option must not toggle it off
	if !state.sftpPathInput {
		t.Fatal("selecting Input field twice changed the style")
	}
	result = state.Key(ui.KeyEvent{Target: hitSettingsPathInput, Key: ui.KeyLeft})
	if !result.Handled || state.sftpPathInput {
		t.Fatal("left arrow did not select breadcrumbs")
	}
	state.setLanguage(i18n.Chinese)
	searchSettings(t, state, "输入框")
	if state.settings.count == 0 || state.settings.results[0].category != settingsFiles {
		t.Fatal("path style is not searchable")
	}
}

type pathErrorRemote struct {
	*workbenchSFTPRemote
	err error
}

func (remote pathErrorRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	if directory == "/home/test/missing" {
		return nil, remote.err
	}
	return remote.workbenchSFTPRemote.ReadDir(directory)
}

func TestSFTPPathRemoteFailureUsesAGlobalToastWithTheRequestedPath(t *testing.T) {
	for _, cause := range []error{os.ErrNotExist, &sftp.StatusError{Code: uint32(sftp.ErrSSHFxNoSuchFile)}, os.ErrPermission} {
		t.Run(cause.Error(), func(t *testing.T) {
			state, session, _, remote := openTestSFTP(t)
			state.setLanguage(i18n.Chinese)
			state.sftpPathInput = true
			view := session.files
			view.worker.Close()
			<-view.worker.Done()
			view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return pathErrorRemote{remote, cause}, nil }, nil)
			// A manual request must be checked even when this directory was cached.
			view.remember("/home/test/missing", nil)
			for attempt := 0; attempt < 2; attempt++ {
				*state.EditorFor(hitSFTPPath) = *text.NewEditor("missing")
				state.Key(ui.KeyEvent{Target: contextID(hitSFTPPath), Key: ui.KeyEnter})
				waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
			}
			if view.path != "/home/test" || view.errorText != "" || state.EditorFor(hitSFTPPath).String() != "missing" || view.pathRequest != "" {
				t.Fatalf("failed path changed the view: path=%q error=%q request=%q", view.path, view.errorText, view.pathRequest)
			}
			if len(state.fileNotices.items) != 1 {
				t.Fatalf("duplicate error toasts = %d", len(state.fileNotices.items))
			}
			notice := state.fileNotices.items[0]
			if !strings.Contains(notice.detail, "/home/test/missing") || notice.kind != components.ToastError {
				t.Fatalf("toast = %+v", notice)
			}
			if cause != os.ErrPermission && notice.detail != "路径不存在：/home/test/missing" {
				t.Fatalf("missing path message = %q", notice.detail)
			}
			if cause == os.ErrPermission && !strings.Contains(notice.detail, cause.Error()) {
				t.Fatalf("failure reason missing from %q", notice.detail)
			}
			var canvas ui.Canvas
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
			if hit, ok := hitByID(&canvas, notice.id); !ok || hit.Bounds.Y > 250 {
				t.Fatalf("global toast close button = %+v, present = %v", hit, ok)
			}
			state.Click(notice.id)
			if len(state.fileNotices.items) != 0 || state.fileNotices.queue.Len() != 0 {
				t.Fatal("global toast did not dismiss")
			}
		})
	}
}

func TestSFTPPathMissingLocalDirectoryShowsItsPathInTheGlobalToast(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.setLanguage(i18n.Chinese)
	state.sftpPathInput = true
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	*state.EditorFor(hitSFTPPath) = *text.NewEditor("不存在的目录")
	state.Key(ui.KeyEvent{Target: hitSFTPPath, Key: ui.KeyEnter})
	want := fmt.Sprintf(state.messages.Tools.FilesPathNotFoundFmt, filepath.Join(dir, "不存在的目录"))
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].detail != want || view.errorText != "" || view.lfolder.path != dir {
		t.Fatalf("local path failure: notices=%+v, path=%q, inline error=%q", state.fileNotices.items, view.lfolder.path, view.errorText)
	}
}

func TestSFTPPathDoesNotNavigateOnRepeatsModifiersOrIMEConfirmation(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	state.sftpPathInput = true
	view := session.files
	editor := state.EditorFor(hitSFTPPath)
	*editor = *text.NewEditor("docs")
	before := remote.readCount("/home/test/docs")
	for _, event := range []ui.KeyEvent{
		{Target: hitSFTPPath, Key: ui.KeyEnter, Repeat: true},
		{Target: hitSFTPPath, Key: ui.KeyEnter, Modifiers: ui.ModifierShift},
	} {
		state.Key(event)
	}
	editor.SetMarkedText("目录", text.Range{Start: 0, End: 2})
	state.Key(ui.KeyEvent{Target: hitSFTPPath, Key: ui.KeyEnter})
	if view.busy || view.path != "/home/test" || remote.readCount("/home/test/docs") != before {
		t.Fatal("an uncommitted input started navigation")
	}
}
