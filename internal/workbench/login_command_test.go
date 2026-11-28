package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

// loginCommandSession connects a host that has a login command, with a clock the
// test moves.
func loginCommandSession(t *testing.T, command string) (*State, *fakeShell, *time.Duration) {
	t.Helper()
	return loginCommandSessionWithPolicy(t, command, "")
}

func loginCommandSessionWithPolicy(t *testing.T, command, policy string) (*State, *fakeShell, *time.Duration) {
	t.Helper()
	world, options := newWorld(t)
	hosts := append([]savedHost(nil), sampleHosts...)
	hosts[0].savedTransport.LoginCommand = command
	hosts[0].savedTransport.LoginCommandPolicy = policy
	if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	now := new(time.Duration)
	options.Now = func() time.Duration { return *now }
	options.Invalidate = func() {}
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	return state, fake.shell(t), now
}

func TestLoginCommandWaitsForThePromptTheShellReports(t *testing.T) {
	state, shell, _ := loginCommandSession(t, "sudo -i")
	token := state.sessions[0].term.folderToken

	shell.say("Last login: today\r\n")
	state.Wake()
	if shell.typed() != "" {
		t.Fatalf("typed before the shell was ready: %q", shell.typed())
	}

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("typed %q once the prompt was ready", shell.typed())
	}

	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("typed again: %q", shell.typed())
	}
}

// A PowerShell integration wraps the shell's prompt, as Windows Terminal does:
// every prompt reports its start, the folder and its end, and none is preceded
// by a command start, because the shell has no hook before a line runs. The
// first such prompt is the shell waiting for input, which is all a login
// command needs to be typed.
func TestLoginCommandRunsInAShellThatReportsItsPromptButNotCommandStarts(t *testing.T) {
	state, shell, _ := loginCommandSession(t, "cd /tmp/projects")
	token := state.sessions[0].term.folderToken
	prompt := "\x1b]133;A;motty=" + token + "\x07PS C:\\Users\\me> \x1b]7;file://box/C:/Users/me\x07\x1b]133;B;motty=" + token + "\x07"

	shell.say(prompt)
	state.Wake()
	if shell.typed() != "cd /tmp/projects\r" {
		t.Fatalf("typed %q once the prompt was ready", shell.typed())
	}

	// The next prompt redraws with nothing new to send: a single-line command is
	// handed to the shell once.
	shell.say("cd /tmp/projects\r\n" + prompt)
	state.Wake()
	if shell.typed() != "cd /tmp/projects\r" {
		t.Fatalf("typed the command again: %q", shell.typed())
	}
}

// Without a command start there is no exit report either, so a multi-line login
// command advances when the shell's prompt comes back: the prompt function runs
// only while the shell waits for a line, so its report follows the last command.
func TestMultilineLoginCommandRunsStepByStepWithoutCommandReports(t *testing.T) {
	state, shell, _ := loginCommandSession(t, "cd /tmp/projects\ngit status")
	token := state.sessions[0].term.folderToken
	prompt := "\x1b]133;A;motty=" + token + "\x07PS C:\\Users\\me> \x1b]7;file://box/C:/Users/me\x07\x1b]133;B;motty=" + token + "\x07"

	shell.say(prompt)
	state.Wake()
	if shell.typed() != "cd /tmp/projects\r" {
		t.Fatalf("typed %q, want only the first line", shell.typed())
	}

	// The first line's own echo is not its completion.
	shell.say("cd /tmp/projects\r\n")
	state.Wake()
	if shell.typed() != "cd /tmp/projects\r" {
		t.Fatalf("sent the second line before the prompt came back: %q", shell.typed())
	}

	shell.say(prompt)
	state.Wake()
	if shell.typed() != "cd /tmp/projects\rgit status\r" {
		t.Fatalf("typed %q, want the second line after its prompt", shell.typed())
	}
}

