package workbench

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
)

// maxLoginCommand bounds the command text a host types after login.
const maxLoginCommand = 1000

var (
	// loginPasswordPrompt matches an explicit password prompt, including the
	// common OpenSSH "user@host's password:" and key passphrase forms. Other
	// secret prompts, such as verification codes and tokens, remain blocked.
	loginPasswordPrompt = regexp.MustCompile(`(?i)^\s*(?:\[sudo\]\s*)?(?:(?:(?:\|\s*)?\[[\w.-]+@[\w.:-]+\]|[\w.-]+@[\w.:-]+(?:['’]s)?)\s+)?(?:password|passwd|passphrase|passcode|(?:please\s+)?(?:enter|type|input)\s+(?:your\s+)?(?:password|passwd|passphrase|passcode)|(?:please\s+)?(?:请输入|输入)(?:登录)?(?:密码|口令)|密码|口令)(?:\s+for\b[^:：?？]*)?\s*[:：?？]\s*$`)

	// These are REPL and database prompts where sending the next configured line
	// is a valid handoff. Pager prompts and Vim's colon prompt are deliberately
	// excluded: the next line would be treated as a program keystroke.
	loginProgramPrompt = regexp.MustCompile(`(?i)^\s*(?:>{3}\s*|\.{3}>\s*|(?:mysql|mariadb|sqlite3?|psql|redis-cli|redis|mongo|mongosh|node|deno|irb|pry|julia|scala|ghci|cqlsh|hive|spark-sql|jshell|trino|presto|duckdb|sqlplus|sql)\s*>(?:\s|$)|irb\([^)]*\)[^>]*>(?:\s|$)|pry\([^)]*\)[^>]*>(?:\s|$)|mariadb\s*\[[^\]]*\]>(?:\s|$)|(?:postgres|cockroachdb|yugabyte)\s*(?:=#|[#>])\s*$)`)

	// Match prompt-shaped lines, not arbitrary output that happens to mention a
	// prompt word. In particular, the line must start like a question or an
	// instruction and end with the actual reply cue.
	loginInteractivePrompt = regexp.MustCompile(`(?i)^\s*(?:(?:please\s+)?press\s+(?:enter|return|any\s+key)(?:\s+to\s+(?:continue|proceed))?|(?:are you sure|do you want|would you like|continue|proceed|accept|trust)[^\r\n]{0,120}(?:yes/no|\[\s*y\s*/\s*n\s*\]|\(\s*y\s*/\s*n\s*\))|yes/no|\[\s*y\s*/\s*n\s*\]|\(\s*y\s*/\s*n\s*\)|按回车(?:键)?(?:继续)?|按任意键(?:继续)?|是否继续|请选择[^，。！？?？:：]{0,24}|请输入[^，。！？?？:：]{0,24})[ \t]*[)\]）】?？:：!！。]*$`)
)

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
	loginCommandGrace = time.Second
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
	running                bool
	activeIndex            int
	activeLine             string
	sentAt                 time.Duration
	commandStarted         bool
	hasExitCode            bool
	exitCode               int
	beforeLines            []string
	beforePrompt           string
	outputCaptured         bool
	capturedOutput         string
	results                []LoginCommandResult
	outputVersion          uint64
	passwordPromptObserved bool
	passwordPromptVersion  uint64
	secretPromptReplies    map[string]uint64
	secretLineIndices      map[int]struct{}
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
		login.outputVersion++
	}
}

func (login *loginCommand) redactedCommand(index int, command string) string {
	if _, secret := login.secretLineIndices[index]; secret {
		return "***"
	}
	return command
}

