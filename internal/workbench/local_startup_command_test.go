package workbench

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
)

// localStartupSession is the login-command fixture made into a local shell: the
// fake shell, its prompt token and the clock are the same, but the session runs
// this machine's shell and a startup command was configured for it.
func localStartupSession(t *testing.T, command string) (*State, *Session, *fakeShell, *time.Duration) {
	t.Helper()
	state, shell, now := loginCommandSession(t, "")
	session := &state.sessions[0]
	session.HostID = localHostID
	session.host = localHost("Local")
	session.conn = &localConn{shell: "wsl:Ubuntu"}
	state.localStartupCommands = map[string]string{localStartupKey("wsl:Ubuntu"): command}
	session.term.armInitialization(state, session)
	return state, session, shell, now
}

// wakeLocalStartup parses what the shell said and runs the login rule, as one
// wake of the window would.
func wakeLocalStartup(state *State, session *Session) {
	state.pump(session)
	state.runLoginCommand(session)
}

// A local shell starts with the integration, so its first prompt has no
// command-start marker before it (issue 1225: a command set to run at startup
// never ran).
func TestLocalShellStartupCommandRunsAtItsFirstPrompt(t *testing.T) {
	state, session, shell, _ := localStartupSession(t, "export LANG=zh_CN.UTF-8")
	token := session.term.folderToken
	shell.say("Last login: today\r\n")
	wakeLocalStartup(state, session)
	if shell.typed() != "" {
		t.Fatalf("typed before the prompt: %q", shell.typed())
	}
	shell.say("\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	wakeLocalStartup(state, session)
	if shell.typed() != "export LANG=zh_CN.UTF-8\r" {
		t.Fatalf("typed %q once the prompt ended", shell.typed())
	}
	if session.term.login.line != "" {
		t.Fatal("the startup command stayed armed after it ran")
	}
}

// A shell family the integration does not know never reports a prompt: its
// startup command follows once it stops talking.
func TestLocalShellStartupCommandFallsBackToQuiet(t *testing.T) {
	state, session, shell, now := localStartupSession(t, "cd /srv")
	shell.say("$ ")
	wakeLocalStartup(state, session)
	quiet := state.loginQuiet(session)
	*now += quiet / 2
	wakeLocalStartup(state, session)
	if shell.typed() != "" {
		t.Fatalf("typed while the shell was still starting: %q", shell.typed())
	}
	*now += quiet
	wakeLocalStartup(state, session)
	if shell.typed() != "cd /srv\r" {
		t.Fatalf("typed %q after the shell stayed quiet", shell.typed())
	}
}

// A user who is already typing is not interrupted: their line comes first.
func TestLocalShellStartupCommandYieldsToTypedInput(t *testing.T) {
	state, session, shell, _ := localStartupSession(t, "cd /srv")
	token := session.term.folderToken
	state.typed(session, []byte("ls"))
	shell.say("\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	wakeLocalStartup(state, session)
	if shell.typed() != "ls" {
		t.Fatalf("the startup command was typed over the user's line: %q", shell.typed())
	}
	if session.term.login.line != "" {
		t.Fatal("the startup command stayed armed after yielding")
	}
}

// A reported prompt that never ends, or a command already submitted, must not
// fall back to typing after the quiet wait.
func TestLocalShellStartupCommandDoesNotFallBackAfterPromptStartOrUserSubmit(t *testing.T) {
	state, session, shell, now := localStartupSession(t, "echo startup")
	token := session.term.folderToken
	shell.say("\x1b]133;A;motty=" + token + "\x07")
	wakeLocalStartup(state, session)
	*now += time.Minute
	wakeLocalStartup(state, session)
	if shell.typed() != "" {
		t.Fatal("quiet fallback typed into an unfinished integrated prompt")
	}
	state.typed(session, []byte("echo user\r"))
	shell.say("\x1b]133;B;motty=" + token + "\x07")
	wakeLocalStartup(state, session)
	if shell.typed() != "echo user\r" || session.term.login.line != "" {
		t.Fatal("startup command survived user input and reached the running command")
	}
}

// The command a local shell types belongs to the shell the tab actually runs:
// the tab's own choice, or the settings' default. A command saved for one shell
// must not run in another.
func TestLocalShellStartupCommandIsArmedForItsOwnShell(t *testing.T) {
	state := newTestState(t, testOptions(t))
	program := filepath.Join(t.TempDir(), "zsh")
	state.localStartupCommands = map[string]string{
		localStartupKey(program):      "export LANG=zh_CN.UTF-8",
		localStartupKey("wsl:Ubuntu"): "echo Ubuntu",
	}
	for _, test := range []struct{ shell, want string }{
		{program, "export LANG=zh_CN.UTF-8"},
		{filepath.Join(t.TempDir(), "bash"), ""},
		{"wsl:Ubuntu", "echo Ubuntu"},
		{"wsl:Debian", ""},
	} {
		session := &Session{HostID: localHostID, host: localHost("Local"), term: &terminal{}, conn: &localConn{shell: test.shell}}
		session.term.armInitialization(state, session)
		if got := session.term.login.line; got != test.want {
			t.Errorf("a %q shell armed %q, want %q", test.shell, got, test.want)
		}
	}
}

// A saved command is kept only when it could have been typed as one line, and a
// shell that is no longer detected keeps its command for when it comes back.
func TestLocalStartupCommandsKeepOnlyTypedLines(t *testing.T) {
	program := filepath.Join(t.TempDir(), "zsh")
	got := cleanLocalStartupCommands(map[string]string{
		program:                       "  export LANG=zh_CN.UTF-8  ",
		localStartupKey("wsl:Ubuntu"): "echo Ubuntu",
		localStartupKey("wsl:Debian"): "one\ntwo",
		localStartupKey("wsl:Fedora"): "",
		"relative":                    "echo bad",
	})
	if len(got) != 2 || got[localStartupKey(program)] != "export LANG=zh_CN.UTF-8" || got[localStartupKey("wsl:Ubuntu")] != "echo Ubuntu" {
		t.Fatalf("unsafe or missing restored commands: %v", got)
	}
}

// startupSettingsState is a state on the terminal settings page with Alpha and
// Beta installed, so the dialog and its settings row can be driven.
func startupSettingsState(t *testing.T, options Options) (*State, []localshell.Choice) {
	t.Helper()
	state := newTestState(t, options)
	choices := []localshell.Choice{
		{Name: "Alpha", Path: filepath.Join(t.TempDir(), "sh"), Default: true},
		{Name: "Beta", Path: filepath.Join(t.TempDir(), "bash")},
	}
	withShellChoices(t, state, choices, nil)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	return state, choices
}

func paintStartupSettings(state *State) {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 2400})
}

