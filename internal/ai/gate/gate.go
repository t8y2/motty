// Package gate is the one door between a model (the in-app assistant or an
// external MCP client) and the user's sessions. A tool call arrives as a name
// and raw JSON; the gate decodes it strictly, rates its risk, asks the user when
// the policy says to, and only then reaches the Backend. Nothing else in the
// assistant touches a session, so the approval rules cannot be bypassed by a new
// caller.
package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// SessionInfo describes one open session for list_sessions.
type SessionInfo struct {
	ID      string // the handle a tool call passes as "session"
	Title   string
	Address string
	Active  bool
	Ready   bool // a shell is open and can take commands
}

type HostInfo struct {
	ID      string
	Title   string
	Address string
}

type HostLister interface {
	Hosts(context.Context) ([]HostInfo, error)
}

// ExecResult is what a command printed and how it ended.
type ExecResult struct {
	Output     string
	ExitCode   int
	Truncated  bool
	TimedOut   bool
	Retained   string
	RetainLost bool
}

// FileEntry is one SFTP directory entry.
type FileEntry struct {
	Name string
	Size int64
	Dir  bool
}

// Backend carries out tool calls on real sessions. Implementations must be safe
// to call from any goroutine and must honour ctx.
type Backend interface {
	Sessions(ctx context.Context) ([]SessionInfo, error)
	ReadTerminal(ctx context.Context, args tools.TerminalReadArgs) (string, error)
	Exec(ctx context.Context, args tools.TerminalExecArgs) (ExecResult, error)
	// JobStart types a command into the session's terminal and returns at once.
	// The job belongs to OutputOwner(ctx); JobPoll and JobStop refuse any other.
	JobStart(ctx context.Context, args tools.TerminalStartArgs) (JobStatus, error)
	JobPoll(ctx context.Context, args tools.TerminalPollArgs) (JobStatus, error)
	JobStop(ctx context.Context, args tools.TerminalStopArgs) (JobStatus, error)
	ListDir(ctx context.Context, args tools.SFTPListArgs) ([]FileEntry, error)
	ReadFile(ctx context.Context, args tools.SFTPReadArgs) (data string, truncated bool, err error)
	WriteFile(ctx context.Context, args tools.SFTPWriteArgs) error
	Remove(ctx context.Context, args tools.SFTPRemoveArgs) error
}

// Request is what the user is asked to approve.
type Request struct {
	Tool         string
	Kind         tools.Kind
	Session      string
	SessionLabel string // immutable target name/address for the approval UI
	Summary      string // the command, or "write /path (N bytes)"
	Assessment   risk.Assessment
	// NeedsText is set for a critical action: the user must type Confirm to go on.
	NeedsText bool
	Confirm   string
}

// Approver shows the request to the user and returns their answer. It blocks
// until answered or ctx ends. confirmText is what they typed when NeedsText.
type Approver func(ctx context.Context, request Request) (approved bool, confirmText string, err error)

// Mode selects the caller's capabilities and risk-approval behavior.
type Mode uint8

const (
	// ModeAsk applies the risk policy: low-risk actions run, the rest ask.
	ModeAsk Mode = iota
	// ModeObserve allows only read tools; anything else is refused.
	ModeObserve
	// ModeAuto skips questions up to the policy's level like ModeAsk but also
	// runs medium actions; high and critical still ask.
	ModeAuto
	// ModeAllowAll skips approvals, including cross-connection approvals, but
	// preserves explicit policy blocks. Callers
	// must obtain explicit consent before enabling this mode.
	ModeAllowAll
)

// Gate decides and runs tool calls.
type Gate struct {
	Backend  Backend
	Policy   risk.Policy
	Mode     Mode
	Approver Approver // nil means nothing that needs approval can run
	Outputs  *OutputStore
	// Target is an in-app request's immutable default. References contains only
	// sessions explicitly mentioned in this question. Nil Target retains the
	// external MCP contract; references never change the default.
	Target     *SessionInfo
	References []SessionInfo
}

func (g Gate) targetSession(session string) (string, error) {
	if g.Target == nil {
		return session, nil
	}
	if session != "" {
		if session == g.Target.ID {
			return session, nil
		}
		for _, reference := range g.References {
			if reference.ID == session {
				return session, nil
			}
		}
		return "", errors.New("session is outside this request's locked target and explicit @references; mention that session in a new request")
	}
	if g.Target.ID == "" {
		return "", errors.New("no default target selected; specify a session explicitly mentioned in this question or choose a default connection")
	}
	return g.Target.ID, nil
}

