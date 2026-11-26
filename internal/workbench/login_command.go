package workbench

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
)

// maxLoginCommand bounds the command text a host types after login.
const maxLoginCommand = 1000

// loginFailurePolicy says how multi-line post-login execution reacts when one
// command exits with a non-zero status.
type loginFailurePolicy uint8

const (
	// loginFailureStop aborts remaining login commands when one fails.
	loginFailureStop loginFailurePolicy = iota
	// loginFailureContinue records the failure and continues with the next line.
	loginFailureContinue
)

func (host Host) savedLoginCommandPolicy() string {
	if host.LoginCommandPolicy == loginFailureContinue {
		return "continue"
	}
	return ""
}

func (host *Host) applyLoginCommandPolicy(saved string) {
	switch strings.TrimSpace(saved) {
	case "continue", "ignore":
		host.LoginCommandPolicy = loginFailureContinue
	default:
		host.LoginCommandPolicy = loginFailureStop
	}
}

// validSingleLineCommand reports whether value can be typed into a shell as one line.
func validSingleLineCommand(value string) bool {
	if utf8.RuneCountInString(value) > maxLoginCommand {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// restoredSingleLineCommand is a saved single-line command, or none when the
// saved text could not have come from a single-line form field.
func restoredSingleLineCommand(value string) string {
	value = strings.TrimSpace(value)
	if !validSingleLineCommand(value) {
		return ""
	}
	return value
}

// normalizeLoginCommand normalizes line endings to LF and trims outer blank space.
func normalizeLoginCommand(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	return strings.TrimSpace(value)
}

// validLoginCommand reports whether value is a valid single-line or multi-line
// post-login command configuration.
func validLoginCommand(value string) bool {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	if utf8.RuneCountInString(value) > maxLoginCommand {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f
	})
}

// restoredLoginCommand is a saved login command, or none when the saved text
// could not have come from the form.
func restoredLoginCommand(value string) string {
	value = normalizeLoginCommand(value)
	if !validLoginCommand(value) {
		return ""
	}
	return value
}

// parseLoginCommands splits a single-line or multi-line login command
// configuration into executable lines in order, ignoring blank lines and
// comment lines (whose trimmed text begins with '#') while keeping quotes,
// arguments, pipes and redirections intact.
func parseLoginCommands(value string) []string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	var commands []string
	for _, raw := range strings.Split(value, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		commands = append(commands, line)
	}
	return commands
}

// relayPrelude returns the jump host's post-login command as a single shell line
// suitable for an SSH shell relay prelude.
func (host Host) relayPrelude() string {
	return strings.Join(parseLoginCommands(host.LoginCommand), " && ")
}

// LoginCommandResult records the outcome of one executed post-login command line.
type LoginCommandResult struct {
	Step     int
	Total    int
	Command  string
	Output   string
	ExitCode int
	Success  bool
	Aborted  string
}

// LoginCommandResults returns the execution results of post-login commands in
// this session in execution order.
func (session *Session) LoginCommandResults() []LoginCommandResult {
	if session == nil {
		return nil
	}
	return append([]LoginCommandResult(nil), session.loginResults...)
}

// LoginCommandAborted returns the reason post-login command execution was
// aborted in this session, or empty when no abort occurred.
func (session *Session) LoginCommandAborted() string {
	if session == nil {
		return ""
	}
	return session.loginAborted
}

type loginAbortReason uint8

const (
	loginAbortNone loginAbortReason = iota
	loginAbortDisconnected
	loginAbortTimeout
	loginAbortAbnormalPrompt
)

// A host's login command is typed into each new shell when the shell is ready
// for input, one line at a time in order. Readiness is the end of a prompt that
// Motty's shell integration reported; where that never comes (a shell the
// integration does not start, or a server that keeps it from running) the shell
// is taken as ready once its output has been quiet for a while. The line is
// typed like the user would, but it is not the user's own input: it stays out
// of the command history and of the broadcast to other panes.
const (
	// loginCommandQuiet is how long a shell without integration has to be quiet.
	loginCommandQuiet = 400 * time.Millisecond
	// loginCommandGrace is how long a shell that should report its prompt is given
	// to do so before it is treated like one that never will.
	loginCommandGrace = 4 * time.Second
	// loginCommandExecTimeout bounds how long one post-login command step may run
	// without completing before remaining commands are aborted.
	loginCommandExecTimeout = 30 * time.Second
)

