package workbench

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
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

// loginSendMode chooses how multi-line post-login commands pace between lines.
type loginSendMode uint8

const (
	// loginSendWaitPrompt waits for the shell prompt (or per-line @expect)
	// before sending the next line.
	loginSendWaitPrompt loginSendMode = iota
	// loginSendFixedInterval sends lines sequentially at a fixed interval.
	loginSendFixedInterval
)

const (
	defaultLoginCommandFixedInterval = 500 * time.Millisecond
	maxLoginCommandInterval          = 60 * time.Second
	maxLoginCommandExpectTimeout     = 10 * time.Minute
)

func (host Host) savedLoginCommandMode() string {
	if host.LoginCommandMode == loginSendFixedInterval {
		return "interval"
	}
	return ""
}

func (host *Host) applyLoginCommandMode(saved string) {
	switch strings.TrimSpace(saved) {
	case "interval", "fixed", "delay":
		host.LoginCommandMode = loginSendFixedInterval
	default:
		host.LoginCommandMode = loginSendWaitPrompt
	}
}

func parseLoginCommandInterval(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0, true
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 || d > maxLoginCommandInterval {
		return 0, false
	}
	return d, true
}

func formatLoginCommandInterval(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func (host Host) savedLoginCommandInterval() string {
	return formatLoginCommandInterval(host.LoginCommandInterval)
}

func (host *Host) applyLoginCommandInterval(saved string) {
	if d, ok := parseLoginCommandInterval(saved); ok {
		host.LoginCommandInterval = d
	} else {
		host.LoginCommandInterval = 0
	}
}

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

type loginStep struct {
	command       string
	hasDelay      bool
	delay         time.Duration
	expect        string
	expectRE      *regexp.Regexp
	hasTimeout    bool
	expectTimeout time.Duration
	secret        bool
}

func isSecretExpect(pattern string) bool {
	if pattern == "" {
		return false
	}
	if len(pattern) >= 3 && strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") {
		pattern = pattern[1 : len(pattern)-1]
	}
	if cmdhistory.PasswordPrompt(pattern) {
		return true
	}
	for _, kw := range []string{"密码", "口令", "验证码", "令牌"} {
		if strings.Contains(pattern, kw) {
			return true
		}
	}
	words := strings.FieldsFunc(strings.ToLower(pattern), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		switch w {
		case "password", "passphrase", "passcode", "verification", "secret", "token", "pin", "otp", "credential":
			return true
		}
	}
	return false
}

func redactedLoginCommand(cmd string, secret bool) string {
	if secret {
		return "***"
	}
	return cmd
}

func scrubSecretFromOutput(output, secret string) string {
	if output == "" || secret == "" {
		return output
	}
	return strings.ReplaceAll(output, secret, "***")
}

