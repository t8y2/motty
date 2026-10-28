// Package tools defines the AI assistant's tools once, for both the in-app
// agent and the MCP server: names, typed arguments, strict decoders, JSON
// Schemas and LLM-facing descriptions. It performs no I/O.
package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Kind classifies what a tool may do; approval policy is decided from it.
type Kind uint8

const (
	// Read inspects state and changes nothing.
	Read Kind = iota
	// Exec runs a command in the user's terminal.
	Exec
	// Write changes remote files.
	Write
)

func (k Kind) String() string {
	switch k {
	case Read:
		return "read"
	case Exec:
		return "exec"
	case Write:
		return "write"
	}
	return "unknown"
}

// Spec describes one tool.
type Spec struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON Schema of the arguments object
	Kind        Kind
}

// RequestAgentModeArgs is what Ask mode's escalation tool takes. Command is
// optional: it lets the UI show what the model wants to run, but the Agent run
// starts from the original request and the model chooses there.
type RequestAgentModeArgs struct {
	Reason  string `json:"reason,omitempty"`
	Command string `json:"command,omitempty"`
}

// RequestAgentModeSpec is the escalation tool Ask mode alone offers. It is not
// in All: Agent mode and external MCP callers must never see it.
func RequestAgentModeSpec() Spec {
	return Spec{
		Name:        NameRequestAgentMode,
		Description: "Ask mode only. Call this when the user asks you to run, change, inspect a session, or otherwise use a tool that Ask mode withholds. It runs nothing; the application asks the user to switch to Agent mode and retry. Do not use it for an ordinary question that can be answered from the attached terminal text.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"reason":{"type":"string","maxLength":512,"description":"One short sentence saying what needs to run or why tools are needed."},` +
			`"command":{"type":"string","maxLength":8192,"description":"Optional command you would run if switched."}` +
			`},"additionalProperties":false}`),
		Kind: Read,
	}
}

// DecodeRequestAgentMode validates the escalation arguments.
func DecodeRequestAgentMode(raw []byte) (RequestAgentModeArgs, error) {
	var args RequestAgentModeArgs
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	args.Reason = strings.TrimSpace(args.Reason)
	args.Command = strings.TrimSpace(args.Command)
	if len(args.Reason) > MaxEscalationReason {
		return args, fmt.Errorf("reason is longer than %d bytes", MaxEscalationReason)
	}
	if len(args.Command) > MaxEscalationCommand {
		return args, fmt.Errorf("command is longer than %d bytes", MaxEscalationCommand)
	}
	return args, nil
}

// Tool names.
const (
	NameListSessions = "list_sessions"
	NameTerminalRead = "terminal_read"
	NameTerminalExec = "terminal_exec"
	// NameTerminalExecMany runs one command on several sessions at once.
	NameTerminalExecMany = "terminal_exec_many"
	NameSFTPList         = "sftp_list"
	NameSFTPRead         = "sftp_read"
	NameSFTPWrite        = "sftp_write"
	NameSFTPRemove       = "sftp_remove"

	// NameRequestAgentMode is the one tool Ask mode offers. It runs nothing:
	// the application asks the user to switch to Agent and retry.
	NameRequestAgentMode = "request_agent_mode"
)

// Limits and defaults.
const (
	MaxSessionLen         = 256
	MaxPathLen            = 4096
	MaxCommandBytes       = 8192
	MaxBatchSessions      = 8
	MaxContentBytes       = 262144
	MaxReadBytes          = 262144
	DefaultReadLines      = 60
	MaxReadLines          = 500
	DefaultTimeoutSeconds = 60
	MaxTimeoutSeconds     = 600
	MaxEscalationReason   = 512
	MaxEscalationCommand  = 8192
	ScopeScreen           = "screen"
	ScopeScrollback       = "scrollback"
)

