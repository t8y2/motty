package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

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

func editorTestRequest(editor ExternalEditor, port uint16) editorRequest {
	return editorRequest{
		editor: editor, directory: "/srv/app",
		portFmt: i18n.For(i18n.English).Tools.FilesEditorPortFmt, jumpFmt: i18n.For(i18n.English).Tools.FilesEditorJump,
		host: editorHost{address: "prod.example.com", user: "deploy", systemUser: "me", port: port},
	}
}

// editorConfig writes an SSH config and points a request at it, looking for the
// names given.
func editorConfig(t *testing.T, request editorRequest, config string, aliases ...string) editorRequest {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	request.configPath, request.aliases = path, aliases
	return request
}

func TestResolveEditorAuthority(t *testing.T) {
	ipv6 := editorTestRequest(editorZed, 22)
	ipv6.host.address = "2001:db8::1"
	jump := editorTestRequest(editorZed, 22)
	jump.host.mottyJump = true
	hostile := editorTestRequest(editorZed, 22)
	hostile.host.user = "a/b"
	chinese := editorTestRequest(editorZed, 22)
	chinese.host.user = "\u5f20\u4e09"
	noAddress := editorTestRequest(editorZed, 22)
	noAddress.host.address = ""
	for _, test := range []struct {
		name    string
		request editorRequest
		want    string
		wantErr bool
	}{
		{"a port Zed can carry", editorTestRequest(editorZed, 2222), "deploy@prod.example.com:2222", false},
		{"an IPv6 endpoint", ipv6, "deploy@[2001:db8::1]", false},
		{"a folder on this computer", editorRequest{editor: editorVSCode, local: true}, "", false},
		{"a port VS Code cannot carry", editorTestRequest(editorVSCode, 2222), "", true},
		{"a jump host only Motty keeps", jump, "", true},
		{"a user that would change the URL", hostile, "", true},
		{"a user outside ASCII is escaped", chinese, "%E5%BC%A0%E4%B8%89@prod.example.com", false},
		{"a server with no address", noAddress, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveEditorAuthority(test.request)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("resolveEditorAuthority = %q, %v; want %q, error %t", got, err, test.want, test.wantErr)
			}
		})
	}
}