func (login *loginCommand) redactSecrets(text string) string {
	for index := range login.secretLineIndices {
		if index >= 0 && index < len(login.lines) && login.lines[index] != "" {
			text = strings.ReplaceAll(text, login.lines[index], "***")
		}
	}
	return text
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

// loginInteractivePromptReady reports that a command has stopped at a prompt
// which cannot be completed by shell integration. These anchored prompts are
// actionable as soon as they appear; in a multi-line login sequence, the next
// configured line is the user's answer to that prompt.
func (state *State) loginInteractivePromptReady(t *terminal) bool {
	if t == nil || t.screen == nil || t.promptDirty || !t.login.seen {
		return false
	}
	rowText := strings.TrimSpace(t.screen.RowText(t.screen.Cursor().Y))
	if rowText == "" {
		return false
	}
	if loginPasswordPrompt.MatchString(rowText) {
		return true
	}
	if cmdhistory.PasswordPrompt(rowText) {
		return false
	}
	if loginProgramPrompt.MatchString(rowText) {
		return true
	}
	return loginInteractivePrompt.MatchString(rowText)
}

func loginPasswordPromptText(t *terminal) string {
	if t == nil || t.screen == nil || t.promptDirty || !t.login.seen {
		return ""
	}
	rowText := strings.TrimSpace(t.screen.RowText(t.screen.Cursor().Y))
	if loginPasswordPrompt.MatchString(rowText) {
		return rowText
	}
	return ""
}

func loginPromptKey(prompt string) string {
	return strings.Join(strings.Fields(strings.ToLower(prompt)), " ")
}

// handleLoginPasswordPrompt sends at most one configured secret for each
// stable password prompt while keeping the command that opened the prompt
// active. A repeated prompt after new remote output means the reply was
// rejected, so later configured lines must not be tried as more passwords.
func (state *State) handleLoginPasswordPrompt(session *Session, prompt string, now time.Duration) bool {
	if session == nil || session.term == nil || !session.term.login.running {
		return false
	}
	t := session.term
	login := &t.login
	login.passwordPromptObserved = true
	key := loginPromptKey(prompt)
	if sentVersion, sent := login.secretPromptReplies[key]; sent {
		if login.outputVersion > sentVersion {
			state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			return true
		}
		// The same static screen is not a second prompt. Let the ordinary command
		// timeout remain active while waiting for new output or a new prompt.
		return false
	}
	if login.line == "" || login.isWorkspaceStep || login.next >= len(login.lines) {
		return true
	}
	secretIndex := login.next
	if login.secretLineIndices == nil {
		login.secretLineIndices = make(map[int]struct{})
	}
	login.secretLineIndices[secretIndex] = struct{}{}
	if login.secretPromptReplies == nil {
		login.secretPromptReplies = make(map[string]uint64)
	}
	// Record the attempt even if the shell refuses the write: a later redraw
	// must not cause an automatic retry to consume another configured line.
	login.secretPromptReplies[key] = login.outputVersion
	login.passwordPromptVersion = t.promptVersion
	wasPaused := login.paused
	accepted := t.sendSecret([]byte(login.line + "\r"))
	login.paused = wasPaused || !accepted
	if !accepted {
		state.showActionWarning(state.messages.Tools.LoginCommandField, state.messages.TerminalInput.SendFailed)
		return true
	}
	login.last = now
	login.next++
	if login.next < len(login.lines) {
		login.line = login.lines[login.next]
		login.isWorkspaceStep = false
	} else if login.workspace != "" && login.loginFirst {
		login.line = login.workspace
		login.isWorkspaceStep = true
	} else {
		login.line = ""
		login.isWorkspaceStep = false
	}
	login.waitPrompt = true
	return true
}

// completeLoginHandoff ends tracking for an interactive command that accepted
// a configured password. It deliberately records no successful command result:
// an interactive SSH/sudo child has not produced an exit code.
func (state *State) completeLoginHandoff(session *Session) bool {
	t := session.term
	t.login.running = false
	t.login.activeLine = ""
	t.login.hasExitCode = false
	t.login.exitCode = 0
	t.login.outputCaptured = false
	t.login.capturedOutput = ""
	t.login.passwordPromptObserved = false
	t.login.passwordPromptVersion = 0
	t.login.secretPromptReplies = nil
	// A successful interactive authentication takes over the submitted command
	// without an OSC 133;D exit report. The new prompt is the readiness signal
	// for any remaining configured login commands.
	t.submittedLines = 0
	if t.promptStarted && !t.promptDirty {
		t.promptReady = true
	}
	if t.login.line == "" {
		t.login = loginCommand{results: t.login.results}
		return false
	}
	return true
}

// loginUnapprovedSecretPromptReady reports a stable secret prompt that is not
// an explicitly supported password prompt. Login scripts stop before sending
// the next configured line to verification-code, token, and similar prompts.
func loginUnapprovedSecretPromptReady(t *terminal, now time.Duration) bool {
	if t == nil || t.screen == nil || !t.login.seen || t.login.quietWait(now, loginCommandGrace) > 0 {
		return false
	}
	rowText := strings.TrimSpace(t.screen.RowText(t.screen.Cursor().Y))
	return rowText != "" && cmdhistory.PasswordPrompt(rowText) && !loginPasswordPrompt.MatchString(rowText)
}

// loginIncompletePromptReady preserves the shell-integration failure path: an
// A marker without its matching B marker must not wait for the full command
// timeout. A separately recognized interactive prompt may still hand off.
func loginIncompletePromptReady(t *terminal, now time.Duration) bool {
	return t != nil && t.screen != nil && t.promptStarted && !t.promptIdle() &&
		t.login.seen && t.login.quietWait(now, loginCommandGrace) == 0
}

func (state *State) loginStepFinished(session *Session, now time.Duration) bool {
	t := session.term
	if !t.login.running {
		return false
	}
	if loginPasswordPromptText(t) != "" {
		return false
	}
	if t.login.passwordPromptObserved && t.promptStarted && !t.promptDirty && t.promptVersion > t.login.passwordPromptVersion {
		return true
	}
	if t.promptIdle() && !t.promptDirty && t.promptVersion > t.login.promptVersion {
		return true
	}
	if t.login.hasExitCode && ((t.login.exitCode != 0 && t.login.policy == loginFailureStop) || (t.login.line == "" || t.login.isWorkspaceStep)) {
		return true
	}
	if state.loginInteractivePromptReady(t) {
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
	cmd := t.login.redactedCommand(t.login.activeIndex, t.login.activeLine)
	code := t.login.exitCode
	output := t.login.redactSecrets(t.login.finalOutput(t))
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
	commandIndex := t.login.next
	output := ""
	if t.login.running {
		step = t.login.activeIndex + 1
		cmd = t.login.activeLine
		commandIndex = t.login.activeIndex
		output = t.login.redactSecrets(t.login.finalOutput(t))
	}
	cmd = t.login.redactedCommand(commandIndex, cmd)
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
		if prompt := loginPasswordPromptText(t); prompt != "" && state.handleLoginPasswordPrompt(session, prompt, now) {
			return
		}
		if t.login.hasRemainingSteps() && loginUnapprovedSecretPromptReady(t, now) {
			state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			return
		}
		if loginIncompletePromptReady(t, now) && !state.loginInteractivePromptReady(t) {
			if t.login.hasRemainingSteps() {
				state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			} else {
				t.login = loginCommand{results: t.login.results}
			}
			return
		}
		if state.loginStepFinished(session, now) {
			if t.login.passwordPromptObserved && !t.login.hasExitCode {
				if !state.completeLoginHandoff(session) {
					return
				}
			} else if !state.completeLoginStep(session) {
				return
			}
		} else {
			if !t.login.isWorkspaceStep {
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
		if !t.login.isWorkspaceStep && t.login.hasRemainingSteps() && loginUnapprovedSecretPromptReady(t, now) {
			state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			return
		}
		if !t.login.isWorkspaceStep && t.login.hasRemainingSteps() && loginIncompletePromptReady(t, now) &&
			!state.loginInteractivePromptReady(t) {
			state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			return
		}
		if t.login.isWorkspaceStep {
			if !t.promptIdle() || t.promptDirty || t.promptVersion <= t.login.promptVersion {
				return
			}
		} else if t.folderIntegrationSeen {
			if !t.promptIdle() || t.promptDirty || t.promptVersion <= t.login.promptVersion {
				if !state.loginInteractivePromptReady(t) {
					return
				}
			}
		} else {
			if !t.login.seen || t.login.quietWait(now, loginCommandQuiet) > 0 {
				return
			}
			rowText := strings.TrimSpace(t.screen.RowText(t.screen.Cursor().Y))
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
		// Wait for the shell's prompt marker, or a stable interactive prompt
		// whose next answer is part of this configured sequence.
		if !t.login.seen {
			if !t.login.local && !t.login.isWorkspaceStep && t.login.hasRemainingSteps() && now-t.login.armedAt >= loginCommandExecTimeout {
				state.abortLoginCommands(session, loginAbortTimeout)
			}
			return
		}
		if !state.loginInteractivePromptReady(t) {
			if !t.login.isWorkspaceStep && t.login.hasRemainingSteps() && loginIncompletePromptReady(t, now) {
				state.abortLoginCommands(session, loginAbortAbnormalPrompt)
			}
			return
		}
	case t.login.quietWait(now, state.loginQuiet(session)) > 0:
		return
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
	t.login.passwordPromptObserved = false
	t.login.passwordPromptVersion = 0
	t.login.secretPromptReplies = nil
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