var specs = []Spec{
	{
		Name:        NameListSessions,
		Description: "List the open terminal sessions with their ids, titles, hosts and connection state, and say which one is active. Call this first when you need a session id.",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Kind:        Read,
	},
	{
		Name:        NameTerminalRead,
		Description: "Read recent text from a terminal session without changing anything. Use scope \"screen\" for the visible screen or \"scrollback\" for earlier output.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"session":{"type":"string","maxLength":256,"description":"Session id from list_sessions. Defaults to the active session."},` +
			`"lines":{"type":"integer","minimum":1,"maximum":500,"default":60,"description":"Number of lines to return, counted from the end."},` +
			`"scope":{"type":"string","enum":["screen","scrollback"],"default":"screen","description":"Visible screen or scrollback history."}` +
			`},"additionalProperties":false}`),
		Kind: Read,
	},
	{
		Name:        NameTerminalExec,
		Description: "Run one shell command in the user's visible terminal session and return its output and exit status. The user sees it run and may refuse it. Use it for a single non-interactive command; it must finish within the timeout. Do not start interactive programs.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"session":{"type":"string","maxLength":256,"description":"Session id from list_sessions. Defaults to the active session."},` +
			`"command":{"type":"string","minLength":1,"maxLength":8192,"description":"The shell command line to run."},` +
			`"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":60,"description":"Give up waiting after this many seconds."}` +
			`},"required":["command"],"additionalProperties":false}`),
		Kind: Exec,
	},
	{
		Name:        NameTerminalExecMany,
		Description: "Run the same shell command on several terminal sessions at once and return each session's output and exit status, labelled by session. The user approves the command once, seeing every target. Use it to check or change the same thing across hosts; use terminal_exec for one host. It must finish within the timeout on every host. Do not start interactive programs.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"sessions":{"type":"array","minItems":2,"maxItems":8,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":256},"description":"Session ids from list_sessions, two to eight, each a host that is ready."},` +
			`"command":{"type":"string","minLength":1,"maxLength":8192,"description":"The shell command line to run on every listed session."},` +
			`"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":60,"description":"Give up waiting after this many seconds."}` +
			`},"required":["sessions","command"],"additionalProperties":false}`),
		Kind: Exec,
	},
	{
		Name:        NameSFTPList,
		Description: "List the entries of a remote directory over SFTP, with type, size and modification time.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"session":{"type":"string","maxLength":256,"description":"Session id from list_sessions. Defaults to the active session."},` +
			`"path":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute remote directory path."}` +
			`},"required":["path"],"additionalProperties":false}`),
		Kind: Read,
	},
	{
		Name:        NameSFTPRead,
		Description: "Read the start of a remote file over SFTP as text. The result says whether the file was cut off at max_bytes.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"session":{"type":"string","maxLength":256,"description":"Session id from list_sessions. Defaults to the active session."},` +
			`"path":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute remote file path."},` +
			`"max_bytes":{"type":"integer","minimum":1,"maximum":262144,"default":262144,"description":"Maximum number of bytes to read."}` +
			`},"required":["path"],"additionalProperties":false}`),
		Kind: Read,
	},
	{
		Name:        NameSFTPWrite,
		Description: "Write a text file on the remote host over SFTP. Fails if the file exists unless overwrite is true. The user must approve it.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"session":{"type":"string","maxLength":256,"description":"Session id from list_sessions. Defaults to the active session."},` +
			`"path":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute remote file path."},` +
			`"content":{"type":"string","maxLength":262144,"description":"Complete new file content (UTF-8 text, at most 262144 bytes)."},` +
			`"overwrite":{"type":"boolean","default":false,"description":"Replace the file if it already exists."}` +
			`},"required":["path","content"],"additionalProperties":false}`),
		Kind: Write,
	},
	{
		Name:        NameSFTPRemove,
		Description: "Delete one remote file or empty directory over SFTP. It cannot be undone. The user must approve it.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"session":{"type":"string","maxLength":256,"description":"Session id from list_sessions. Defaults to the active session."},` +
			`"path":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute remote path to delete."}` +
			`},"required":["path"],"additionalProperties":false}`),
		Kind: Write,
	},
}

// All returns every tool in a stable order. The slice is a copy.
func All() []Spec { return append([]Spec(nil), specs...) }