func TestLoginCommandFallsBackToQuietWhenNoPromptIsReported(t *testing.T) {
	state, shell, now := loginCommandSession(t, "cd /srv")
	shell.say("banner\r\n$ ")
	state.Wake()
	*now += loginCommandGrace / 2
	state.Wake()
	if shell.typed() != "" {
		t.Fatalf("typed while the shell might still announce a prompt: %q", shell.typed())
	}
	*now += loginCommandGrace
	state.Wake()
	if shell.typed() != "cd /srv\r" {
		t.Fatalf("typed %q after the shell stayed quiet", shell.typed())
	}
}

func TestLoginInteractivePromptReadyRequiresKnownPrompt(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   bool
	}{
		{name: "SSH password prompt", prompt: "root@example.com's password:", want: true},
		{name: "GateShell bracketed SSH password prompt", prompt: "[root@10.169.156.26:22] Password:", want: true},
		{name: "GateShell pipe-prefixed SSH password prompt", prompt: "| [root@10.169.156.26:22] Password:", want: true},
		{name: "sudo password prompt", prompt: "[sudo] password for root:", want: true},
		{name: "passphrase prompt", prompt: "Enter passphrase for key '/home/user/.ssh/id_ed25519':", want: true},
		{name: "Chinese password prompt", prompt: "请输入密码：", want: true},
		{name: "password status is not a prompt", prompt: "Password expires in 30 days:", want: false},
		{name: "yes no question", prompt: "Are you sure you want to continue? (yes/no)", want: true},
		{name: "yes no shorthand", prompt: "Continue? [y/N]", want: true},
		{name: "press enter", prompt: "Press Enter to continue", want: true},
		{name: "press return", prompt: "Press Return to continue", want: true},
		{name: "press any key", prompt: "Press any key to continue", want: true},
		{name: "Chinese press enter", prompt: "按回车键继续", want: true},
		{name: "Chinese continue", prompt: "是否继续？", want: true},
		{name: "Chinese selection", prompt: "请选择操作", want: true},
		{name: "Chinese input", prompt: "请输入命令", want: true},
		{name: "verification code is secret", prompt: "请输入验证码", want: false},
		{name: "token prompt is secret", prompt: "API token:", want: false},
		{name: "REPL prompt", prompt: ">>> ", want: true},
		{name: "database prompt", prompt: "mysql> ", want: true},
		{name: "postgres prompt", prompt: "postgres=# ", want: true},
		{name: "pager prompt", prompt: "--More--", want: false},
		{name: "pager end prompt", prompt: "(END)", want: false},
		{name: "Vim colon prompt", prompt: ":", want: false},
		{name: "unknown angle prompt", prompt: "deploy>", want: false},
		{name: "connection status ending in colon", prompt: "Connecting to host:", want: false},
		{name: "prompt phrase in log but not at row end", prompt: "Step 3/5: running tests, please press enter to continue later", want: false},
		{name: "Chinese prompt word in log", prompt: "Step 3/5: running tests, please select... 请选择", want: false},
		{name: "yes no cue in log", prompt: "Build log: [y/n] will be shown later", want: false},
		{name: "generic question", prompt: "Are you sure?", want: false},
		{name: "generic angle bracket", prompt: "loading >", want: false},
		{name: "incomplete prompt marker alone", prompt: "broken prompt>", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, shell, now := loginCommandSession(t, "")
			term := state.sessions[0].term
			shell.say(tt.prompt)
			state.Wake()
			term.login.seen = true
			term.login.last = *now
			term.promptStarted = true // A prompt-start marker alone is not enough.
			term.promptReady = false
			term.promptDirty = false
			if got := state.loginInteractivePromptReady(term); got != tt.want {
				t.Fatalf("loginInteractivePromptReady(%q) = %t, want %t", tt.prompt, got, tt.want)
			}
		})
	}
}

func TestLoginCommandStaysOutOfTheCommandHistory(t *testing.T) {
	state, shell, _ := loginCommandSession(t, "sudo -i")
	var remembered []string
	state.sessions[0].term.onCommand = func(command string) { remembered = append(remembered, command) }
	token := state.sessions[0].term.folderToken
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("typed %q", shell.typed())
	}
	if len(remembered) != 0 {
		t.Fatalf("the login command was offered to the command history: %q", remembered)
	}
}