// The terminal settings row opens the dialog for the default shell, and Save
// stores what was typed for that shell only.
func TestLocalStartupCommandDialogSavesTheShellItShows(t *testing.T) {
	options := freshOptions(t)
	state, choices := startupSettingsState(t, options)
	state.localShell = choices[1].Path // the default shell, and the one the dialog opens on
	paintStartupSettings(state)
	state.Click(hitSettingsLocalStartupOpen)
	if state.modal != modalLocalStartupCommand || state.localStartupDialog.shell.Path != choices[1].Path {
		t.Fatalf("the dialog opened on %q, want %q", state.localStartupDialog.shell.Path, choices[1].Path)
	}
	paintStartupSettings(state)
	input := state.WindowConfig(app.Config{}).TextInput(app.TextInputContext{ID: hitLocalStartupField})
	if input == nil || input.Editor == nil {
		t.Fatal("the dialog's command field has no page editor")
	}
	input.Editor.Insert("echo Beta")
	if got := input.Editor.String(); got != "echo Beta" {
		t.Fatalf("the draft is %q", got)
	}
	state.Click(hitLocalStartupSubmit)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("save left modal %v, error %q", state.modal, state.dialogError)
	}
	if got := state.localStartupCommand(choices[1].Path); got != "echo Beta" {
		t.Fatalf("the typed command was stored as %q", got)
	}
	if got := state.localStartupCommand(choices[0].Path); got != "" {
		t.Fatalf("typing for one shell stored %q for another", got)
	}
	reopened := newTestState(t, options)
	if got := reopened.localStartupCommand(choices[1].Path); got != "echo Beta" {
		t.Fatalf("the command came back as %q", got)
	}
}

