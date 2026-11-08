package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// JobState is where a terminal job stands.
type JobState string

const (
	JobRunning      JobState = "running"
	JobCompleted    JobState = "completed"
	JobCancelled    JobState = "cancelled"    // ended by terminal_stop
	JobInterrupted  JobState = "interrupted"  // the user took the terminal back
	JobDisconnected JobState = "disconnected" // the shell ended before the command did
)

// Terminal readiness: whether the next command typed would reach a shell prompt.
const (
	TerminalReady       = "ready"
	TerminalBusy        = "busy"
	TerminalUnconfirmed = "unconfirmed"
	TerminalClosed      = "closed"
)

// JobStatus is what a Backend reports about one terminal job and a page of its
// output. Offsets are UTF-8 byte positions in the normalized output text.
type JobStatus struct {
	ID       string
	Session  string
	State    JobState
	ExitCode int // meaningful only when State is JobCompleted
	Terminal string
	Data     string
	// Start is where Data begins, later than the requested offset when older
	// output was dropped. Next is what to ask for next; Total is the end of the
	// output committed so far.
	Start, Next, Total int
	// Pending is the unfinished last line, not yet counted in Next or Total.
	Pending string
	// Interrupted is set by JobStop when this call delivered Ctrl-C.
	Interrupted bool
}

type jobView struct {
	JobID        string `json:"job_id"`
	Session      string `json:"session,omitempty"`
	State        string `json:"state"`
	ExitCode     *int   `json:"exit_code,omitempty"`
	TerminalOK   string `json:"terminal_ready"`
	Data         string `json:"data"`
	Pending      string `json:"pending_line,omitempty"`
	NextOffset   int    `json:"next_offset"`
	TotalBytes   int    `json:"total_bytes"`
	SkippedBytes int    `json:"skipped_bytes"`
	Interrupted  *bool  `json:"interrupt_sent,omitempty"`
}

func (g Gate) runJob(ctx context.Context, name string, raw []byte) Result {
	if outputOwner(ctx) == "" {
		return Result{Text: name + " needs an MCP client session that can read the job later; it is not available here", IsError: true}
	}
	switch name {
	case tools.NameTerminalStart:
		args, err := tools.DecodeTerminalStart(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session == "" {
			if args.Session, err = g.readySession(ctx); err != nil {
				return failure(err)
			}
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		if denied := g.ask(ctx, name, tools.Exec, args.Session, args.Command, risk.Classify(args.Command)); denied != nil {
			return *denied
		}
		status, err := g.Backend.JobStart(ctx, args)
		if err != nil {
			return failure(err)
		}
		return jobResult(status, status.Start, false)
	case tools.NameTerminalPoll:
		args, err := tools.DecodeTerminalPoll(raw)
		if err != nil {
			return failure(err)
		}
		status, err := g.Backend.JobPoll(ctx, args)
		if err != nil {
			return failure(err)
		}
		return jobResult(status, args.Offset, false)
	default:
		args, err := tools.DecodeTerminalStop(raw)
		if err != nil {
			return failure(err)
		}
		status, err := g.Backend.JobStop(ctx, args)
		if err != nil {
			return failure(err)
		}
		return jobResult(status, status.Start, true)
	}
}

func jobResult(status JobStatus, requested int, stop bool) Result {
	view := jobView{
		JobID: status.ID, Session: status.Session, State: string(status.State), TerminalOK: status.Terminal,
		Data: status.Data, Pending: status.Pending, NextOffset: status.Next, TotalBytes: status.Total,
		SkippedBytes: max(0, status.Start-requested),
	}
	if status.State == JobCompleted && status.ExitCode >= 0 {
		code := status.ExitCode
		view.ExitCode = &code
	}
	if stop {
		sent := status.Interrupted
		view.Interrupted = &sent
	}
	structured, _ := json.Marshal(view)

	var text strings.Builder
	text.WriteString(status.Data)
	if status.Data != "" && !strings.HasSuffix(status.Data, "\n") {
		text.WriteByte('\n')
	}
	if status.Pending != "" {
		fmt.Fprintf(&text, "[unfinished line: %s]\n", status.Pending)
	}
	fmt.Fprintf(&text, "[job %s: %s", status.ID, status.State)
	if view.ExitCode != nil {
		fmt.Fprintf(&text, ", exit status %d", *view.ExitCode)
	}
	if view.SkippedBytes > 0 {
		fmt.Fprintf(&text, ", %d earlier bytes were dropped", view.SkippedBytes)
	}
	fmt.Fprintf(&text, "; next_offset %d of %d; terminal %s]", status.Next, status.Total, status.Terminal)
	if stop && !status.Interrupted && status.State != JobRunning {
		text.WriteString("\n[the job had already finished; nothing was sent]")
	}
	return Result{Text: text.String(), Structured: structured}
}

// ErrNoJob is what a Backend returns for a job id it does not know, has expired,
// or belongs to another client; the three are not told apart on purpose.
var ErrNoJob = errors.New("job not found or expired")