func TestSingleLineLoginCommandSupportsFullScreenProgramsWithoutTimeoutOrAbort(t *testing.T) {
	state, shell, now := loginCommandSession(t, "tmux attach")
	session := &state.sessions[0]
	token := session.term.folderToken

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "tmux attach\r" {
		t.Fatalf("typed %q, want %q", shell.typed(), "tmux attach\r")
	}

	// tmux attach enters alternate screen mode (smcup / \x1b[?1049h).
	shell.say("\x1b[?1049h")
	state.Wake()
	if !session.term.screen.Modes().AltScreen {
		t.Fatal("terminal did not enter AltScreen")
	}

	// Must preserve legacy fire-and-forget: no abort on entering AltScreen.
	if session.LoginCommandAborted() != "" {
		t.Fatalf("aborted on entering AltScreen: %q", session.LoginCommandAborted())
	}

	// 40 seconds pass (longer than loginCommandExecTimeout = 30s) while staying in AltScreen.
	*now += loginCommandExecTimeout + 10*time.Second
	state.Wake()

	// Must not abort on timeout.
	if session.LoginCommandAborted() != "" {
		t.Fatalf("aborted after timeout: %q", session.LoginCommandAborted())
	}
	if session.term.login.running {
		t.Fatal("single-line login command left in running state")
	}
	if state.dialogError != "" {
		t.Fatalf("dialogError = %q, want empty", state.dialogError)
	}
	screenText := string(session.term.screen.AppendRetainedText(nil))
	if strings.Contains(screenText, "提示符异常") || strings.Contains(screenText, "超时") || strings.Contains(screenText, "abnormal") || strings.Contains(screenText, "timed out") {
		t.Fatalf("failure notice written to screen: %q", screenText)
	}
}