// parseLoginStepsWithValidation parses a single-line or multi-line login
// command configuration into executable steps and reports whether all # @
// directives are valid and followed by a command line.
//
// Supported directives:
//
//	# @delay <duration>          (or @wait / @interval)
//	# @expect <substring>
//	# @expect-timeout <duration>
//	# @secret
func parseLoginStepsWithValidation(value string) ([]loginStep, bool) {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	var steps []loginStep
	var pendingDelay time.Duration
	var hasPendingDelay bool
	var pendingExpect string
	var pendingExpectRE *regexp.Regexp
	var pendingTimeout time.Duration
	var hasPendingTimeout bool
	var pendingSecret bool
	valid := true
	for _, raw := range strings.Split(value, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			directive := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if strings.HasPrefix(directive, "@") {
				key, arg, hasArg := strings.Cut(directive, " ")
				arg = strings.TrimSpace(arg)
				switch strings.ToLower(key) {
				case "@delay", "@wait", "@interval":
					if !hasArg || arg == "" {
						valid = false
						continue
					}
					if d, ok := parseLoginCommandInterval(arg); ok {
						pendingDelay = d
						hasPendingDelay = true
					} else {
						valid = false
					}
				case "@expect":
					if !hasArg || arg == "" {
						valid = false
						continue
					}
					pendingExpect = arg
					pendingExpectRE = nil
					if len(arg) >= 3 && strings.HasPrefix(arg, "/") && strings.HasSuffix(arg, "/") {
						re, err := regexp.Compile(arg[1 : len(arg)-1])
						if err != nil {
							valid = false
							continue
						}
						pendingExpectRE = re
					}
				case "@expect-timeout":
					if !hasArg || arg == "" {
						valid = false
						continue
					}
					if d, err := time.ParseDuration(arg); err == nil && d > 0 && d <= maxLoginCommandExpectTimeout {
						pendingTimeout = d
						hasPendingTimeout = true
					} else {
						valid = false
					}
				case "@secret":
					if arg != "" {
						valid = false
						continue
					}
					pendingSecret = true
				default:
					valid = false
				}
			}
			continue
		}
		steps = append(steps, loginStep{
			command:       line,
			hasDelay:      hasPendingDelay,
			delay:         pendingDelay,
			expect:        pendingExpect,
			expectRE:      pendingExpectRE,
			hasTimeout:    hasPendingTimeout,
			expectTimeout: pendingTimeout,
			secret:        pendingSecret || isSecretExpect(pendingExpect),
		})
		pendingDelay = 0
		hasPendingDelay = false
		pendingExpect = ""
		pendingExpectRE = nil
		pendingTimeout = 0
		hasPendingTimeout = false
		pendingSecret = false
	}
	if hasPendingDelay || pendingExpect != "" || hasPendingTimeout || pendingSecret {
		valid = false
	}
	return steps, valid
}

func parseLoginSteps(value string) []loginStep {
	steps, _ := parseLoginStepsWithValidation(value)
	return steps
}

func validLoginCommandDirectives(value string) bool {
	_, ok := parseLoginStepsWithValidation(value)
	return ok
}