// An invalid line keeps the dialog open with its error and the stored command
// intact; Cancel discards the draft.
func TestLocalStartupCommandDialogKeepsInvalidDrafts(t *testing.T) {
	state, choices := startupSettingsState(t, freshOptions(t))
	state.setLocalStartupCommand(choices[0].Path, "echo saved")
	paintStartupSettings(state)
	state.Click(hitSettingsLocalStartupOpen)
	paintStartupSettings(state)
	state.localStartupDialog.editor.Insert("one\ntwo")
	state.Click(hitLocalStartupSubmit)
	if state.modal != modalLocalStartupCommand || state.dialogError != state.messages.SettingsLocalStartupInvalid {
		t.Fatalf("an invalid line was accepted: modal %v, error %q", state.modal, state.dialogError)
	}
	if got := state.localStartupCommand(choices[0].Path); got != "echo saved" {
		t.Fatalf("an invalid line replaced the stored command: %q", got)
	}
	state.Click(hitLocalStartupCancel)
	if state.modal != modalNone || state.localStartupCommand(choices[0].Path) != "echo saved" {
		t.Fatal("Cancel did not discard the draft")
	}
}

// The gear beside a shell in the Local Terminal menu opens the dialog on that
// shell, so any installed shell can be configured from where it is chosen.
func TestLocalShellPickerGearOpensStartupCommandDialog(t *testing.T) {
	state, choices := startupSettingsState(t, freshOptions(t))
	state.localShellPicker.open = true
	state.Click(hitLocalShellPickerSetup.At(1))
	if state.modal != modalLocalStartupCommand || state.localStartupDialog.shell.Path != choices[1].Path || state.localShellPicker.open {
		t.Fatalf("the gear opened %q in modal %v (picker open %v), want %q",
			state.localStartupDialog.shell.Path, state.modal, state.localShellPicker.open, choices[1].Path)
	}
}

// The saved commands are this process's, one per shell: a save reaches the other
// windows, while a command already armed for a running shell keeps its value.
func TestLocalStartupCommandEditsReachOtherWindowsWithoutChangingLiveInitialization(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	opened := tabOrder(pair.second)
	program := "wsl:Ubuntu"
	pair.first.setLocalStartupCommand(program, "echo first")
	local := &Session{HostID: localHostID, term: &terminal{}, conn: &localConn{shell: program}}
	local.term.armInitialization(pair.first, local)
	before := pair.first.snapshot()
	pair.first.setLocalStartupCommand(program, "echo second")
	if before.LocalStartupCommands[localStartupKey(program)] != "echo first" {
		t.Fatal("editing mutated the previous persistence snapshot")
	}
	if pair.second.localStartupCommand(program) != "echo second" {
		t.Fatal("another window kept stale command configuration")
	}
	if local.term.login.line != "echo first" {
		t.Fatalf("an armed command changed with the settings: %q", local.term.login.line)
	}
	pair.first.setLocalStartupCommand(program, "")
	if pair.second.localStartupCommand(program) != "" {
		t.Fatal("another window did not adopt a disabled startup command")
	}
	if !slices.Equal(opened, tabOrder(pair.second)) {
		t.Fatal("the other window opened or lost tabs while only this device's settings changed")
	}
	if first := pair.first.snapshot(); first.LocalStartupCommands[localStartupKey(program)] != "" {
		t.Fatal("the live snapshot still carries a cleared command")
	}
}