func TestMultilineLoginCommandLastStepCanBeFullScreenProgramWithoutTimeoutOrAbort(t *testing.T) {
	state, shell, now := loginCommandSession(t, "cd /opt/app\ntmux attach")
	session := &state.sessions[0]
	token := session.term.folderToken

	// Initial prompt triggers step 1.
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "cd /opt/app\r" {
		t.Fatalf("step 1 typed %q", shell.typed())
	}

	// Step 1 finishes -> step 2 (tmux attach) is sent.
	shell.say("cd /opt/app\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	wantTyped := "cd /opt/app\rtmux attach\r"
	if shell.typed() != wantTyped {
		t.Fatalf("step 2 typed %q, want %q", shell.typed(), wantTyped)
	}

	// Step 2 enters AltScreen and stays there.
	shell.say("\x1b[?1049h")
	state.Wake()
	if !session.term.screen.Modes().AltScreen {
		t.Fatal("terminal did not enter AltScreen")
	}

	// Must not abort on entering AltScreen when no further steps are pending.
	if session.LoginCommandAborted() != "" {
		t.Fatalf("aborted on entering AltScreen: %q", session.LoginCommandAborted())
	}

	// 40 seconds pass (longer than loginCommandExecTimeout = 30s) while staying in AltScreen.
	*now += loginCommandExecTimeout + 10*time.Second
	state.Wake()

	// Must not abort on timeout when no further steps are pending.
	if session.LoginCommandAborted() != "" {
		t.Fatalf("aborted after timeout: %q", session.LoginCommandAborted())
	}
	if session.term.login.running {
		t.Fatal("login command left in running state")
	}
	results := session.LoginCommandResults()
	if len(results) < 1 || results[0].Command != "cd /opt/app" || !results[0].Success {
		t.Fatalf("step 1 result missing or failed: %+v", results)
	}
}

func TestHostFormSavesAndReEditsMultilineLoginCommandAndPolicy(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Edge")
	typeInto(state, hitDialogAddress, "edge.example.com")
	typeInto(state, hitDialogUser, "ops")
	script := "cd /opt/app\r\n# init locale\r\nexport LANG=C.UTF-8\r\n./check.sh\r\nsystemctl status app --no-pager"
	typeInto(state, hitDialogLoginCommand, script)
	// Enter inside the multiline login command box must not submit the host form.
	if res := state.Key(gfx.KeyEvent{Target: hitDialogLoginCommand, Key: gfx.KeyEnter}); res.Handled || state.modal != modalNewHost {
		t.Fatalf("Enter in multiline login command submitted form: res=%+v modal=%v", res, state.modal)
	}
	state.Click(hitDialogLoginCommandPolicy)
	state.Click(hitDialogSubmit)
	if state.modal == modalNewHost {
		t.Fatalf("form refused: %q", state.dialogError)
	}
	added := state.hosts[len(state.hosts)-1]
	wantScript := "cd /opt/app\n# init locale\nexport LANG=C.UTF-8\n./check.sh\nsystemctl status app --no-pager"
	if added.LoginCommand != wantScript || added.LoginCommandPolicy != loginFailureContinue {
		t.Fatalf("saved host = %+v", added)
	}
	if got := added.transport(); got.LoginCommand != wantScript || got.LoginCommandPolicy != "continue" {
		t.Fatalf("persisted %+v", got)
	}

	// Re-open the host in edit mode and verify echo + re-editing.
	reloaded := newTestState(t, options)
	editedHost := reloaded.hosts[len(reloaded.hosts)-1]
	reloaded.openEditHostID(editedHost.ID)
	if !reloaded.editor.advancedOpen {
		t.Fatal("advanced section did not open for host with login command")
	}
	if got := reloaded.editor.value(fieldLoginCommand); got != wantScript {
		t.Fatalf("echoed login command = %q, want %q", got, wantScript)
	}
	if reloaded.editor.loginFailurePolicy != loginFailureContinue {
		t.Fatalf("echoed policy = %v, want %v", reloaded.editor.loginFailurePolicy, loginFailureContinue)
	}
}

func TestALoginCommandWithControlCharactersOrExcessLengthIsRefusedAndNotRestored(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Edge")
	typeInto(state, hitDialogAddress, "edge.example.com")
	typeInto(state, hitDialogLoginCommand, "sudo -i\x03")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrLoginCommand {
		t.Fatalf("a command with control characters was accepted: modal=%v error=%q", state.modal, state.dialogError)
	}
	if restoredLoginCommand("sudo -i\x03") != "" || restoredLoginCommand("  one\r\ntwo  ") != "one\ntwo" {
		t.Fatal("a saved login command is not read back as the form would have it")
	}
}

func TestMultilineLoginCommandExecutesLineByLineInOrderAndIgnoresBlankAndCommentLines(t *testing.T) {
	script := "\n# switch working directory\ncd /opt/app\n\n  # set locale\nexport LANG=C.UTF-8\n./check.sh --env=\"prod\" | grep -v 'debug' > /tmp/check.log 2>&1\nsystemctl status app --no-pager\n"
	state, shell, _ := loginCommandSession(t, script)
	session := &state.sessions[0]
	token := session.term.folderToken

	prompt := func(code int, output string) {
		var chunk string
		if output != "" {
			chunk += output
		}
		chunk += "\x1b]133;D;" + strconvItoa(code) + ";motty=" + token + "\x07"
		chunk += "\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07"
		shell.say(chunk)
		state.Wake()
	}

	// Initial prompt after login triggers only the 1st command.
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	want1 := "cd /opt/app\r"
	if shell.typed() != want1 {
		t.Fatalf("after initial prompt typed %q, want %q", shell.typed(), want1)
	}

	// Extra wakes before command 1 finishes must not send command 2 early.
	shell.say("cd /opt/app\r\n")
	state.Wake()
	if shell.typed() != want1 {
		t.Fatalf("sent 2nd command before 1st finished: %q", shell.typed())
	}

	// Command 1 finishes -> command 2 is sent.
	prompt(0, "")
	want2 := want1 + "export LANG=C.UTF-8\r"
	if shell.typed() != want2 {
		t.Fatalf("after step 1 finished typed %q, want %q", shell.typed(), want2)
	}

	// Command 2 finishes -> command 3 (with quotes, pipes, redirection) is sent verbatim.
	prompt(0, "export LANG=C.UTF-8\r\n")
	want3 := want2 + "./check.sh --env=\"prod\" | grep -v 'debug' > /tmp/check.log 2>&1\r"
	if shell.typed() != want3 {
		t.Fatalf("after step 2 finished typed %q, want %q", shell.typed(), want3)
	}

	// Command 3 finishes with output -> command 4 is sent.
	prompt(0, "./check.sh --env=\"prod\" | grep -v 'debug' > /tmp/check.log 2>&1\r\ncheck passed\r\n")
	want4 := want3 + "systemctl status app --no-pager\r"
	if shell.typed() != want4 {
		t.Fatalf("after step 3 finished typed %q, want %q", shell.typed(), want4)
	}

	// Command 4 finishes with output.
	prompt(0, "systemctl status app --no-pager\r\nActive: active (running)\r\n")
	if shell.typed() != want4 {
		t.Fatalf("unexpected extra input after 4th command: %q", shell.typed())
	}

	results := session.LoginCommandResults()
	if len(results) != 4 {
		t.Fatalf("results count = %d, want 4: %+v", len(results), results)
	}
	wantCommands := []string{
		"cd /opt/app",
		"export LANG=C.UTF-8",
		"./check.sh --env=\"prod\" | grep -v 'debug' > /tmp/check.log 2>&1",
		"systemctl status app --no-pager",
	}
	for i, r := range results {
		if r.Step != i+1 || r.Total != 4 || r.Command != wantCommands[i] || !r.Success || r.ExitCode != 0 {
			t.Fatalf("result[%d] = %+v", i, r)
		}
	}
	if results[2].Output != "check passed" {
		t.Fatalf("result[2] output = %q, want %q", results[2].Output, "check passed")
	}
	if results[3].Output != "Active: active (running)" {
		t.Fatalf("result[3] output = %q, want %q", results[3].Output, "Active: active (running)")
	}
}

func TestMultilineLoginCommandStopsOnFailureByDefaultAndShowsFailedCommandOutputAndExitCode(t *testing.T) {
	script := "cd /opt/app\n./check.sh\nsystemctl status app --no-pager"
	state, shell, _ := loginCommandSession(t, script)
	session := &state.sessions[0]
	token := session.term.folderToken

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "cd /opt/app\r" {
		t.Fatalf("step 1 typed %q", shell.typed())
	}

	// Step 1 succeeds -> Step 2 (./check.sh) is sent.
	shell.say("cd /opt/app\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	wantTyped := "cd /opt/app\r./check.sh\r"
	if shell.typed() != wantTyped {
		t.Fatalf("step 2 typed %q, want %q", shell.typed(), wantTyped)
	}

	// Step 2 fails with exit code 2 and error output.
	shell.say("./check.sh\r\nERROR: database unreachable\r\n\x1b]133;D;2;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()

	// Step 3 must NOT be executed under default stop-on-failure policy.
	if shell.typed() != wantTyped {
		t.Fatalf("executed step 3 after failure: %q", shell.typed())
	}
	results := session.LoginCommandResults()
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 steps", results)
	}
	failed := results[1]
	if failed.Step != 2 || failed.Total != 3 || failed.Command != "./check.sh" || failed.ExitCode != 2 || failed.Success || failed.Output != "ERROR: database unreachable" {
		t.Fatalf("failed result = %+v", failed)
	}
	screenText := string(session.term.screen.AppendRetainedText(nil))
	for _, piece := range []string{"./check.sh", "2", "ERROR: database unreachable"} {
		if !strings.Contains(screenText, piece) || !strings.Contains(state.status, piece) {
			t.Fatalf("missing %q in screen=%q or status=%q", piece, screenText, state.status)
		}
	}
}

func TestMultilineLoginCommandContinuesOnFailureWhenConfigured(t *testing.T) {
	script := "cd /opt/app\n./check.sh\nsystemctl status app --no-pager"
	state, shell, _ := loginCommandSessionWithPolicy(t, script, "continue")
	session := &state.sessions[0]
	token := session.term.folderToken

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()

	// Step 1 succeeds -> Step 2 is sent.
	shell.say("cd /opt/app\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()

	// Step 2 fails with exit code 2 -> Step 3 is still sent because policy is continue!
	shell.say("./check.sh\r\nWARN: check returned non-zero\r\n\x1b]133;D;2;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	wantTyped := "cd /opt/app\r./check.sh\rsystemctl status app --no-pager\r"
	if shell.typed() != wantTyped {
		t.Fatalf("typed %q, want %q", shell.typed(), wantTyped)
	}

	// Step 3 finishes with exit code 0.
	shell.say("systemctl status app --no-pager\r\nactive\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()

	results := session.LoginCommandResults()
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3 steps", results)
	}
	if results[0].ExitCode != 0 || !results[0].Success {
		t.Fatalf("step 1 = %+v", results[0])
	}
	if results[1].ExitCode != 2 || results[1].Success || results[1].Output != "WARN: check returned non-zero" {
		t.Fatalf("step 2 = %+v", results[1])
	}
	if results[2].ExitCode != 0 || !results[2].Success || results[2].Output != "active" {
		t.Fatalf("step 3 = %+v", results[2])
	}
}

func TestMultilineLoginCommandAbortsOnTimeoutAndDisconnectButContinuesAtPrompt(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		state, shell, now := loginCommandSession(t, "sleep 60\necho after")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "sleep 60\r" {
			t.Fatalf("typed %q", shell.typed())
		}
		shell.say("sleep 60\r\nstill running...\r\n")
		state.Wake()
		*now += loginCommandExecTimeout
		state.Wake()

		if session.LoginCommandAborted() == "" || !strings.Contains(session.LoginCommandAborted(), "sleep 60") {
			t.Fatalf("expected timeout abort notice, got %q", session.LoginCommandAborted())
		}
		// A late prompt after timeout must not run the remaining command.
		shell.say("\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "sleep 60\r" {
			t.Fatalf("ran remaining command after timeout: %q", shell.typed())
		}
	})

	t.Run("disconnect", func(t *testing.T) {
		state, shell, _ := loginCommandSession(t, "cd /opt/app\necho second")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "cd /opt/app\r" {
			t.Fatalf("typed %q", shell.typed())
		}
		state.closeDisconnectedShell(session)
		if session.LoginCommandAborted() == "" {
			t.Fatal("expected disconnect abort notice")
		}
		if session.term.login.line != "" || session.term.login.running {
			t.Fatal("pending login commands survived disconnect")
		}
	})

	t.Run("allowlisted interactive prompts", func(t *testing.T) {
		prompts := []struct {
			name string
			text string
		}{
			{name: "yes no", text: "Are you sure? (yes/no)"},
			{name: "press enter", text: "Press Enter to continue"},
			{name: "Chinese continue", text: "是否继续？"},
			{name: "REPL", text: ">>> "},
		}
		for _, prompt := range prompts {
			t.Run(prompt.name, func(t *testing.T) {
				state, shell, _ := loginCommandSession(t, "sudo -i\necho after")
				session := &state.sessions[0]
				token := session.term.folderToken

				shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
				state.Wake()
				if shell.typed() != "sudo -i\r" {
					t.Fatalf("typed %q", shell.typed())
				}
				shell.say("\x1b]133;A;motty=" + token + "\x07\r\n" + prompt.text)
				state.Wake()
				state.Wake()
				if session.LoginCommandAborted() != "" {
					t.Fatalf("prompt aborted remaining commands: %q", session.LoginCommandAborted())
				}
				if shell.typed() != "sudo -i\recho after\r" {
					t.Fatalf("next prompt answer was not sent: %q", shell.typed())
				}
			})
		}
	})

	t.Run("sudo password prompt sends one secret without recording it", func(t *testing.T) {
		const password = "configured-password"
		state, shell, _ := loginCommandSession(t, "sudo -i\n"+password+"\necho after")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("\x1b]133;A;motty=" + token + "\x07\r\n[sudo] password for root:")
		state.Wake()
		if aborted := session.LoginCommandAborted(); aborted != "" {
			t.Fatalf("password prompt aborted the configured password: %q", aborted)
		}
		if shell.typed() != "sudo -i\r"+password+"\r" {
			t.Fatalf("configured password was not sent as a secret reply: %q", shell.typed())
		}
		if results := session.LoginCommandResults(); len(results) != 0 {
			t.Fatalf("password handoff recorded a command result before authentication completed: %+v", results)
		}
		if strings.Contains(state.status, password) || strings.Contains(string(session.term.screen.AppendRetainedText(nil)), password) {
			t.Fatal("configured password was displayed in the status bar or terminal")
		}

		// Authentication opens the root shell. Advance to the next configured
		// command without reporting the still-open sudo process as successful.
		shell.say("\r\n\x1b]133;A;motty=" + token + "\x07root@host:~# \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "sudo -i\r"+password+"\recho after\r" {
			t.Fatalf("next command did not run at the authenticated prompt: %q", shell.typed())
		}
		if results := session.LoginCommandResults(); len(results) != 0 {
			t.Fatalf("interactive sudo step was recorded as a successful command: %+v", results)
		}
	})

	t.Run("GateShell bracketed SSH password prompt sends configured secret", func(t *testing.T) {
		const password = "configured-password"
		state, shell, _ := loginCommandSession(t, "ssh root@10.169.156.26:22\n"+password+"\necho after")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("\x1b]133;A;motty=" + token + "\x07\r\n| [root@10.169.156.26:22] Password:")
		state.Wake()

		if aborted := session.LoginCommandAborted(); aborted != "" {
			t.Fatalf("GateShell password prompt aborted the configured password: %q", aborted)
		}
		if shell.typed() != "ssh root@10.169.156.26:22\r"+password+"\r" {
			t.Fatalf("configured password was not sent as a secret reply: %q", shell.typed())
		}
		if results := session.LoginCommandResults(); len(results) != 0 {
			t.Fatalf("password handoff recorded a command result before authentication completed: %+v", results)
		}
		if strings.Contains(state.status, password) || strings.Contains(string(session.term.screen.AppendRetainedText(nil)), password) {
			t.Fatal("configured password was displayed in the status bar or terminal")
		}

		shell.say("\r\n\x1b]133;A;motty=" + token + "\x07root@host:~# \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "ssh root@10.169.156.26:22\r"+password+"\recho after\r" {
			t.Fatalf("next command did not run after authentication: %q", shell.typed())
		}
	})

	t.Run("repeated password prompt aborts without trying another line", func(t *testing.T) {
		const password = "configured-password"
		state, shell, _ := loginCommandSession(t, "ssh root@example\n"+password+"\necho after")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("\x1b]133;A;motty=" + token + "\x07\r\nroot@example's password:")
		state.Wake()
		if shell.typed() != "ssh root@example\r"+password+"\r" {
			t.Fatalf("first password reply was not sent once: %q", shell.typed())
		}
		state.Wake()
		if session.LoginCommandAborted() != "" || shell.typed() != "ssh root@example\r"+password+"\r" {
			t.Fatalf("unchanged password prompt retried or aborted unexpectedly: typed=%q aborted=%q", shell.typed(), session.LoginCommandAborted())
		}

		shell.say("\r\nPermission denied, please try again.\r\nroot@example's password:")
		state.Wake()
		if session.LoginCommandAborted() == "" {
			t.Fatal("repeated password prompt should abort the remaining sequence")
		}
		if shell.typed() != "ssh root@example\r"+password+"\r" {
			t.Fatalf("sent another configured line as a password: %q", shell.typed())
		}
		if strings.Contains(state.status, password) {
			t.Fatal("password was exposed in the status bar")
		}
		for _, result := range session.LoginCommandResults() {
			if strings.Contains(result.Command, password) || strings.Contains(result.Output, password) || strings.Contains(result.Aborted, password) {
				t.Fatalf("password leaked through login result: %+v", result)
			}
		}
	})

	t.Run("verification code prompt keeps the abort policy", func(t *testing.T) {
		state, shell, now := loginCommandSession(t, "ssh root@example\necho after")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("\x1b]133;A;motty=" + token + "\x07\r\nVerification code:")
		state.Wake()
		*now += loginCommandGrace
		state.Wake()

		if session.LoginCommandAborted() == "" {
			t.Fatal("verification-code prompt should abort the remaining configured commands")
		}
		if shell.typed() != "ssh root@example\r" {
			t.Fatalf("sent a configured reply at the verification-code prompt: %q", shell.typed())
		}
	})

	t.Run("incomplete prompt marker aborts after grace", func(t *testing.T) {
		state, shell, now := loginCommandSession(t, "sudo -i\necho after")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("\x1b]133;A;motty=" + token + "\x07still running") // Missing matching 133;B.
		state.Wake()
		*now += loginCommandGrace
		state.Wake()

		if session.LoginCommandAborted() == "" {
			t.Fatal("incomplete prompt marker should abort after the grace period")
		}
		if shell.typed() != "sudo -i\r" {
			t.Fatalf("sent a configured reply at an incomplete prompt marker: %q", shell.typed())
		}
	})
}

func TestMultilineLoginCommandDoesNotAdvanceOnSlowOutputWithColon(t *testing.T) {
	state, shell, now := loginCommandSession(t, "ssh root@example\necho after")
	token := state.sessions[0].term.folderToken

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "ssh root@example\r" {
		t.Fatalf("typed %q", shell.typed())
	}

	shell.say("ssh root@example\r\nConnecting to host:")
	state.Wake()
	*now += loginCommandGrace
	state.Wake()
	if shell.typed() != "ssh root@example\r" {
		t.Fatalf("advanced on intermediate output: %q", shell.typed())
	}
	if aborted := state.sessions[0].LoginCommandAborted(); aborted != "" {
		t.Fatalf("aborted while the command was still producing output: %q", aborted)
	}
}

func TestMultilineLoginCommandClearsFinalStepAfterIncompletePromptGrace(t *testing.T) {
	state, shell, now := loginCommandSession(t, "echo first\necho last")
	session := &state.sessions[0]
	token := session.term.folderToken

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "echo first\r" {
		t.Fatalf("first step typed %q", shell.typed())
	}

	shell.say("echo first\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "echo first\recho last\r" {
		t.Fatalf("last step was not sent: %q", shell.typed())
	}

	// A final command that emits an incomplete 133;A marker should clear its
	// tracking state after the grace interval instead of waiting for timeout.
	shell.say("echo last\r\nstill running\r\n\x1b]133;A;motty=" + token + "\x07")
	state.Wake()
	*now += loginCommandGrace
	state.Wake()
	if session.LoginCommandAborted() != "" {
		t.Fatalf("final incomplete marker aborted the sequence: %q", session.LoginCommandAborted())
	}
	if session.term.login.running || session.term.login.line != "" {
		t.Fatal("final incomplete marker did not clear login command tracking after grace")
	}
	if results := session.LoginCommandResults(); len(results) != 1 || results[0].Command != "echo first" || !results[0].Success {
		t.Fatalf("unexpected results after clearing final step: %+v", results)
	}
}

func TestMultilineLoginCommandFallsBackToQuietStepByStepWhenUnintegrated(t *testing.T) {
	state, shell, now := loginCommandSession(t, "cd /opt/app\n./check.sh")
	shell.say("banner\r\n$ ")
	state.Wake()
	*now += loginCommandGrace
	state.Wake()
	if shell.typed() != "cd /opt/app\r" {
		t.Fatalf("step 1 typed %q", shell.typed())
	}
	// While step 1 is still printing output and hasn't drawn the next prompt, quiet wait must not send step 2.
	shell.say("cd /opt/app\r\n")
	state.Wake()
	*now += loginCommandQuiet
	state.Wake()
	if shell.typed() != "cd /opt/app\r" {
		t.Fatalf("sent step 2 before next prompt appeared: %q", shell.typed())
	}
	// Once the shell draws the next prompt and stays quiet, step 2 is sent.
	shell.say("$ ")
	state.Wake()
	*now += loginCommandQuiet
	state.Wake()
	if shell.typed() != "cd /opt/app\r./check.sh\r" {
		t.Fatalf("step 2 typed %q", shell.typed())
	}
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
