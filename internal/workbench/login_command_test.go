package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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

func TestMultilineLoginCommandAbortsOnTimeoutDisconnectAndAbnormalPrompt(t *testing.T) {
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

	t.Run("abnormal prompt", func(t *testing.T) {
		state, shell, now := loginCommandSession(t, "sudo -i\necho after")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "sudo -i\r" {
			t.Fatalf("typed %q", shell.typed())
		}
		// An incomplete prompt (133;A without 133;B) that hangs past grace aborts remaining commands.
		shell.say("\x1b]133;A;motty=" + token + "\x07broken prompt>")
		state.Wake()
		*now += loginCommandGrace
		state.Wake()
		if session.LoginCommandAborted() == "" {
			t.Fatal("expected abnormal prompt abort notice")
		}
		if shell.typed() != "sudo -i\r" {
			t.Fatalf("ran remaining command after abnormal prompt: %q", shell.typed())
		}
	})
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

func loginCommandSessionWithOptions(t *testing.T, command, mode, interval, policy string) (*State, *fakeShell, *time.Duration) {
	t.Helper()
	world, options := newWorld(t)
	hosts := append([]savedHost(nil), sampleHosts...)
	hosts[0].savedTransport.LoginCommand = command
	hosts[0].savedTransport.LoginCommandMode = mode
	hosts[0].savedTransport.LoginCommandInterval = interval
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

func TestMultilineLoginCommandFixedIntervalSendsLineByLineWithPasswordPrompt(t *testing.T) {
	script := "ssh ops@10.0.0.2\nsecret-pass\nwhoami"
	state, shell, now := loginCommandSessionWithOptions(t, script, "interval", "0.5s", "")
	session := &state.sessions[0]
	token := session.term.folderToken

	// Initial prompt sends line 1 immediately.
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "ssh ops@10.0.0.2\r" {
		t.Fatalf("step 1 typed %q", shell.typed())
	}

	// Remote shows a password prompt; fixed-interval mode must NOT abort on it.
	shell.say("ssh ops@10.0.0.2\r\nops@10.0.0.2's password: ")
	state.Wake()
	*now += 400 * time.Millisecond
	state.Wake()
	if shell.typed() != "ssh ops@10.0.0.2\r" {
		t.Fatalf("sent step 2 before 500ms interval: %q", shell.typed())
	}
	if session.LoginCommandAborted() != "" {
		t.Fatalf("aborted on password prompt in fixed-interval mode: %q", session.LoginCommandAborted())
	}

	// At 500ms, line 2 (password) is sent.
	*now += 100 * time.Millisecond
	state.Wake()
	want2 := "ssh ops@10.0.0.2\rsecret-pass\r"
	if shell.typed() != want2 {
		t.Fatalf("step 2 typed %q, want %q", shell.typed(), want2)
	}
	if strings.Contains(state.status, "secret-pass") {
		t.Fatalf("status leaked secret password: %q", state.status)
	}

	// After another 500ms, line 3 is sent and sequence completes.
	*now += 500 * time.Millisecond
	state.Wake()
	want3 := want2 + "whoami\r"
	if shell.typed() != want3 {
		t.Fatalf("step 3 typed %q, want %q", shell.typed(), want3)
	}
	if session.LoginCommandAborted() != "" {
		t.Fatalf("unexpected abort: %q", session.LoginCommandAborted())
	}
	results := session.LoginCommandResults()
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3", results)
	}
	wantCommands := []string{"ssh ops@10.0.0.2", "***", "whoami"}
	for i, r := range results {
		if !r.Success || r.Step != i+1 || r.Total != 3 || r.HasExitCode || r.ExitCode != -1 || r.Command != wantCommands[i] {
			t.Fatalf("result[%d] = %+v", i, r)
		}
	}
	if strings.Contains(state.status, "secret-pass") {
		t.Fatalf("status leaked secret password: %q", state.status)
	}
}

func TestMultilineLoginCommandPerLineDelayOverride(t *testing.T) {
	script := "echo one\n# @delay 1s\necho two\n# @delay 200ms\necho three"
	state, shell, now := loginCommandSessionWithOptions(t, script, "interval", "500ms", "")
	session := &state.sessions[0]
	token := session.term.folderToken

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "echo one\r" {
		t.Fatalf("step 1 typed %q", shell.typed())
	}

	// 600ms is past default 500ms, but step 2 has @delay 1s.
	*now += 600 * time.Millisecond
	state.Wake()
	if shell.typed() != "echo one\r" {
		t.Fatalf("sent step 2 before 1s override: %q", shell.typed())
	}

	*now += 400 * time.Millisecond
	state.Wake()
	if shell.typed() != "echo one\recho two\r" {
		t.Fatalf("step 2 typed %q", shell.typed())
	}

	// Step 3 has @delay 200ms override.
	*now += 200 * time.Millisecond
	state.Wake()
	if shell.typed() != "echo one\recho two\recho three\r" {
		t.Fatalf("step 3 typed %q", shell.typed())
	}
	if len(session.LoginCommandResults()) != 3 {
		t.Fatalf("results = %+v", session.LoginCommandResults())
	}
}

func TestMultilineLoginCommandExpectFeedbackAndTimeout(t *testing.T) {
	t.Run("matches expected feedback", func(t *testing.T) {
		script := "ssh ops@10.0.0.2\n# @expect password:\nsecret-pass\n# @expect $\nwhoami"
		state, shell, _ := loginCommandSessionWithOptions(t, script, "wait", "", "")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "ssh ops@10.0.0.2\r" {
			t.Fatalf("step 1 typed %q", shell.typed())
		}

		// Other output does not trigger step 2.
		shell.say("ssh ops@10.0.0.2\r\nConnecting...\r\n")
		state.Wake()
		if shell.typed() != "ssh ops@10.0.0.2\r" {
			t.Fatalf("sent step 2 before expected feedback: %q", shell.typed())
		}

		// Expected feedback arrives -> step 2 is sent.
		shell.say("ops@10.0.0.2's password: ")
		state.Wake()
		want2 := "ssh ops@10.0.0.2\rsecret-pass\r"
		if shell.typed() != want2 {
			t.Fatalf("step 2 typed %q, want %q", shell.typed(), want2)
		}

		shell.say("\r\nWelcome!\r\nops@remote:~$ ")
		state.Wake()
		want3 := want2 + "whoami\r"
		if shell.typed() != want3 {
			t.Fatalf("step 3 typed %q, want %q", shell.typed(), want3)
		}
		if session.LoginCommandAborted() != "" {
			t.Fatalf("unexpected abort: %q", session.LoginCommandAborted())
		}
		results := session.LoginCommandResults()
		if len(results) < 2 {
			t.Fatalf("results = %+v", results)
		}
		if results[0].HasExitCode || results[0].ExitCode != -1 {
			t.Fatalf("step 1 still running at password prompt must not report exit code 0: %+v", results[0])
		}
		if results[1].Command != "***" || strings.Contains(state.status, "secret-pass") {
			t.Fatalf("secret line leaked: result=%+v status=%q", results[1], state.status)
		}
	})

	t.Run("ignores stale screen text before step is sent", func(t *testing.T) {
		script := "echo password: done\n# @expect $\necho next\n# @expect password:\nsuper-secret"
		state, shell, _ := loginCommandSessionWithOptions(t, script, "wait", "", "")
		session := &state.sessions[0]
		token := session.term.folderToken

		// Screen already contains "$" and earlier "password:" before step 1 runs.
		shell.say("old password: \r\n\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "echo password: done\r" {
			t.Fatalf("step 1 typed %q", shell.typed())
		}

		// Echoing step 1 without a new "$" prompt must NOT trigger step 2.
		shell.say("echo password: done\r\n")
		state.Wake()
		if shell.typed() != "echo password: done\r" {
			t.Fatalf("matched stale $ prompt: %q", shell.typed())
		}

		// New prompt "$" arrives after step 1 -> step 2 is sent.
		shell.say("$ ")
		state.Wake()
		want2 := "echo password: done\recho next\r"
		if shell.typed() != want2 {
			t.Fatalf("step 2 typed %q, want %q", shell.typed(), want2)
		}

		// Stale "password:" from before step 2 must NOT trigger step 3.
		shell.say("echo next\r\nrunning...\r\n")
		state.Wake()
		if shell.typed() != want2 {
			t.Fatalf("matched stale password: prompt: %q", shell.typed())
		}

		// New "password:" arrives after step 2 -> step 3 is sent.
		shell.say("sudo password: ")
		state.Wake()
		want3 := want2 + "super-secret\r"
		if shell.typed() != want3 {
			t.Fatalf("step 3 typed %q, want %q", shell.typed(), want3)
		}
	})

	t.Run("expect timeout aborts with clear reason", func(t *testing.T) {
		script := "ssh ops@10.0.0.2\n# @expect ready>\n# @expect-timeout 5s\necho done"
		state, shell, now := loginCommandSessionWithOptions(t, script, "wait", "", "")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("ssh ops@10.0.0.2\r\nStill connecting...\r\n")
		state.Wake()
		*now += 5 * time.Second
		state.Wake()

		if session.LoginCommandAborted() == "" || !strings.Contains(session.LoginCommandAborted(), "ready>") {
			t.Fatalf("expected expect-timeout abort notice, got %q", session.LoginCommandAborted())
		}
		if shell.typed() != "ssh ops@10.0.0.2\r" {
			t.Fatalf("sent step 2 after expect timeout: %q", shell.typed())
		}
	})

	t.Run("regex pattern and exact word secret matching", func(t *testing.T) {
		script := "open-shop\n# @expect shopping>\nbuy milk\n# @expect /(?i)(verification|passcode):/\n998877\necho ok"
		state, shell, _ := loginCommandSessionWithOptions(t, script, "wait", "", "")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("shopping> ")
		state.Wake()
		if shell.typed() != "open-shop\rbuy milk\r" {
			t.Fatalf("step 2 typed %q", shell.typed())
		}
		// Status bar must not leak @expect pattern when waiting.
		if strings.Contains(state.status, "verification") || strings.Contains(state.status, "passcode") {
			t.Fatalf("status bar leaked @expect pattern: %q", state.status)
		}

		shell.say("Verification: ")
		state.Wake()
		if shell.typed() != "open-shop\rbuy milk\r998877\r" {
			t.Fatalf("step 3 typed %q", shell.typed())
		}
		shell.say("\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()

		results := session.LoginCommandResults()
		if len(results) != 4 {
			t.Fatalf("results = %+v, want 4", results)
		}
		if results[1].Command != "buy milk" {
			t.Fatalf("shopping> falsely masked non-secret command: %+v", results[1])
		}
		if results[2].Command != "***" {
			t.Fatalf("Verification: did not mask secret command: %+v", results[2])
		}
	})
}

func TestHostFormSavesAndReEditsLoginCommandModeAndInterval(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Timed")
	typeInto(state, hitDialogAddress, "timed.example.com")
	typeInto(state, hitDialogUser, "ops")
	// Interval editor is unavailable while mode is Wait for feedback.
	if state.EditorFor(hitDialogLoginCommandInterval) != nil {
		t.Fatal("interval editor should be unavailable in wait mode")
	}
	typeInto(state, hitDialogLoginCommand, "# @deliy 500ms\necho hi")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrLoginCommandDirective {
		t.Fatalf("invalid directive accepted: modal=%v err=%q", state.modal, state.dialogError)
	}

	state.editor.loginCommand = text.Editor{}
	typeInto(state, hitDialogLoginCommand, "ssh 10.0.0.2\n# @secret\nsecret")
	state.Click(hitDialogLoginCommandMode)
	if state.EditorFor(hitDialogLoginCommandInterval) == nil {
		t.Fatal("interval editor unavailable in fixed-interval mode")
	}
	typeInto(state, hitDialogLoginCommandInterval, "bad")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrLoginCommandInterval {
		t.Fatalf("invalid interval accepted: modal=%v err=%q", state.modal, state.dialogError)
	}

	state.editor.loginCommandInterval = text.Editor{}
	typeInto(state, hitDialogLoginCommandInterval, "0.5s")
	state.Click(hitDialogSubmit)
	if state.modal == modalNewHost {
		t.Fatalf("form refused: %q", state.dialogError)
	}
	added := state.hosts[len(state.hosts)-1]
	if added.LoginCommandMode != loginSendFixedInterval || added.LoginCommandInterval != 500*time.Millisecond {
		t.Fatalf("saved host = %+v", added)
	}
	if got := added.transport(); got.LoginCommandMode != "interval" || got.LoginCommandInterval != "500ms" {
		t.Fatalf("persisted = %+v", got)
	}

	reloaded := newTestState(t, options)
	editedHost := reloaded.hosts[len(reloaded.hosts)-1]
	reloaded.openEditHostID(editedHost.ID)
	if !reloaded.editor.advancedOpen {
		t.Fatal("advanced section did not open")
	}
	if reloaded.editor.loginCommandMode != loginSendFixedInterval {
		t.Fatalf("echoed mode = %v", reloaded.editor.loginCommandMode)
	}
	if got := reloaded.editor.value(fieldLoginCommandInterval); got != "500ms" {
		t.Fatalf("echoed interval = %q", got)
	}
}

func TestMultilineLoginCommandSecretRedaction(t *testing.T) {
	script := "sudo -i\n# @secret\nmy-top-secret-pw"
	state, shell, _ := loginCommandSessionWithOptions(t, script, "wait", "", "")
	session := &state.sessions[0]
	token := session.term.folderToken

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	shell.say("sudo -i\r\n\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "sudo -i\rmy-top-secret-pw\r" {
		t.Fatalf("typed %q", shell.typed())
	}

	// Simulate failure on the secret step -> must never leak secret into status, screen, or results.
	shell.say("\r\nSorry, try again.\r\n\x1b]133;D;1;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()

	results := session.LoginCommandResults()
	if len(results) != 2 || results[1].Command != "***" {
		t.Fatalf("results leaked secret: %+v", results)
	}
	screenText := string(session.term.screen.AppendRetainedText(nil))
	if strings.Contains(screenText, "my-top-secret-pw") || strings.Contains(state.status, "my-top-secret-pw") {
		t.Fatalf("secret leaked in screen=%q or status=%q", screenText, state.status)
	}
}

func TestMultilineLoginCommandFixedIntervalWithWorkspaceCommand(t *testing.T) {
	state, shell, now := loginCommandSessionWithOptions(t, "ssh ops@10.0.0.2\n# @secret\nsecret-pass", "interval", "500ms", "")
	session := &state.sessions[0]
	session.workspaceCommand = WorkspaceCommand{Command: "cd /srv/app"}
	session.term.armInitialization(state, session)
	token := session.term.folderToken

	// Initial prompt sends login step 1.
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "ssh ops@10.0.0.2\r" {
		t.Fatalf("step 1 typed %q", shell.typed())
	}

	// 500ms later sends login step 2, then waits for remote prompt before workspace command.
	*now += 500 * time.Millisecond
	state.Wake()
	want2 := "ssh ops@10.0.0.2\rsecret-pass\r"
	if shell.typed() != want2 {
		t.Fatalf("step 2 typed %q, want %q", shell.typed(), want2)
	}

	// Remote shell reports prompt -> workspace command runs.
	shell.say("\x1b]133;D;0;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	want3 := want2 + "cd /srv/app\r"
	if shell.typed() != want3 {
		t.Fatalf("workspace step typed %q, want %q", shell.typed(), want3)
	}
	results := session.LoginCommandResults()
	if len(results) != 2 || results[0].Command != "ssh ops@10.0.0.2" || results[1].Command != "***" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestMultilineLoginCommandPauseAndDisconnectDuringWait(t *testing.T) {
	t.Run("pause on user input during delay", func(t *testing.T) {
		state, shell, now := loginCommandSessionWithOptions(t, "echo first\n# @delay 1s\necho second", "interval", "500ms", "")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "echo first\r" {
			t.Fatalf("step 1 typed %q", shell.typed())
		}

		// User types manually while waiting for step 2's delay.
		session.term.send([]byte("ls\r"))
		*now += 2 * time.Second
		state.Wake()
		if shell.typed() != "echo first\rls\r" {
			t.Fatalf("automated step 2 fired after user input paused it: %q", shell.typed())
		}

		// Closing a paused session must not raise a spurious disconnect abort.
		state.closeDisconnectedShell(session)
		if session.LoginCommandAborted() != "" {
			t.Fatalf("paused session raised disconnect abort: %q", session.LoginCommandAborted())
		}
	})

	t.Run("disconnect during delay", func(t *testing.T) {
		state, shell, _ := loginCommandSessionWithOptions(t, "ssh ops@10.0.0.2\n# @secret\nhidden-pw", "interval", "500ms", "")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		if shell.typed() != "ssh ops@10.0.0.2\r" {
			t.Fatalf("step 1 typed %q", shell.typed())
		}

		state.closeDisconnectedShell(session)
		if session.LoginCommandAborted() == "" {
			t.Fatal("expected disconnect abort during delay wait")
		}
		if strings.Contains(session.LoginCommandAborted(), "hidden-pw") {
			t.Fatalf("abort notice leaked secret: %q", session.LoginCommandAborted())
		}
	})

	t.Run("disconnect during expect wait", func(t *testing.T) {
		state, shell, _ := loginCommandSessionWithOptions(t, "ssh ops@10.0.0.2\n# @expect password:\nhidden-pw", "wait", "", "")
		session := &state.sessions[0]
		token := session.term.folderToken

		shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
		state.Wake()
		shell.say("ssh ops@10.0.0.2\r\nConnecting...\r\n")
		state.Wake()

		state.closeDisconnectedShell(session)
		if session.LoginCommandAborted() == "" {
			t.Fatal("expected disconnect abort during expect wait")
		}
		if strings.Contains(session.LoginCommandAborted(), "hidden-pw") {
			t.Fatalf("abort notice leaked secret: %q", session.LoginCommandAborted())
		}
	})
}

func TestAppendExpectDataSplitInputs(t *testing.T) {
	stream := "\x1b]133;C;motty=abc\x07\x1b[32mops@host\x1b[0m's \x1b]0;title\x1b\\password: \x1b[K"
	want := "ops@host's password: "

	// 1-byte at a time split across every escape boundary.
	var oneByte loginCommand
	for i := 0; i < len(stream); i++ {
		oneByte.appendExpectData([]byte{stream[i]})
	}
	if oneByte.expectText() != want {
		t.Fatalf("1-byte split got %q, want %q", oneByte.expectText(), want)
	}

	// 2-byte and 3-byte splits.
	for _, chunk := range []int{2, 3, 5} {
		var split loginCommand
		for i := 0; i < len(stream); i += chunk {
			end := min(i+chunk, len(stream))
			split.appendExpectData([]byte(stream[i:end]))
		}
		if split.expectText() != want {
			t.Fatalf("%d-byte split got %q, want %q", chunk, split.expectText(), want)
		}
	}
}

func FuzzAppendExpectData(f *testing.F) {
	f.Add([]byte("\x1b[31mhello\x1b[0m \x1b]133;A;motty=tok\x07$ \x1b]0;t\x1b\\"), uint8(3))
	f.Add([]byte("password: \r\n\x1b[?2004h"), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, split uint8) {
		var whole loginCommand
		whole.appendExpectData(data)

		step := int(split%7) + 1
		var chunked loginCommand
		for i := 0; i < len(data); i += step {
			end := min(i+step, len(data))
			chunked.appendExpectData(data[i:end])
		}
		if whole.expectBuf != chunked.expectBuf {
			t.Fatalf("whole=%q != chunked(%d)=%q", whole.expectBuf, step, chunked.expectBuf)
		}
		if strings.ContainsRune(whole.expectBuf, '\x1b') || strings.ContainsRune(whole.expectBuf, '\x07') {
			t.Fatalf("control character leaked: %q", whole.expectBuf)
		}
	})
}