// parseLoginCommands splits a single-line or multi-line login command
// configuration into executable lines in order, ignoring blank lines and
// comment lines (whose trimmed text begins with '#') while keeping quotes,
// arguments, pipes and redirections intact.
func parseLoginCommands(value string) []string {
	steps := parseLoginSteps(value)
	if len(steps) == 0 {
		return nil
	}
	commands := make([]string, len(steps))
	for i, step := range steps {
		commands[i] = step.command
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
	Step        int
	Total       int
	Command     string
	Output      string
	ExitCode    int
	HasExitCode bool
	Success     bool
	Aborted     string
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
	loginAbortExpectTimeout
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
	steps           []loginStep
	next            int
	mode            loginSendMode
	interval        time.Duration
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
	waitingDelay    bool
	delayUntil      time.Duration
	// local marks a local shell's startup command. Such a shell reports its
	// prompt from the moment it starts, because the integration is part of its
	// launch, so the first prompt carries no command-start marker and promptReady
	// never turns true before the first command; it is typed at the prompt's end
	// instead.
	local bool

	// Active running post-login command tracking.
	running         bool
	activeIndex     int
	activeLine      string
	activeSecret    bool
	sentAt          time.Duration
	commandStarted  bool
	hasExitCode     bool
	exitCode        int
	beforeLines     []string
	beforePrompt    string
	outputCaptured  bool
	capturedOutput  string
	expectBuf       string
	expectANSIState uint8
	results         []LoginCommandResult
}

func (login *loginCommand) arm(line string) {
	login.armSteps(parseLoginSteps(line), loginSendWaitPrompt, 0, loginFailureStop)
}

func (login *loginCommand) armLines(lines []string, policy loginFailurePolicy) {
	steps := make([]loginStep, len(lines))
	for i, line := range lines {
		steps[i] = loginStep{command: line}
	}
	login.armSteps(steps, loginSendWaitPrompt, 0, policy)
}

func (login *loginCommand) armSteps(steps []loginStep, mode loginSendMode, interval time.Duration, policy loginFailurePolicy) {
	if len(steps) == 0 {
		*login = loginCommand{}
		return
	}
	copiedSteps := append([]loginStep(nil), steps...)
	lines := make([]string, len(copiedSteps))
	for i, step := range copiedSteps {
		lines[i] = step.command
	}
	*login = loginCommand{
		line:     lines[0],
		lines:    lines,
		steps:    copiedSteps,
		mode:     mode,
		interval: interval,
		policy:   policy,
	}
}

func (login *loginCommand) effectiveInterval() time.Duration {
	if login.interval > 0 {
		return login.interval
	}
	if login.mode == loginSendFixedInterval {
		return defaultLoginCommandFixedInterval
	}
	return 0
}

func (login *loginCommand) stepAt(index int) loginStep {
	if index >= 0 && index < len(login.steps) {
		return login.steps[index]
	}
	if index >= 0 && index < len(login.lines) {
		return loginStep{command: login.lines[index]}
	}
	return loginStep{}
}

func (login *loginCommand) delayForStep(index int) time.Duration {
	step := login.stepAt(index)
	if step.hasDelay {
		return step.delay
	}
	if index > 0 {
		return login.effectiveInterval()
	}
	return 0
}

func (login *loginCommand) nextExpect() string {
	if login.isWorkspaceStep || login.line == "" {
		return ""
	}
	return login.stepAt(login.next).expect
}

func (login *loginCommand) nextExpectTimeout() time.Duration {
	if login.isWorkspaceStep || login.line == "" {
		return loginCommandExecTimeout
	}
	if step := login.stepAt(login.next); step.hasTimeout && step.expectTimeout > 0 {
		return step.expectTimeout
	}
	return loginCommandExecTimeout
}

const maxLoginExpectBuf = 16 * 1024

func (login *loginCommand) appendExpectData(data []byte) {
	if len(data) == 0 {
		return
	}
	var b strings.Builder
	b.Grow(len(data))
	for _, ch := range data {
		switch login.expectANSIState {
		case 0:
			switch {
			case ch == 0x1b:
				login.expectANSIState = 1
			case ch == '\r' || ch == '\n' || ch == '\t' || (ch >= 0x20 && ch != 0x7f):
				b.WriteByte(ch)
			}
		case 1:
			switch ch {
			case '[':
				login.expectANSIState = 2
			case ']', 'P', 'X', '^', '_':
				login.expectANSIState = 3
			default:
				login.expectANSIState = 0
			}
		case 2:
			if ch >= 0x40 && ch <= 0x7e {
				login.expectANSIState = 0
			}
		case 3:
			if ch == 0x07 {
				login.expectANSIState = 0
			} else if ch == 0x1b {
				login.expectANSIState = 4
			}
		case 4:
			if ch == '\\' {
				login.expectANSIState = 0
			} else if ch != 0x1b {
				login.expectANSIState = 3
			}
		}
	}
	if b.Len() == 0 {
		return
	}
	login.expectBuf += b.String()
	if len(login.expectBuf) > maxLoginExpectBuf {
		login.expectBuf = login.expectBuf[len(login.expectBuf)-maxLoginExpectBuf:]
	}
}

func (login *loginCommand) expectText() string {
	if login.expectBuf == "" {
		return ""
	}
	text := strings.ReplaceAll(strings.ReplaceAll(login.expectBuf, "\r\n", "\n"), "\r", "\n")
	if login.activeLine != "" {
		trimmed := strings.TrimLeft(text, " \t\n")
		if strings.HasPrefix(trimmed, login.activeLine) {
			rest := strings.TrimPrefix(trimmed, login.activeLine)
			// Only consume the echoed command line once a newline or trailing separator follows it
			// (or if it is the complete echoed line so far, don't match the command against itself).
			if rest == "" || strings.HasPrefix(rest, "\n") {
				text = strings.TrimLeft(rest, "\n")
			}
		}
	}
	return text
}

func (login *loginCommand) matchesExpect(step loginStep) bool {
	if step.expect == "" {
		return false
	}
	text := login.expectText()
	if text == "" {
		return false
	}
	if step.expectRE != nil {
		return step.expectRE.MatchString(text)
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(step.expect))
}

func (login *loginCommand) isSecretStepAtSend(t *terminal, step loginStep) bool {
	if step.secret || isSecretExpect(step.expect) {
		return true
	}
	if text := strings.TrimSpace(login.expectText()); text != "" {
		lines := strings.Split(text, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
				return cmdhistory.PasswordPrompt(trimmed) || isSecretExpect(trimmed)
			}
		}
	}
	if (login.next == 0 || login.seen) && t != nil && t.screen != nil {
		row := t.screen.RowText(t.screen.Cursor().Y)
		if cmdhistory.PasswordPrompt(row) || isSecretExpect(row) {
			return true
		}
	}
	return false
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
	return login.waitingDelay || len(login.lines) > 1 || (len(login.lines) == 1 && login.workspace != "" && login.loginFirst)
}