// loginCommand is the initialization state of a terminal's post-login and
// workspace commands.
type loginCommand struct {
	line            string
	lines           []string
	next            int
	policy          loginFailurePolicy
	workspace       string
	paused          bool
	waitPrompt      bool
	loginFirst      bool
	isWorkspaceStep bool
	promptVersion   uint64
	last            time.Duration
	armedAt         time.Duration
	seen            bool
	// local marks a local shell's startup command. Such a shell reports its
	// prompt from the moment it starts, because the integration is part of its
	// launch, so the first prompt carries no command-start marker and promptReady
	// never turns true before the first command; it is typed at the prompt's end
	// instead.
	local bool

	// Active running post-login command tracking.
	running        bool
	activeIndex    int
	activeLine     string
	sentAt         time.Duration
	commandStarted bool
	hasExitCode    bool
	exitCode       int
	beforeLines    []string
	beforePrompt   string
	outputCaptured bool
	capturedOutput string
	results        []LoginCommandResult
}

func (login *loginCommand) arm(line string) {
	login.armLines(parseLoginCommands(line), loginFailureStop)
}

func (login *loginCommand) armLines(lines []string, policy loginFailurePolicy) {
	if len(lines) == 0 {
		*login = loginCommand{}
		return
	}
	copied := append([]string(nil), lines...)
	*login = loginCommand{
		line:   copied[0],
		lines:  copied,
		policy: policy,
	}
}

// armLocal freezes a local shell's startup command.
func (login *loginCommand) armLocal(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		*login = loginCommand{}
		return
	}
	*login = loginCommand{line: line, local: true}
}

func (login *loginCommand) hasPendingOrRunningLogin() bool {
	if login.local {
		return false
	}
	if login.running {
		return true
	}
	return login.line != "" && !login.isWorkspaceStep
}

// hasRemainingSteps reports whether there are subsequent commands waiting to
// be executed after the current step.
func (login *loginCommand) hasRemainingSteps() bool {
	if login.local {
		return false
	}
	if login.running {
		return login.line != ""
	}
	return len(login.lines) > 1 || (len(login.lines) == 1 && login.workspace != "" && login.loginFirst)
}

// output notes that the shell wrote something at now, while a command waits or runs.
func (login *loginCommand) output(now time.Duration) {
	if login.line != "" || login.running {
		login.last, login.seen = now, true
	}
}

// quietWait reports how long to wait before the quiet rule can apply, or zero when
// it already does.
func (login *loginCommand) quietWait(now, quiet time.Duration) time.Duration {
	return max(login.last+quiet-now, 0)
}

func (login *loginCommand) noteCommandStart() {
	if login.running {
		login.commandStarted = true
	}
}

func (login *loginCommand) noteCommandFinished(t *terminal, code int) {
	if !login.running {
		return
	}
	login.exitCode = code
	login.hasExitCode = true
	if !login.outputCaptured {
		login.capturedOutput = login.extractOutput(t, false)
		login.outputCaptured = true
	}
}

func (login *loginCommand) notePromptStart(t *terminal) {
	if !login.running || login.outputCaptured {
		return
	}
	login.capturedOutput = login.extractOutput(t, false)
	login.outputCaptured = true
}

func (login *loginCommand) snapshotBeforeSend(t *terminal) {
	if t == nil || t.screen == nil {
		login.beforeLines = nil
		login.beforePrompt = ""
		return
	}
	text := string(t.screen.AppendRetainedText(nil))
	if text == "" {
		login.beforeLines = nil
	} else {
		login.beforeLines = strings.Split(text, "\n")
	}
	login.beforePrompt = strings.TrimSpace(t.screen.RowText(t.screen.Cursor().Y))
}