// Targeted describes the in-app request's locked default rather than the
// currently visible tab. Its schemas are copied so external MCP catalogs keep
// their active-session contract.
func Targeted() []Spec {
	const activeHint = "Session id from list_sessions. Defaults to the active session."
	const targetHint = "Session id from list_sessions. If omitted, uses this request's locked primary session. Always specify the id when operating on an explicitly @referenced session."
	out := make([]Spec, len(specs))
	for index, spec := range specs {
		if schema := string(spec.Schema); strings.Contains(schema, activeHint) {
			spec.Schema = json.RawMessage(strings.ReplaceAll(schema, activeHint, targetHint))
		}
		switch spec.Name {
		case NameListSessions:
			spec.Description = "List this request's locked primary connection and explicitly @referenced sessions with ids, titles, addresses and readiness. Active marks the locked primary, not the currently visible tab. Call this first when you need a session id."
		case NameTerminalExec:
			spec.Description = "Run one non-interactive shell command in the addressed terminal session and return its output and exit status. If the session id is omitted, use the locked primary. Explicitly supply the id for an @referenced session; cross-connection execution requires approval naming that actual target. The command remains visible in that session's tab and must finish within the timeout."
		}
		out[index] = spec
	}
	return out
}

// Headless returns the catalog of a server that has no terminal windows to act
// in: it works on saved hosts over their own connections. The tools and their
// arguments are the same, so the same decoders and the same gate serve them;
// what changes is what the descriptions promise and which tools are offered.
// There is no screen to read, so terminal_read is absent. A command runs through
// an SSH exec channel, with no terminal and no shell state kept between calls,
// and the user does not watch it run; the command tools are offered only when
// commands is true. File writes are offered only when writeFiles is true, for
// the separately confirmed full-access mode. The slice is a copy.
func Headless(commands, writeFiles bool) []Spec {
	const (
		sessionHint = "Session id from list_sessions. Defaults to the active session."
		hostHint    = "Host id from list_sessions. May be left out when only one host is available."
		manyHint    = "Session ids from list_sessions, two to eight, each a host that is ready."
	)
	out := make([]Spec, 0, len(specs))
	for _, spec := range specs {
		switch {
		case spec.Name == NameTerminalRead, spec.Kind == Write && !writeFiles, spec.Kind == Exec && !commands:
			continue
		}
		schema := strings.ReplaceAll(string(spec.Schema), sessionHint, hostHint)
		spec.Schema = json.RawMessage(strings.ReplaceAll(schema, manyHint, "Host ids from list_sessions, two to eight."))
		switch spec.Name {
		case NameListSessions:
			spec.Description = "List the saved hosts the user allowed you to use, with their ids. A host connects when you first use it. Call this first when you need a host id."
		case NameTerminalExec:
			spec.Description = "Run one shell command on a saved host over a fresh SSH exec channel and return its combined output and exit status. There is no terminal: nothing interactive works, and the working directory and environment do not carry over to the next call. The user does not watch it run. It must finish within the timeout."
		case NameTerminalExecMany:
			spec.Description = "Run the same shell command on several saved hosts at once and return each host's combined output and exit status, labelled by host. There is no terminal and no state carries over. Use it to check or change the same thing across hosts; use terminal_exec for one host. It must finish within the timeout on every host."
		case NameSFTPWrite:
			spec.Description = "Write UTF-8 text to a file on an authorized saved host over SFTP. Full access was enabled, so this does not request approval. Set overwrite only when replacing existing content is intended."
		case NameSFTPRemove:
			spec.Description = "Delete one remote file or empty directory on an authorized saved host over SFTP. Full access was enabled, so this does not request approval. Deletion cannot be undone."
		}
		out = append(out, spec)
	}
	return out
}