// A Host block that shares a name with the saved host is only used when it
// reaches the same machine, as the same user, on the same port: the editor's ssh
// would otherwise connect somewhere the file panel is not.
func TestAnSSHConfigAliasIsUsedOnlyForTheSameHost(t *testing.T) {
	const direct = "deploy@prod.example.com" // what a host the file does not name gets
	for _, test := range []struct {
		name    string
		config  string
		aliases []string
		want    string
	}{
		{"the same host", "Host prod\n\tHostName prod.example.com\n\tUser deploy\n", []string{"prod"}, "prod"},
		{"a host the file names only by its address", "Host prod.example.com\n\tUser deploy\n", []string{"prod.example.com"}, "prod.example.com"},
		{"the second name is the right one", "Host a\n\tHostName elsewhere\n\tUser deploy\nHost b\n\tHostName prod.example.com\n\tUser deploy\n", []string{"a", "b"}, "b"},
		{"a name in Chinese", "Host \u751f\u4ea7\n\tHostName prod.example.com\n\tUser deploy\n", []string{"\u751f\u4ea7"}, "%E7%94%9F%E4%BA%A7"},
		{"the system user is not the saved user", "Host prod\n\tHostName prod.example.com\n", []string{"prod"}, direct},
		{"another machine", "Host prod\n\tHostName other.example.com\n\tUser deploy\n", []string{"prod"}, direct},
		{"another user", "Host prod\n\tHostName prod.example.com\n\tUser root\n", []string{"prod"}, direct},
		{"another port", "Host prod\n\tHostName prod.example.com\n\tUser deploy\n\tPort 2222\n", []string{"prod"}, direct},
		{"no such block", "Host other\n\tHostName x\n", []string{"prod"}, direct},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := editorConfig(t, editorTestRequest(editorVSCode, 22), test.config, test.aliases...)
			if got, err := resolveEditorAuthority(request); err != nil || got != test.want {
				t.Fatalf("resolveEditorAuthority = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

// chooseEditor picks an editor in the settings select the way a user does.
func chooseEditor(t *testing.T, state *State, editor ExternalEditor) {
	t.Helper()
	state.Click(hitSettingsEditor)
	if !state.editorMenu.open {
		t.Fatal("the editor select did not open")
	}
	index := slices.Index(state.editorMenu.choices, editor)
	if index < 0 {
		t.Fatalf("%q is not offered: %v", editor, state.editorMenu.choices)
	}
	state.Click(hitSettingsEditorItems.At(index))
}

func TestDefaultEditorIsChosenSavedAndRestored(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.options.SchemeHandlerAvailable = func(string) (bool, error) { return true, nil }
	if label, _ := state.sftpEditorItem(view, &view.sftpFolder); label != "" {
		t.Fatalf("a menu item was offered before an editor was chosen: %q", label)
	}
	state.openSettings()
	state.selectSettingsCategory(settingsFiles)
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.FilesEditor) || !hasHit(canvas, hitSettingsEditor) {
		t.Fatalf("the editor setting is not shown: %v", texts)
	}
	chooseEditor(t, state, editorZed)
	saved, _, err := loadState(state.options.StatePath)
	if state.defaultEditor != editorZed || err != nil || saved.DefaultEditor != string(editorZed) {
		t.Fatalf("editor = %q, saved = %q, err = %v", state.defaultEditor, saved.DefaultEditor, err)
	}
	if restarted := newTestState(t, state.options); restarted.defaultEditor != editorZed {
		t.Fatalf("restored editor = %q", restarted.defaultEditor)
	}
	chooseEditor(t, state, editorNone)
	if state.defaultEditor != editorNone || state.editorSelected() != 0 {
		t.Fatalf("editor = %q, selected = %d", state.defaultEditor, state.editorSelected())
	}
}

func TestOnlyInstalledEditorsAreOffered(t *testing.T) {
	state := newTestState(t, testOptions(t))
	known := map[string]error{"vscode": nil, "zed": errors.New("no xdg-mime")}
	installed := map[string]bool{"vscode": false, "zed": false}
	state.options.SchemeHandlerAvailable = func(scheme string) (bool, error) { return installed[scheme], known[scheme] }

	// VS Code is not installed; the system cannot say whether Zed is.
	state.refreshEditors()
	if want := []ExternalEditor{editorNone, editorZed}; !slices.Equal(state.editorMenu.choices, want) {
		t.Fatalf("choices = %v, want %v", state.editorMenu.choices, want)
	}
	// An editor chosen earlier and since uninstalled stays visible, so the row
	// does not claim another one.
	state.defaultEditor = editorVSCode
	state.refreshEditors()
	if want := []ExternalEditor{editorNone, editorZed, editorVSCode}; !slices.Equal(state.editorMenu.choices, want) {
		t.Fatalf("choices = %v, want %v", state.editorMenu.choices, want)
	}
	if state.editorInstalled(editorVSCode) || state.editorSelected() != 2 {
		t.Fatalf("installed = %t, selected = %d", state.editorInstalled(editorVSCode), state.editorSelected())
	}
	// A value this build does not know highlights nothing and says so.
	state.defaultEditor = ExternalEditor("sublime")
	state.refreshEditors()
	if state.editorSelected() != -1 {
		t.Fatalf("an unknown editor selected choice %d", state.editorSelected())
	}
	if description := state.settingsEditorDescription(); !strings.Contains(description, "sublime") {
		t.Fatalf("description = %q", description)
	}
	if name := state.defaultEditor.name(state.messages.Tools); name != "" {
		t.Fatalf("an unknown editor has the name %q", name)
	}
}

// editorOpened records what the editor was asked to open.
type editorOpened struct{ scheme, link string }

func recordEditorOpens(state *State) chan editorOpened {
	opened := make(chan editorOpened, 1)
	state.options.OpenSchemeURL = func(scheme, link string) error {
		opened <- editorOpened{scheme, link}
		return nil
	}
	return opened
}

// writeEditorConfig makes the SSH config name the session's host.
func writeEditorConfig(t *testing.T, state *State, session *Session, alias string) {
	t.Helper()
	host := session.host
	config := filepath.Join(t.TempDir(), "config")
	body := fmt.Sprintf("Host %s\n\tHostName %s\n\tUser %s\n\tPort %d\n", alias, host.Address, state.hostCatalog.login(host), port22(host.Port))
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.SSHConfigPath = config
}

func awaitEditorOpen(t *testing.T, opened chan editorOpened) editorOpened {
	t.Helper()
	select {
	case got := <-opened:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the editor was never asked to open the folder")
		return editorOpened{}
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
	writeEditorConfig(t, state, session, "prod")
	opened := recordEditorOpens(state)
	if label, disabled := state.sftpEditorItem(view, view.active()); label != "Open in VS Code" || disabled {
		t.Fatalf("menu item = %q, disabled = %t", label, disabled)
	}
	view.active().headerMenuOpen = true
	state.Click(hitSFTPHeaderEditor)
	if got, want := awaitEditorOpen(t, opened), (editorOpened{"vscode", "vscode://vscode-remote/ssh-remote+prod/home/test"}); got != want {
		t.Fatalf("opened %+v, want %+v", got, want)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editorResults == nil })
}

func TestSFTPEditorUsesTheAliasTheSSHConfigNamesByLabel(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.defaultEditor = editorZed
	writeEditorConfig(t, state, session, session.host.Label)
	opened := recordEditorOpens(state)
	view := session.files
	view.active().headerMenuOpen = true
	state.Click(hitSFTPHeaderEditor)
	if got, want := awaitEditorOpen(t, opened), (editorOpened{"zed", "zed://ssh/" + session.host.Label + "/home/test"}); got != want {
		t.Fatalf("opened %+v, want %+v", got, want)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editorResults == nil })
}

func TestSFTPEditorWontOpenAHostBehindAJumpHostOnlyMottyKnows(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.defaultEditor = editorZed
	state.proxies = []proxyRecord{{ID: 1, TargetID: session.host.ID, JumpID: session.host.ID + 1}}
	opened := recordEditorOpens(state)
	view := session.files
	view.active().headerMenuOpen = true
	state.Click(hitSFTPHeaderEditor)
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editorResults == nil })
	select {
	case got := <-opened:
		t.Fatalf("the editor was asked to open %+v and would connect without the jump host", got)
	default:
	}
}

func TestSFTPEditorOpensTheFolderOnThisComputer(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.defaultEditor = editorVSCode
	dir, want := localEditorFolder()
	view.local = true
	view.lfolder.path = dir
	opened := recordEditorOpens(state)
	if label, disabled := state.sftpEditorItem(view, &view.lfolder); label != "Open in VS Code" || disabled {
		t.Fatalf("menu item = %q, disabled = %t", label, disabled)
	}
	view.active().headerMenuOpen = true
	state.Click(hitSFTPHeaderEditor)
	if got := awaitEditorOpen(t, opened); got != (editorOpened{"vscode", want}) {
		t.Fatalf("opened %+v, want %q", got, want)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editorResults == nil })
}