func (login *loginCommand) extractOutput(t *terminal, stripTrailingPrompt bool) string {
	if t == nil || t.screen == nil {
		return ""
	}
	text := string(t.screen.AppendRetainedText(nil))
	if text == "" {
		return ""
	}
	afterLines := strings.Split(text, "\n")
	if stripTrailingPrompt && t.screen.Cursor().X > 0 && len(afterLines) > 0 {
		afterLines = afterLines[:len(afterLines)-1]
	}
	startIdx := 0
	if len(login.beforeLines) > 0 {
		startIdx = len(login.beforeLines) - 1
	}
	if startIdx >= len(afterLines) {
		return ""
	}
	var out []string
	first := afterLines[startIdx]
	if login.beforePrompt != "" {
		trimmedFirst := strings.TrimSpace(first)
		if strings.HasPrefix(trimmedFirst, login.beforePrompt) {
			first = strings.TrimSpace(strings.TrimPrefix(trimmedFirst, login.beforePrompt))
		}
	}
	first = strings.TrimSpace(first)
	switch {
	case first == "" || first == login.activeLine:
		// The prompt row only echoed the command itself.
	case strings.HasPrefix(first, login.activeLine):
		if rest := strings.TrimSpace(strings.TrimPrefix(first, login.activeLine)); rest != "" {
			out = append(out, rest)
		}
	default:
		out = append(out, first)
	}
	for i := startIdx + 1; i < len(afterLines); i++ {
		line := strings.TrimRight(afterLines[i], " \t\r")
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func (login *loginCommand) finalOutput(t *terminal) string {
	if login.outputCaptured {
		return login.capturedOutput
	}
	return login.extractOutput(t, !t.folderIntegrationSeen)
}

// loginQuiet is the quiet the terminal's shell needs: short when no integration
// was asked of it, longer while the integration may still announce a prompt.
func (state *State) loginQuiet(session *Session) time.Duration {
	if session.term.integrated {
		return loginCommandGrace
	}
	return loginCommandQuiet
}

func (state *State) loginStepFinished(session *Session, now time.Duration) bool {
	t := session.term
	if !t.login.running {
		return false
	}
	if t.promptIdle() && !t.promptDirty && t.promptVersion > t.login.promptVersion {
		return true
	}
	if t.login.hasExitCode && ((t.login.exitCode != 0 && t.login.policy == loginFailureStop) || (t.login.line == "" || t.login.isWorkspaceStep)) {
		return true
	}
	if !t.folderIntegrationSeen && t.login.seen && t.login.quietWait(now, loginCommandQuiet) == 0 {
		rowText := strings.TrimSpace(t.screen.RowText(t.screen.Cursor().Y))
		if rowText != "" && !cmdhistory.PasswordPrompt(rowText) {
			return true
		}
	}
	return false
}

func (t *terminal) sayAbovePrompt(style, line string) {
	if t == nil || t.screen == nil {
		return
	}
	cursor := t.screen.Cursor()
	if cursor.X > 0 {
		prefix, _ := t.screen.AppendCellText(nil, cursor.Y, 0, cursor.X)
		wasReady := t.promptReady
		wasStarted := t.promptStarted
		wasDirty := t.promptDirty
		version := t.promptVersion
		t.write([]byte("\r\x1b[2K"))
		t.say(style, line)
		if t.folderIntegrationSeen && t.folderToken != "" {
			t.write([]byte("\x1b]133;A;motty=" + t.folderToken + "\x07" + string(prefix) + "\x1b]133;B;motty=" + t.folderToken + "\x07"))
		} else {
			t.write(prefix)
		}
		t.promptReady = wasReady
		t.promptStarted = wasStarted
		t.promptDirty = wasDirty
		t.promptVersion = version
		return
	}
	t.say(style, line)
}

func (state *State) completeLoginStep(session *Session) bool {
	t := session.term
	step := t.login.activeIndex + 1
	total := len(t.login.lines)
	if total == 0 {
		total = 1
	}
	cmd := t.login.activeLine
	code := t.login.exitCode
	output := t.login.finalOutput(t)
	success := code == 0
	result := LoginCommandResult{
		Step:     step,
		Total:    total,
		Command:  cmd,
		Output:   output,
		ExitCode: code,
		Success:  success,
	}
	t.login.results = append(t.login.results, result)
	session.loginResults = append(session.loginResults[:0], t.login.results...)
	t.login.running = false
	t.login.activeLine = ""
	t.login.hasExitCode = false
	t.login.exitCode = 0
	t.login.outputCaptured = false
	t.login.capturedOutput = ""

	m := state.messages.Tools
	if !success {
		hasNext := t.login.line != "" && !t.login.isWorkspaceStep
		if t.login.policy == loginFailureContinue && hasNext {
			msg := fmt.Sprintf(m.LoginCommandStepFailedContinueFmt, step, total, code, cmd)
			if output != "" {
				msg += " (" + fmt.Sprintf(m.LoginCommandOutputFmt, singleLineSummary(output)) + ")"
			}
			t.sayAbovePrompt(sgrWarn, msg)
			state.status = msg
			return true
		}
		msg := fmt.Sprintf(m.LoginCommandStepFailedStopFmt, step, total, code, cmd)
		if output != "" {
			msg += " (" + fmt.Sprintf(m.LoginCommandOutputFmt, singleLineSummary(output)) + ")"
		}
		t.sayAbovePrompt(sgrFail, msg)
		state.status = msg
		state.showActionWarning(m.LoginCommandField, msg)
		t.login = loginCommand{results: t.login.results}
		return false
	}

	if total > 1 {
		msg := fmt.Sprintf(m.LoginCommandStepSuccessFmt, step, total, code, cmd)
		state.status = msg
	}
	return t.login.line != ""
}

func singleLineSummary(output string) string {
	output = strings.ReplaceAll(strings.ReplaceAll(output, "\r\n", "\n"), "\r", "\n")
	var parts []string
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	joined := strings.Join(parts, " / ")
	if utf8.RuneCountInString(joined) > 160 {
		runes := []rune(joined)
		return string(runes[:160]) + "…"
	}
	return joined
}

func (state *State) abortLoginCommands(session *Session, reason loginAbortReason) {
	if session == nil || session.term == nil {
		return
	}
	t := session.term
	if !t.login.hasPendingOrRunningLogin() {
		return
	}
	step := t.login.next + 1
	cmd := t.login.line
	output := ""
	if t.login.running {
		step = t.login.activeIndex + 1
		cmd = t.login.activeLine
		output = t.login.finalOutput(t)
	}
	total := len(t.login.lines)
	if total == 0 {
		total = 1
	}
	m := state.messages.Tools
	var msg string
	switch reason {
	case loginAbortDisconnected:
		msg = fmt.Sprintf(m.LoginCommandAbortDisconnectFmt, cmd)
	case loginAbortTimeout:
		msg = fmt.Sprintf(m.LoginCommandAbortTimeoutFmt, cmd)
	case loginAbortAbnormalPrompt:
		msg = fmt.Sprintf(m.LoginCommandAbortPromptFmt, cmd)
	}
	if output != "" {
		msg += " (" + fmt.Sprintf(m.LoginCommandOutputFmt, singleLineSummary(output)) + ")"
	}
	result := LoginCommandResult{
		Step:     step,
		Total:    total,
		Command:  cmd,
		Output:   output,
		ExitCode: -1,
		Success:  false,
		Aborted:  msg,
	}
	t.login.results = append(t.login.results, result)
	session.loginResults = append(session.loginResults[:0], t.login.results...)
	session.loginAborted = msg
	t.sayAbovePrompt(sgrFail, msg)
	state.status = msg
	state.showActionWarning(m.LoginCommandField, msg)
	t.login = loginCommand{results: t.login.results}
}

func (state *State) abortLoginOnDisconnect(session *Session) {
	if session == nil || session.term == nil {
		return
	}
	t := session.term
	// An unexecuted login command, or a multi-line batch still in flight when
	// the shell drops, was cut short by the disconnect.
	if !t.login.local && !t.login.isWorkspaceStep && t.login.hasRemainingSteps() {
		state.abortLoginCommands(session, loginAbortDisconnected)
	}
}

// runLoginCommand types the initialization command when the shell is ready.
func (state *State) runLoginCommand(session *Session) {
	t := session.term
	if t == nil || (t.login.line == "" && !t.login.running) || t.shell == nil || t.ended {
		return
	}
	now := state.now()
	if t.login.armedAt == 0 {
		t.login.armedAt = now
	}
	if t.screen.Modes().AltScreen {
		if t.login.local || !t.login.hasRemainingSteps() {
			t.login = loginCommand{results: t.login.results} // startup handed control to a full-screen program
		} else if !t.login.isWorkspaceStep {
			state.abortLoginCommands(session, loginAbortAbnormalPrompt)
		}
		return
	}
	if t.injection.holding() {
		return
	}
	if t.login.running {
		if state.loginStepFinished(session, now) {
			if !state.completeLoginStep(session) {
				return
			}
		} else {
			if !t.login.isWorkspaceStep {
				if t.promptStarted && !t.promptIdle() && t.login.seen && t.login.quietWait(now, loginCommandGrace) == 0 {
					if t.login.hasRemainingSteps() {
						state.abortLoginCommands(session, loginAbortAbnormalPrompt)
					} else {
						t.login = loginCommand{results: t.login.results}
					}
					return
				}
				if cmdhistory.PasswordPrompt(t.screen.RowText(t.screen.Cursor().Y)) && t.login.seen && t.login.quietWait(now, loginCommandGrace) == 0 && t.login.hasRemainingSteps() {
					state.abortLoginCommands(session, loginAbortAbnormalPrompt)
					return
				}
				if now-t.login.sentAt >= loginCommandExecTimeout {
					if t.login.hasRemainingSteps() {
						state.abortLoginCommands(session, loginAbortTimeout)
					} else {
						t.login = loginCommand{results: t.login.results}
					}
					return
				}
			}
			return
		}
	}
	if t.login.line == "" || t.login.paused {
		return
	}
	if t.login.waitPrompt {
		if t.login.isWorkspaceStep {
			if !t.promptIdle() || t.promptDirty || t.promptVersion <= t.login.promptVersion {
				return
			}
		} else if t.folderIntegrationSeen {
			if !t.promptIdle() || t.promptDirty || t.promptVersion <= t.login.promptVersion {
				if t.promptStarted && !t.promptIdle() && t.login.seen && t.login.quietWait(now, loginCommandGrace) == 0 {
					if t.login.hasRemainingSteps() {
						state.abortLoginCommands(session, loginAbortAbnormalPrompt)
					} else {
						t.login = loginCommand{results: t.login.results}
					}
				}
				return
			}
		} else {
			if !t.login.seen || t.login.quietWait(now, loginCommandQuiet) > 0 {
				return
			}
			rowText := strings.TrimSpace(t.screen.RowText(t.screen.Cursor().Y))
			if cmdhistory.PasswordPrompt(rowText) {
				if t.login.quietWait(now, loginCommandGrace) == 0 && t.login.hasRemainingSteps() {
					state.abortLoginCommands(session, loginAbortAbnormalPrompt)
				}
				return
			}
			if rowText == "" {
				return
			}
		}
		state.sendInitializationCommand(t)
		return
	}
	switch {
	case t.promptIdle() && !t.promptDirty:
	case !t.login.seen || t.folderIntegrationSeen:
		// Once hooks report, only their prompt end authorizes a command.
		if !t.login.local && !t.login.isWorkspaceStep && t.login.hasRemainingSteps() {
			if t.promptStarted && !t.promptIdle() && t.login.seen && t.login.quietWait(now, loginCommandGrace) == 0 {
				state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			} else if !t.login.seen && now-t.login.armedAt >= loginCommandExecTimeout {
				state.abortLoginCommands(session, loginAbortTimeout)
			}
		}
		return
	case t.login.quietWait(now, state.loginQuiet(session)) > 0:
		return
	default:
		if !t.login.local && !t.login.isWorkspaceStep && t.login.hasRemainingSteps() && cmdhistory.PasswordPrompt(t.screen.RowText(t.screen.Cursor().Y)) {
			state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			return
		}
	}
	state.sendInitializationCommand(t)
}

// requestLoginCommandFrames schedules the existing quiet fallback only while
// an unreported prompt still has an initialization command waiting.
func (state *State) requestLoginCommandFrames(canvas *gfx.Canvas) {
	for index := range state.sessions {
		session := &state.sessions[index]
		t := session.term
		if t == nil || t.login.line == "" || t.login.paused || (t.login.waitPrompt && t.login.isWorkspaceStep) || !t.login.seen || t.folderIntegrationSeen || t.shell == nil || t.ended {
			continue
		}
		quiet := state.loginQuiet(session)
		if t.login.waitPrompt || t.login.running {
			quiet = loginCommandQuiet
		}
		canvas.RequestFrameIn(max(t.login.quietWait(state.now(), quiet), time.Millisecond))
	}
}

// armInitialization freezes both commands for this shell, independently of later edits.
func (t *terminal) armInitialization(state *State, session *Session) {
	t.login = loginCommand{}
	if session != nil {
		session.loginResults = nil
		session.loginAborted = ""
	}
	if session.host.isWSL() {
		t.login.armLocal(session.host.LoginCommand)
		return
	}
	if session.isLocal() {
		// A local shell's startup command belongs to the shell it runs, whether
		// the tab chose one or the settings' default did; the connection froze
		// that choice when it was made, as it froze the folder.
		if local, ok := session.conn.(*localConn); ok {
			t.login.armLocal(state.localStartupCommand(local.shell))
		}
		return
	}
	if !workspaceCommandHost(session.host) || session.containerCommand != "" {
		return
	}
	lines := parseLoginCommands(session.host.LoginCommand)
	command := session.workspaceCommand
	switch {
	case len(lines) > 0 && command.Command != "":
		t.login.armLines(lines, session.host.LoginCommandPolicy)
		t.login.workspace = command.Command
		t.login.loginFirst = true
	case len(lines) > 0:
		t.login.armLines(lines, session.host.LoginCommandPolicy)
	case command.Command != "":
		t.login.arm(command.Command)
		t.login.workspace = command.Command
		t.login.isWorkspaceStep = true
	}
}

func (t *terminal) pauseInitialization() {
	if t.login.local {
		t.login = loginCommand{} // never interleave startup with user/assistant input
	} else if t.login.workspace != "" {
		t.login.paused = true
	}
}

func (state *State) sendInitializationCommand(t *terminal) {
	login := t.login
	singleLine := len(login.lines) <= 1 && (login.workspace == "" || !login.loginFirst)
	if !login.local && !login.isWorkspaceStep && !singleLine {
		t.login.snapshotBeforeSend(t)
	}
	if !t.sendAssistant([]byte(login.line + "\r")) {
		t.login.paused = true
		if login.local {
			state.showActionWarning(state.messages.SettingsLocalStartupCommand, state.messages.TerminalInput.SendFailed)
		} else if login.workspace != "" {
			state.showActionWarning(state.messages.WorkspaceCommandField, state.messages.TerminalInput.SendFailed)
		} else {
			state.showActionWarning(state.messages.Tools.LoginCommandField, state.messages.TerminalInput.SendFailed)
		}
		return
	}
	if login.local || login.isWorkspaceStep || singleLine {
		t.login = loginCommand{results: t.login.results}
		return
	}
	t.login.running = true
	t.login.activeIndex = t.login.next
	t.login.activeLine = login.line
	t.login.sentAt = state.now()
	t.login.promptVersion = t.promptVersion
	t.login.commandStarted = false
	t.login.hasExitCode = false
	t.login.exitCode = 0
	t.login.outputCaptured = false
	t.login.capturedOutput = ""
	t.login.seen = false
	t.login.next++
	if t.login.next < len(t.login.lines) {
		t.login.line = t.login.lines[t.login.next]
		t.login.waitPrompt = true
	} else if t.login.workspace != "" && t.login.loginFirst {
		t.login.line = t.login.workspace
		t.login.isWorkspaceStep = true
		t.login.waitPrompt = true
	} else {
		t.login.line = ""
		t.login.waitPrompt = true
	}
}