// ConfirmWord is what a user types to approve a critical action.
const ConfirmWord = "CONFIRM"

// Result is the text sent back to the model and whether it is an error.
type Result struct {
	Text       string
	IsError    bool
	Structured json.RawMessage
	// Denied is set when the user (or the mode) refused the action.
	Denied bool
}

// ErrDenied is returned by helpers when an action was refused.
var ErrDenied = errors.New("gate: action not approved")

// Run executes one tool call.
func (g Gate) Run(ctx context.Context, name string, raw []byte) Result {
	spec, ok := tools.Lookup(name)
	if !ok {
		return Result{Text: "unknown tool " + name, IsError: true}
	}
	if g.Mode == ModeObserve && spec.Kind != tools.Read {
		return Result{Text: "this connection is read-only: " + name + " is not allowed", IsError: true, Denied: true}
	}
	if result, handled := g.runHostTool(ctx, name, raw); handled {
		return result
	}
	switch name {
	case tools.NameListHosts:
		if _, err := tools.DecodeListHosts(raw); err != nil {
			return failure(err)
		}
		if g.Target != nil {
			return Result{Text: "host listing is unavailable in a session-scoped request", IsError: true}
		}
		lister, ok := g.Backend.(HostLister)
		if !ok {
			return Result{Text: "host listing is unavailable", IsError: true}
		}
		hosts, err := lister.Hosts(ctx)
		if err != nil {
			return failure(err)
		}
		return Result{Text: formatHosts(hosts)}
	case tools.NameListSessions:
		if _, err := tools.DecodeListSessions(raw); err != nil {
			return failure(err)
		}
		sessions, err := g.Backend.Sessions(ctx)
		if err != nil {
			return failure(err)
		}
		if g.Target != nil {
			filtered := make([]SessionInfo, 0, 1+len(g.References))
			for _, session := range sessions {
				if _, err := g.targetSession(session.ID); err == nil {
					session.Active = session.ID == g.Target.ID
					filtered = append(filtered, session)
				}
			}
			sessions = filtered
		}
		return Result{Text: formatSessions(sessions)}
	case tools.NameTerminalRead:
		args, err := tools.DecodeTerminalRead(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		text, err := g.Backend.ReadTerminal(ctx, args)
		if err != nil {
			return failure(err)
		}
		return Result{Text: text}
	case tools.NameToolOutputRead:
		args, err := tools.DecodeToolOutputRead(raw)
		if err != nil {
			return failure(err)
		}
		if g.Outputs == nil {
			return Result{Text: "output paging is unavailable", IsError: true}
		}
		item, data, next, err := g.Outputs.read(outputOwner(ctx), args)
		if err != nil {
			return failure(err)
		}
		sessions, err := g.Backend.Sessions(ctx)
		if err != nil {
			return failure(err)
		}
		allowed := false
		for _, session := range sessions {
			if session.ID == item.session && session.Ready {
				allowed = true
				break
			}
		}
		if !allowed {
			return Result{Text: "the original session is no longer available", IsError: true}
		}
		page := struct {
			Data       string `json:"data"`
			NextOffset int    `json:"next_offset"`
			EOF        bool   `json:"eof"`
		}{data, next, next == len(item.data)}
		structured, _ := json.Marshal(page)
		return Result{Text: string(structured), Structured: structured}
	case tools.NameSFTPList:
		args, err := tools.DecodeSFTPList(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		entries, err := g.Backend.ListDir(ctx, args)
		if err != nil {
			return failure(err)
		}
		return Result{Text: formatEntries(entries)}
	case tools.NameSFTPRead:
		args, err := tools.DecodeSFTPRead(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		// Reading a private key or shadow file is a read, but not a harmless one.
		if a := risk.Classify("cat " + shellQuote(args.Path)); a.Level > risk.Low {
			if denied := g.ask(ctx, name, tools.Read, args.Session, "read "+args.Path, a); denied != nil {
				return *denied
			}
		}
		data, truncated, err := g.Backend.ReadFile(ctx, args)
		if err != nil {
			return failure(err)
		}
		if truncated {
			data += "\n[truncated]"
		}
		return Result{Text: data}
	case tools.NameTerminalExec:
		args, err := tools.DecodeTerminalExec(raw)
		if err != nil {
			return failure(err)
		}
		owner := outputOwner(ctx)
		if g.Outputs != nil && owner != "" && args.Session == "" {
			if args.Session, err = g.readySession(ctx); err != nil {
				return failure(err)
			}
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		assessment := risk.Classify(args.Command)
		if denied := g.ask(ctx, name, tools.Exec, args.Session, args.Command, assessment); denied != nil {
			return *denied
		}
		args.RetainOutput = g.Outputs != nil && owner != ""
		result, err := g.Backend.Exec(ctx, args)
		if err != nil {
			return failure(err)
		}
		text := formatExec(result)
		var outputID string
		if args.RetainOutput && result.Truncated && ctx.Err() == nil {
			if result.RetainLost {
				text += "\n[full output unavailable: capture incomplete or retention limit exceeded]"
			} else {
				if !utf8.ValidString(result.Retained) {
					result.Retained = strings.ToValidUTF8(result.Retained, "�")
				}
				id, err := g.Outputs.save(owner, args.Session, result.Retained)
				if err == nil {
					outputID = id
					text += "\n[full output available via tool_output_read; output_id: " + id + "; total_bytes: " + fmt.Sprint(len(result.Retained)) + "]"
				} else {
					text += "\n[full output unavailable: retention failed]"
				}
			}
		}
		response := Result{Text: text, IsError: result.TimedOut}
		if args.RetainOutput {
			fields := struct {
				Preview          string `json:"preview"`
				OutputID         string `json:"output_id,omitempty"`
				ExitCode         int    `json:"exit_code"`
				Truncated        bool   `json:"truncated"`
				TimedOut         bool   `json:"timed_out"`
				SourceOutputLost bool   `json:"source_output_lost"`
				TotalBytes       *int   `json:"total_bytes,omitempty"`
			}{Preview: result.Output, OutputID: outputID, ExitCode: result.ExitCode, Truncated: result.Truncated, TimedOut: result.TimedOut, SourceOutputLost: result.Truncated && outputID == ""}
			if !result.Truncated {
				total := len(result.Output)
				fields.TotalBytes = &total
			} else if outputID != "" {
				total := len(result.Retained)
				fields.TotalBytes = &total
			}
			response.Structured, _ = json.Marshal(fields)
		}
		return response
	case tools.NameTerminalStart, tools.NameTerminalPoll, tools.NameTerminalStop:
		return g.runJob(ctx, name, raw)
	case tools.NameTerminalExecMany:
		args, err := tools.DecodeTerminalExecMany(raw)
		if err != nil {
			return failure(err)
		}
		for _, session := range args.Sessions {
			if _, err := g.targetSession(session); err != nil {
				return failure(err)
			}
		}
		return g.execMany(ctx, name, args)
	case tools.NameSFTPWrite:
		args, err := tools.DecodeSFTPWrite(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		assessment := risk.Classify("tee " + shellQuote(args.Path))
		if assessment.Level < risk.Medium {
			assessment.Level = risk.Medium
		}
		if args.Overwrite && assessment.Level < risk.High {
			assessment.Level = risk.High
			assessment.Reasons = append(assessment.Reasons, "overwrites an existing file")
		}
		summary := fmt.Sprintf("write %s (%d bytes)", args.Path, len(args.Content))
		if denied := g.ask(ctx, name, tools.Write, args.Session, summary, assessment); denied != nil {
			return *denied
		}
		if err := g.Backend.WriteFile(ctx, args); err != nil {
			return failure(err)
		}
		return Result{Text: "written " + args.Path}
	case tools.NameSFTPRemove:
		args, err := tools.DecodeSFTPRemove(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		assessment := risk.Classify("rm -- " + shellQuote(args.Path))
		if assessment.Level < risk.High {
			assessment.Level = risk.High
		}
		if denied := g.ask(ctx, name, tools.Write, args.Session, "remove "+args.Path, assessment); denied != nil {
			return *denied
		}
		if err := g.Backend.Remove(ctx, args); err != nil {
			return failure(err)
		}
		return Result{Text: "removed " + args.Path}
	}
	return Result{Text: "tool not implemented: " + name, IsError: true}
}

// readySession picks the session an exec without an explicit one runs in: the
// active session if it is ready, else the first ready one.
func (g Gate) readySession(ctx context.Context) (string, error) {
	sessions, err := g.Backend.Sessions(ctx)
	if err != nil {
		return "", err
	}
	var chosen string
	for _, session := range sessions {
		if session.Ready && (chosen == "" || session.Active) {
			chosen = session.ID
			if session.Active {
				break
			}
		}
	}
	return chosen, nil
}

// execMany runs one command on several sessions. The user is asked once, with
// every target named, and a command is never run on a host that was not in that
// list. A batch is always at least medium risk: even a harmless command is a
// bigger decision across several machines.
func (g Gate) execMany(ctx context.Context, name string, args tools.TerminalExecManyArgs) Result {
	sessions, err := g.Backend.Sessions(ctx)
	if err != nil {
		return failure(err)
	}
	labels := make(map[string]string, len(args.Sessions))
	var problems []string
	for _, id := range args.Sessions {
		label, ready := "", false
		for _, candidate := range sessions {
			if candidate.ID == id {
				label, ready = candidate.Title, candidate.Ready
			}
		}
		switch {
		case label == "" && !ready:
			problems = append(problems, id+" is not an open session")
		case !ready:
			problems = append(problems, id+" ("+label+") has no shell that can take commands")
		}
		labels[id] = label
	}
	if len(problems) > 0 {
		return Result{Text: "nothing was run: " + strings.Join(problems, "; "), IsError: true}
	}

	assessment := risk.Classify(args.Command)
	if assessment.Level < risk.Medium {
		assessment.Level = risk.Medium
	}
	assessment.Reasons = append(assessment.Reasons, fmt.Sprintf("runs on %d sessions", len(args.Sessions)))
	targets := make([]string, len(args.Sessions))
	for index, id := range args.Sessions {
		targets[index] = labels[id]
		if targets[index] == "" {
			targets[index] = id
		}
	}
	summary := fmt.Sprintf("on %d sessions (%s): %s", len(args.Sessions), strings.Join(targets, ", "), args.Command)
	if denied := g.ask(ctx, name, tools.Exec, args.Sessions[0], summary, assessment, args.Sessions[1:]...); denied != nil {
		return *denied
	}
	if g.Target != nil {
		// Approval can remain open while a tab closes or reconnects. A scoped
		// backend reports readiness for the bound lifetimes, not replacement
		// shells. Check the whole batch again before starting any command.
		current, err := g.Backend.Sessions(ctx)
		if err != nil {
			return failure(err)
		}
		for _, id := range args.Sessions {
			ready := false
			for _, session := range current {
				ready = ready || session.ID == id && session.Ready
			}
			if !ready {
				return Result{Text: "nothing was run: target " + id + " changed or disconnected while awaiting approval", IsError: true}
			}
		}
	}

	type outcome struct {
		result ExecResult
		err    error
	}
	outcomes := make([]outcome, len(args.Sessions))
	var wait sync.WaitGroup
	for index, id := range args.Sessions {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := g.Backend.Exec(ctx, tools.TerminalExecArgs{Session: id, Command: args.Command, TimeoutSeconds: args.TimeoutSeconds})
			outcomes[index] = outcome{result, err}
		}()
	}
	wait.Wait()

	var text strings.Builder
	failed := 0
	for index, id := range args.Sessions {
		if index > 0 {
			text.WriteString("\n\n")
		}
		fmt.Fprintf(&text, "== %s (%s) ==\n", id, targets[index])
		if outcomes[index].err != nil {
			failed++
			text.WriteString("[error: " + outcomes[index].err.Error() + "]")
			continue
		}
		if outcomes[index].result.TimedOut {
			failed++
		}
		text.WriteString(formatExec(outcomes[index].result))
	}
	return Result{Text: text.String(), IsError: failed == len(args.Sessions)}
}

// ask applies the policy and, when needed, the user's answer. It returns nil
// when the action may proceed and the result to send back otherwise.
func (g Gate) ask(ctx context.Context, tool string, kind tools.Kind, session, summary string, assessment risk.Assessment, otherSessions ...string) *Result {
	policy := g.Policy
	if g.Mode == ModeAuto && policy.AutoUpTo < risk.Medium {
		policy.AutoUpTo = risk.Medium
	}
	decision := policy.Decide(assessment)
	// Full access explicitly authorizes all reachable connections without an
	// approval, including references; it never overrides a hard policy block.
	if g.Mode == ModeAllowAll && decision != risk.DecisionBlock {
		return nil
	}
	if g.Target != nil && decision == risk.DecisionAuto {
		crossTarget := session != g.Target.ID
		for _, id := range otherSessions {
			crossTarget = crossTarget || id != g.Target.ID
		}
		// A reference alone is not permission to execute or modify that host.
		// Use the existing approval even for low-risk cross-connection commands.
		if crossTarget {
			decision = risk.DecisionAsk
		}
	}
	switch decision {
	case risk.DecisionAuto:
		return nil
	case risk.DecisionBlock:
		return &Result{Text: "blocked by policy: " + joinReasons(assessment), IsError: true, Denied: true}
	}
	if g.Approver == nil {
		return &Result{Text: "needs the user's approval, which cannot be asked here", IsError: true, Denied: true}
	}
	request := Request{Tool: tool, Kind: kind, Session: session, Summary: summary, Assessment: assessment}
	if g.Target != nil {
		labels := []string{g.sessionLabel(session)}
		for _, id := range otherSessions {
			labels = append(labels, g.sessionLabel(id))
		}
		request.SessionLabel = strings.Join(labels, "; ")
	}
	if decision == risk.DecisionConfirmText {
		request.NeedsText, request.Confirm = true, ConfirmWord
	}
	approved, typed, err := g.Approver(ctx, request)
	switch {
	case err != nil:
		return &Result{Text: "approval failed: " + err.Error(), IsError: true, Denied: true}
	case !approved:
		return &Result{Text: "the user declined this action", IsError: true, Denied: true}
	case request.NeedsText && strings.TrimSpace(typed) != ConfirmWord:
		return &Result{Text: "the confirmation text did not match; action not run", IsError: true, Denied: true}
	}
	return nil
}

func (g Gate) sessionLabel(id string) string {
	var info SessionInfo
	if g.Target != nil && id == g.Target.ID {
		info = *g.Target
	} else {
		for _, reference := range g.References {
			if reference.ID == id {
				info = reference
				break
			}
		}
	}
	if info.Title == "" {
		return id
	}
	if info.Address == "" {
		return info.Title
	}
	return info.Title + " · " + info.Address
}

func failure(err error) Result { return Result{Text: err.Error(), IsError: true} }

func joinReasons(a risk.Assessment) string {
	if len(a.Reasons) == 0 {
		return a.Level.String()
	}
	return strings.Join(a.Reasons, "; ")
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func formatSessions(sessions []SessionInfo) string {
	if len(sessions) == 0 {
		return "no sessions are open"
	}
	var b strings.Builder
	for _, s := range sessions {
		state := "not connected"
		if s.Ready {
			state = "ready"
		}
		active := ""
		if s.Active {
			active = " (active)"
		}
		fmt.Fprintf(&b, "%s: %s %s [%s]%s\n", s.ID, s.Title, s.Address, state, active)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatHosts(hosts []HostInfo) string {
	if len(hosts) == 0 {
		return "no hosts are open to agents"
	}
	var output strings.Builder
	for _, host := range hosts {
		fmt.Fprintf(&output, "%s: %s %s\n", host.ID, host.Title, host.Address)
	}
	return strings.TrimRight(output.String(), "\n")
}

func formatEntries(entries []FileEntry) string {
	if len(entries) == 0 {
		return "(empty)"
	}
	var b strings.Builder
	for _, e := range entries {
		if e.Dir {
			fmt.Fprintf(&b, "%s/\n", e.Name)
		} else {
			fmt.Fprintf(&b, "%s\t%d\n", e.Name, e.Size)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatExec(r ExecResult) string {
	var b strings.Builder
	b.WriteString(r.Output)
	if r.Output != "" && !strings.HasSuffix(r.Output, "\n") {
		b.WriteByte('\n')
	}
	switch {
	case r.TimedOut:
		b.WriteString("[timed out; the command may still be running on the host]")
	case r.ExitCode < 0 && r.Truncated:
		b.WriteString("[output truncated] [exit status unknown: the command returned to the prompt]")
	case r.ExitCode < 0:
		b.WriteString("[exit status unknown: the command returned to the prompt]")
	case r.Truncated:
		fmt.Fprintf(&b, "[output truncated] [exit status %d]", r.ExitCode)
	default:
		fmt.Fprintf(&b, "[exit status %d]", r.ExitCode)
	}
	return b.String()
}
