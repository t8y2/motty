package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

// localEditorFolder is a folder on this computer, and the URL VS Code gets for it.
func localEditorFolder() (string, string) {
	if runtime.GOOS == "windows" {
		return `C:\Users\me\project`, "vscode://file/C:/Users/me/project"
	}
	return "/home/me/project", "vscode://file/home/me/project"
}

func TestExternalEditorURI(t *testing.T) {
	localDir, localWant := localEditorFolder()
	for _, test := range []struct {
		name      string
		editor    ExternalEditor
		authority string
		directory string
		want      string
		wantErr   bool
	}{
		{"vscode alias", editorVSCode, "prod", "/srv/app", "vscode://vscode-remote/ssh-remote+prod/srv/app", false},
		{"zed alias", editorZed, "prod", "/srv/app", "zed://ssh/prod/srv/app", false},
		{"zed endpoint with a port", editorZed, "me@box:2222", "/srv", "zed://ssh/me@box:2222/srv", false},
		{"a folder on this computer", editorVSCode, "", localDir, localWant, false},
		{"a folder whose name needs escaping", editorZed, "prod", "/srv/a b/c#d%e'f/\u9879\u76ee",
			"zed://ssh/prod/srv/a%20b/c%23d%25e%27f/%E9%A1%B9%E7%9B%AE", false},
		{"no folder", editorVSCode, "prod", "", "", true},
		{"a relative folder", editorZed, "prod", "srv/app", "", true},
		{"an editor this build does not know", ExternalEditor("sublime"), "prod", "/srv", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := editorURI(test.editor, test.authority, test.directory)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("editorURI = %q, %v; want %q, error %t", got, err, test.want, test.wantErr)
			}
		})
	}
}

func editorTestRequest(editor ExternalEditor, port uint16, alias string) editorRequest {
	return editorRequest{
		editor: editor, directory: "/srv/app", portFmt: i18n.For(i18n.English).Tools.FilesEditorPortFmt,
		host: editorHost{address: "prod.example.com", user: "deploy", port: port}, alias: alias,
	}
}

func TestResolveEditorAuthority(t *testing.T) {
	ipv6 := editorTestRequest(editorZed, 22, "")
	ipv6.host.address = "2001:db8::1"
	for _, test := range []struct {
		name    string
		request editorRequest
		want    string
		wantErr bool
	}{
		{"a live alias wins", editorTestRequest(editorVSCode, 2222, "prod"), "prod", false},
		{"a port Zed can carry", editorTestRequest(editorZed, 2222, ""), "deploy@prod.example.com:2222", false},
		{"an IPv6 endpoint", ipv6, "deploy@[2001:db8::1]", false},
		{"a folder on this computer", editorRequest{editor: editorVSCode}, "", false},
		{"a port VS Code cannot carry", editorTestRequest(editorVSCode, 2222, ""), "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveEditorAuthority(test.request)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("resolveEditorAuthority = %q, %v; want %q, error %t", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestDefaultEditorIsChosenSavedAndRestored(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	if label, _ := state.sftpEditorItem(view, &view.sftpFolder); label != "" {
		t.Fatalf("a menu item was offered before an editor was chosen: %q", label)
	}
	state.openSettings()
	state.selectSettingsCategory(settingsFiles)
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.FilesEditor) || !hasHit(canvas, hitSettingsEditorZed) {
		t.Fatalf("the editor setting is not shown: %v", texts)
	}
	state.Click(hitSettingsEditorZed)
	saved, _, err := loadState(state.options.StatePath)
	if state.defaultEditor != editorZed || err != nil || saved.DefaultEditor != string(editorZed) {
		t.Fatalf("editor = %q, saved = %q, err = %v", state.defaultEditor, saved.DefaultEditor, err)
	}
	if restarted := newTestState(t, state.options); restarted.defaultEditor != editorZed {
		t.Fatalf("restored editor = %q", restarted.defaultEditor)
	}
	state.Click(hitSettingsEditorNone)
	if state.defaultEditor != editorNone || state.settingsEditorSelected() != hitSettingsEditorNone {
		t.Fatalf("editor = %q, selected = %d", state.defaultEditor, state.settingsEditorSelected())
	}
	state.defaultEditor = ExternalEditor("sublime")
	if selected := state.settingsEditorSelected(); selected != (ui.ID{}) {
		t.Fatalf("an unknown editor highlighted option %d", selected)
	}
	if description := state.settingsEditorDescription(); !strings.Contains(description, "sublime") {
		t.Fatalf("description = %q", description)
	}
	if label, _ := state.sftpEditorItem(view, &view.sftpFolder); label != "" {
		t.Fatalf("an unknown editor still offered a menu item: %q", label)
	}
}

func TestSFTPEditorItemOpensTheFolderInTheChosenEditor(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.defaultEditor = editorVSCode
	state.sshSync.file.Links = []sshSyncLink{
		{ID: session.host.CatalogID, Alias: "old", Detached: true},
		{ID: session.host.CatalogID},
		{ID: session.host.CatalogID, Alias: "prod"},
		{ID: session.host.CatalogID, Alias: "other"},
	}
	opened := make(chan string, 1)
	state.options.OpenExternalURI = func(link string) error {
		opened <- link
		return nil
	}
	if label, disabled := state.sftpEditorItem(view, view.active()); label != "Open in VS Code" || disabled {
		t.Fatalf("menu item = %q, disabled = %t", label, disabled)
	}
	view.active().headerMenuOpen = true
	state.Click(hitSFTPHeaderEditor)
	select {
	case link := <-opened:
		if want := "vscode://vscode-remote/ssh-remote+prod/home/test"; link != want {
			t.Fatalf("link = %q, want %q", link, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the editor was never asked to open the folder")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editorResults == nil })
}

func TestSFTPEditorUsesTheAliasTheSSHConfigNames(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.defaultEditor = editorVSCode
	config := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(config, []byte("Host "+session.host.Label+"\n\tUser chlo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.SSHConfigPath = config
	opened := make(chan string, 1)
	state.options.OpenExternalURI = func(link string) error {
		opened <- link
		return nil
	}
	view := session.files
	view.active().headerMenuOpen = true
	state.Click(hitSFTPHeaderEditor)
	select {
	case link := <-opened:
		if want := "vscode://vscode-remote/ssh-remote+" + session.host.Label + "/home/test"; link != want {
			t.Fatalf("link = %q, want %q", link, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the editor was never asked to open the folder")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editorResults == nil })
}

func TestSFTPEditorOpensTheFolderOnThisComputer(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.defaultEditor = editorVSCode
	dir, want := localEditorFolder()
	view.local = true
	view.lfolder.path = dir
	opened := make(chan string, 1)
	state.options.OpenExternalURI = func(link string) error {
		opened <- link
		return nil
	}
	if label, disabled := state.sftpEditorItem(view, &view.lfolder); label != "Open in VS Code" || disabled {
		t.Fatalf("menu item = %q, disabled = %t", label, disabled)
	}
	view.active().headerMenuOpen = true
	state.Click(hitSFTPHeaderEditor)
	select {
	case link := <-opened:
		if link != want {
			t.Fatalf("link = %q, want %q", link, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the editor was never asked to open the local folder")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editorResults == nil })
}