// output notes that the shell wrote something at now, while a command waits or runs.
func (login *loginCommand) output(data []byte, now time.Duration) {
	if login.line != "" || login.running {
		login.last, login.seen = now, true
	}
	if login.running && !login.isWorkspaceStep {
		login.appendExpectData(data)
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
	if expect := t.login.nextExpect(); expect != "" {
		return t.login.matchesExpect(t.login.stepAt(t.login.next))
	}
	if t.login.mode == loginSendFixedInterval && !t.login.isWorkspaceStep {
		return !t.login.waitingDelay || now >= t.login.delayUntil
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
	rawCmd := t.login.activeLine
	secret := t.login.activeSecret
	cmd := redactedLoginCommand(rawCmd, secret)
	matchedExpect := t.login.nextExpect() != ""
	hasExit := t.login.hasExitCode || (!matchedExpect && t.login.mode != loginSendFixedInterval)
	code := -1
	success := true
	if hasExit {
		code = t.login.exitCode
		success = code == 0
	}
	output := t.login.finalOutput(t)
	if secret {
		output = scrubSecretFromOutput(output, rawCmd)
	}
	result := LoginCommandResult{
		Step:        step,
		Total:       total,
		Command:     cmd,
		Output:      output,
		ExitCode:    code,
		HasExitCode: hasExit,
		Success:     success,
	}
	t.login.results = append(t.login.results, result)
	session.loginResults = append(session.loginResults[:0], t.login.results...)
	t.login.running = false
	t.login.activeLine = ""
	t.login.activeSecret = false
	t.login.hasExitCode = false
	t.login.exitCode = 0
	t.login.outputCaptured = false
	t.login.capturedOutput = ""
	t.login.expectBuf = ""
	t.login.expectANSIState = 0

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
		if hasExit {
			state.status = fmt.Sprintf(m.LoginCommandStepSuccessFmt, step, total, code, cmd)
		} else {
			state.status = fmt.Sprintf(m.LoginCommandStepSentFmt, step, total)
		}
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
	rawCmd := t.login.line
	secret := t.login.isSecretStepAtSend(t, t.login.stepAt(t.login.next))
	output := ""
	if t.login.running {
		step = t.login.activeIndex + 1
		rawCmd = t.login.activeLine
		secret = t.login.activeSecret
		output = t.login.finalOutput(t)
		if secret {
			output = scrubSecretFromOutput(output, rawCmd)
		}
	}
	cmd := redactedLoginCommand(rawCmd, secret)
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
	case loginAbortExpectTimeout:
		expect := t.login.nextExpect()
		if isSecretExpect(expect) {
			expect = "***"
		}
		msg = fmt.Sprintf(m.LoginCommandAbortExpectTimeoutFmt, expect, cmd)
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
	if !t.login.local && !t.login.paused && !t.login.isWorkspaceStep && t.login.hasRemainingSteps() {
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
			matchedExpect := t.login.nextExpect() != ""
			if !state.completeLoginStep(session) {
				return
			}
			if matchedExpect {
				t.login.waitPrompt = false
			}
			// If the next step (when not already waited via waitingDelay) has a
			// configured delay, arm waitingDelay before sending it.
			if t.login.line != "" && !t.login.isWorkspaceStep && !t.login.waitingDelay && t.login.mode != loginSendFixedInterval {
				if delay := t.login.delayForStep(t.login.next); delay > 0 {
					t.login.waitingDelay = true
					t.login.delayUntil = now + delay
					t.login.waitPrompt = false
					state.status = fmt.Sprintf(state.messages.Tools.LoginCommandWaitingIntervalFmt, t.login.next+1, max(len(t.login.lines), 1), delay.String())
					return
				}
			}
			t.login.waitingDelay = false
			if matchedExpect && t.login.line != "" && !t.login.paused {
				state.sendInitializationCommand(session, t)
				return
			}
		} else {
			if !t.login.isWorkspaceStep {
				if expect := t.login.nextExpect(); expect != "" {
					state.status = fmt.Sprintf(state.messages.Tools.LoginCommandWaitingExpectFmt, t.login.next+1, max(len(t.login.lines), 1))
					if now-t.login.sentAt >= t.login.nextExpectTimeout() {
						state.abortLoginCommands(session, loginAbortExpectTimeout)
					}
					return
				}
				if t.login.mode == loginSendFixedInterval && t.login.waitingDelay {
					return
				}
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
	if t.login.waitingDelay {
		if now < t.login.delayUntil {
			return
		}
		t.login.waitingDelay = false
		state.sendInitializationCommand(session, t)
		return
	}
	if t.login.waitPrompt {
		if t.login.isWorkspaceStep {
			if !t.promptIdle() || t.promptDirty || t.promptVersion <= t.login.promptVersion {
				return
			}
		} else if t.login.mode == loginSendFixedInterval {
			// Fixed interval mode paces between steps via waitingDelay rather than waiting for a prompt.
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
		state.sendInitializationCommand(session, t)
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
	if !t.login.local && !t.login.isWorkspaceStep && t.login.next == 0 {
		if delay := t.login.delayForStep(0); delay > 0 {
			t.login.waitingDelay = true
			t.login.delayUntil = now + delay
			state.status = fmt.Sprintf(state.messages.Tools.LoginCommandWaitingIntervalFmt, 1, max(len(t.login.lines), 1), delay.String())
			return
		}
	}
	state.sendInitializationCommand(session, t)
}

// requestLoginCommandFrames schedules the existing quiet fallback only while
// an unreported prompt still has an initialization command waiting.
func (state *State) requestLoginCommandFrames(canvas *gfx.Canvas) {
	now := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		t := session.term
		if t == nil || t.login.paused || t.shell == nil || t.ended {
			continue
		}
		if t.login.waitingDelay {
			canvas.RequestFrameIn(max(t.login.delayUntil-now, time.Millisecond))
			continue
		}
		if t.login.running && !t.login.isWorkspaceStep && t.login.nextExpect() != "" {
			if rem := t.login.sentAt + t.login.nextExpectTimeout() - now; rem > 0 {
				canvas.RequestFrameIn(max(rem, time.Millisecond))
			}
			continue
		}
		if t.login.line == "" || (t.login.waitPrompt && t.login.isWorkspaceStep) || !t.login.seen || t.folderIntegrationSeen {
			continue
		}
		quiet := state.loginQuiet(session)
		if t.login.waitPrompt || t.login.running {
			quiet = loginCommandQuiet
		}
		canvas.RequestFrameIn(max(t.login.quietWait(now, quiet), time.Millisecond))
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
	steps := parseLoginSteps(session.host.LoginCommand)
	command := session.workspaceCommand
	switch {
	case len(steps) > 0 && command.Command != "":
		t.login.armSteps(steps, session.host.LoginCommandMode, session.host.LoginCommandInterval, session.host.LoginCommandPolicy)
		t.login.workspace = command.Command
		t.login.loginFirst = true
	case len(steps) > 0:
		t.login.armSteps(steps, session.host.LoginCommandMode, session.host.LoginCommandInterval, session.host.LoginCommandPolicy)
	case command.Command != "":
		t.login.arm(command.Command)
		t.login.workspace = command.Command
		t.login.isWorkspaceStep = true
	}
}

func (t *terminal) pauseInitialization() {
	if t.login.local {
		t.login = loginCommand{} // never interleave startup with user/assistant input
	} else if t.login.workspace != "" || t.login.hasPendingOrRunningLogin() {
		t.login.paused = true
		t.login.waitingDelay = false
	}
}

func (state *State) sendInitializationCommand(session *Session, t *terminal) {
	login := t.login
	wasAtPrompt := t.promptIdle()
	prevSubmitted := t.submittedLines
	step := login.stepAt(login.next)
	secret := login.isSecretStepAtSend(t, step)
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
	if login.next > 0 && !wasAtPrompt {
		t.submittedLines = prevSubmitted
	}
	if login.local || login.isWorkspaceStep || (singleLine && login.mode != loginSendFixedInterval) {
		t.login = loginCommand{results: t.login.results}
		return
	}
	now := state.now()
	total := len(login.lines)
	if total == 0 {
		total = 1
	}
	if login.mode == loginSendFixedInterval && login.next+1 >= len(login.lines) && (login.workspace == "" || !login.loginFirst) {
		t.login.results = append(t.login.results, LoginCommandResult{
			Step:        login.next + 1,
			Total:       total,
			Command:     redactedLoginCommand(login.line, secret),
			ExitCode:    -1,
			HasExitCode: false,
			Success:     true,
		})
		if session != nil {
			session.loginResults = append(session.loginResults[:0], t.login.results...)
		}
		state.status = fmt.Sprintf(state.messages.Tools.LoginCommandStepSentFmt, login.next+1, total)
		t.login = loginCommand{results: t.login.results}
		return
	}
	t.login.running = true
	t.login.activeIndex = t.login.next
	t.login.activeLine = login.line
	t.login.activeSecret = secret
	t.login.sentAt = now
	t.login.promptVersion = t.promptVersion
	t.login.commandStarted = false
	t.login.hasExitCode = false
	t.login.exitCode = 0
	t.login.outputCaptured = false
	t.login.capturedOutput = ""
	t.login.expectBuf = ""
	t.login.expectANSIState = 0
	t.login.seen = false
	t.login.waitingDelay = false
	t.login.next++
	if t.login.next < len(t.login.lines) {
		t.login.line = t.login.lines[t.login.next]
		t.login.waitPrompt = true
		if t.login.mode == loginSendFixedInterval && t.login.nextExpect() == "" {
			delay := t.login.delayForStep(t.login.next)
			if delay > 0 {
				t.login.waitingDelay = true
				t.login.delayUntil = now + delay
				state.status = fmt.Sprintf(state.messages.Tools.LoginCommandWaitingIntervalFmt, t.login.next+1, total, delay.String())
				return
			}
		}
		if expect := t.login.nextExpect(); expect != "" {
			state.status = fmt.Sprintf(state.messages.Tools.LoginCommandWaitingExpectFmt, t.login.next+1, total)
		} else {
			state.status = fmt.Sprintf(state.messages.Tools.LoginCommandStepSentFmt, t.login.activeIndex+1, total)
		}
	} else if t.login.workspace != "" && t.login.loginFirst {
		t.login.line = t.login.workspace
		t.login.isWorkspaceStep = true
		t.login.waitPrompt = true
		state.status = fmt.Sprintf(state.messages.Tools.LoginCommandStepSentFmt, t.login.activeIndex+1, total)
	} else {
		t.login.line = ""
		t.login.waitPrompt = true
		state.status = fmt.Sprintf(state.messages.Tools.LoginCommandStepSentFmt, t.login.activeIndex+1, total)
	}
}