// Lookup finds a tool by name.
func Lookup(name string) (Spec, bool) {
	for _, s := range specs {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Argument structs. Session "" selects the caller's default: the active session
// for external MCP, or the locked primary of an in-app request.

type ListSessionsArgs struct{}

type TerminalReadArgs struct {
	Session string `json:"session,omitempty"`
	Lines   int    `json:"lines,omitempty"`
	Scope   string `json:"scope,omitempty"`
}

type TerminalExecArgs struct {
	Session        string `json:"session,omitempty"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type TerminalExecManyArgs struct {
	Sessions       []string `json:"sessions"`
	Command        string   `json:"command"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type SFTPListArgs struct {
	Session string `json:"session,omitempty"`
	Path    string `json:"path"`
}

type SFTPReadArgs struct {
	Session  string `json:"session,omitempty"`
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type SFTPWriteArgs struct {
	Session   string `json:"session,omitempty"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Overwrite bool   `json:"overwrite,omitempty"`
}

type SFTPRemoveArgs struct {
	Session string `json:"session,omitempty"`
	Path    string `json:"path"`
}

// Decoders. Each returns the arguments with defaults applied or a descriptive
// error suitable for sending back to the model.

func DecodeListSessions(raw []byte) (ListSessionsArgs, error) {
	var w struct{}
	return ListSessionsArgs{}, decode(raw, &w)
}

func DecodeTerminalRead(raw []byte) (TerminalReadArgs, error) {
	var w struct {
		Session *string `json:"session"`
		Lines   *int    `json:"lines"`
		Scope   *string `json:"scope"`
	}
	if err := decode(raw, &w); err != nil {
		return TerminalReadArgs{}, err
	}
	a := TerminalReadArgs{Lines: DefaultReadLines, Scope: ScopeScreen}
	var err error
	if a.Session, err = session(w.Session); err != nil {
		return a, err
	}
	if w.Lines != nil {
		if *w.Lines < 1 || *w.Lines > MaxReadLines {
			return a, fmt.Errorf("lines must be between 1 and %d", MaxReadLines)
		}
		a.Lines = *w.Lines
	}
	if w.Scope != nil {
		if *w.Scope != ScopeScreen && *w.Scope != ScopeScrollback {
			return a, fmt.Errorf("scope must be %q or %q", ScopeScreen, ScopeScrollback)
		}
		a.Scope = *w.Scope
	}
	return a, nil
}

func DecodeTerminalExec(raw []byte) (TerminalExecArgs, error) {
	var w struct {
		Session        *string `json:"session"`
		Command        *string `json:"command"`
		TimeoutSeconds *int    `json:"timeout_seconds"`
	}
	if err := decode(raw, &w); err != nil {
		return TerminalExecArgs{}, err
	}
	a := TerminalExecArgs{TimeoutSeconds: DefaultTimeoutSeconds}
	var err error
	if a.Session, err = session(w.Session); err != nil {
		return a, err
	}
	a.Command, a.TimeoutSeconds, err = commandAndTimeout(w.Command, w.TimeoutSeconds)
	return a, err
}

func DecodeTerminalExecMany(raw []byte) (TerminalExecManyArgs, error) {
	var w struct {
		Sessions       []string `json:"sessions"`
		Command        *string  `json:"command"`
		TimeoutSeconds *int     `json:"timeout_seconds"`
	}
	if err := decode(raw, &w); err != nil {
		return TerminalExecManyArgs{}, err
	}
	a := TerminalExecManyArgs{TimeoutSeconds: DefaultTimeoutSeconds}
	if len(w.Sessions) < 2 || len(w.Sessions) > MaxBatchSessions {
		return a, fmt.Errorf("sessions must name between 2 and %d sessions (use terminal_exec for one)", MaxBatchSessions)
	}
	seen := map[string]bool{}
	for _, id := range w.Sessions {
		if id == "" {
			return a, errors.New("a session id is empty; take them from list_sessions")
		}
		if seen[id] {
			return a, fmt.Errorf("session %q is listed twice", id)
		}
		seen[id] = true
		if _, err := session(&id); err != nil {
			return a, err
		}
	}
	command, timeout, err := commandAndTimeout(w.Command, w.TimeoutSeconds)
	if err != nil {
		return a, err
	}
	a.Sessions, a.Command, a.TimeoutSeconds = w.Sessions, command, timeout
	return a, nil
}

// commandAndTimeout validates what terminal_exec and terminal_exec_many share.
func commandAndTimeout(command *string, timeoutSeconds *int) (string, int, error) {
	timeout := DefaultTimeoutSeconds
	if command == nil || strings.TrimSpace(*command) == "" {
		return "", timeout, errors.New("command is required")
	}
	if len(*command) > MaxCommandBytes {
		return "", timeout, fmt.Errorf("command is longer than %d bytes", MaxCommandBytes)
	}
	if err := text("command", *command, true); err != nil {
		return "", timeout, err
	}
	if timeoutSeconds != nil {
		if *timeoutSeconds < 1 || *timeoutSeconds > MaxTimeoutSeconds {
			return "", timeout, fmt.Errorf("timeout_seconds must be between 1 and %d", MaxTimeoutSeconds)
		}
		timeout = *timeoutSeconds
	}
	return *command, timeout, nil
}

func DecodeSFTPList(raw []byte) (SFTPListArgs, error) {
	var w struct {
		Session *string `json:"session"`
		Path    *string `json:"path"`
	}
	if err := decode(raw, &w); err != nil {
		return SFTPListArgs{}, err
	}
	var a SFTPListArgs
	var err error
	if a.Session, err = session(w.Session); err != nil {
		return a, err
	}
	a.Path, err = path(w.Path)
	return a, err
}

func DecodeSFTPRead(raw []byte) (SFTPReadArgs, error) {
	var w struct {
		Session  *string `json:"session"`
		Path     *string `json:"path"`
		MaxBytes *int    `json:"max_bytes"`
	}
	if err := decode(raw, &w); err != nil {
		return SFTPReadArgs{}, err
	}
	a := SFTPReadArgs{MaxBytes: MaxReadBytes}
	var err error
	if a.Session, err = session(w.Session); err != nil {
		return a, err
	}
	if a.Path, err = path(w.Path); err != nil {
		return a, err
	}
	if w.MaxBytes != nil {
		if *w.MaxBytes < 1 || *w.MaxBytes > MaxReadBytes {
			return a, fmt.Errorf("max_bytes must be between 1 and %d", MaxReadBytes)
		}
		a.MaxBytes = *w.MaxBytes
	}
	return a, nil
}

func DecodeSFTPWrite(raw []byte) (SFTPWriteArgs, error) {
	var w struct {
		Session   *string `json:"session"`
		Path      *string `json:"path"`
		Content   *string `json:"content"`
		Overwrite *bool   `json:"overwrite"`
	}
	if err := decode(raw, &w); err != nil {
		return SFTPWriteArgs{}, err
	}
	var a SFTPWriteArgs
	var err error
	if a.Session, err = session(w.Session); err != nil {
		return a, err
	}
	if a.Path, err = path(w.Path); err != nil {
		return a, err
	}
	if w.Content == nil {
		return a, errors.New("content is required (use an empty string for an empty file)")
	}
	if len(*w.Content) > MaxContentBytes {
		return a, fmt.Errorf("content is longer than %d bytes", MaxContentBytes)
	}
	a.Content = *w.Content
	a.Overwrite = w.Overwrite != nil && *w.Overwrite
	return a, nil
}

func DecodeSFTPRemove(raw []byte) (SFTPRemoveArgs, error) {
	var w struct {
		Session *string `json:"session"`
		Path    *string `json:"path"`
	}
	if err := decode(raw, &w); err != nil {
		return SFTPRemoveArgs{}, err
	}
	var a SFTPRemoveArgs
	var err error
	if a.Session, err = session(w.Session); err != nil {
		return a, err
	}
	a.Path, err = path(w.Path)
	return a, err
}

// decode strictly decodes one JSON object. Empty input and null mean {}.
func decode(raw []byte, v any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if len(raw) > 2*MaxContentBytes+64<<10 {
		return errors.New("arguments are too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("invalid arguments: trailing data after the JSON object")
	}
	return nil
}

func session(p *string) (string, error) {
	if p == nil {
		return "", nil
	}
	if len(*p) > MaxSessionLen {
		return "", fmt.Errorf("session is longer than %d bytes", MaxSessionLen)
	}
	return *p, text("session", *p, false)
}

func path(p *string) (string, error) {
	if p == nil || *p == "" {
		return "", errors.New("path is required")
	}
	if len(*p) > MaxPathLen {
		return "", fmt.Errorf("path is longer than %d bytes", MaxPathLen)
	}
	return *p, text("path", *p, false)
}

// text rejects invalid UTF-8 and NUL; unless multiline it also rejects every
// control character.
func text(field, s string, multiline bool) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s must be valid UTF-8", field)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0 || (!multiline && (c < 0x20 || c == 0x7f)) {
			return fmt.Errorf("%s contains a control character", field)
		}
	}
	return nil
}
