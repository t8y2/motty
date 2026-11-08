package workbench

import (
	"crypto/rand"
	"encoding/base64"
	"time"

	"motty/internal/ai/capture"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
)

const (
	// jobRetainWindow is how much of a job's newest output is kept for polling.
	jobRetainWindow = 2 << 20
	jobTTL          = 10 * time.Minute
	maxJobs         = 64
	// stopPromptWait is how long a stop waits to see the shell prompt return.
	stopPromptWait = 3 * time.Second
)

// aiJob is a command the assistant typed into a terminal and did not wait for.
// It belongs to one MCP client session and lives on the window thread, as does
// everything that touches it.
type aiJob struct {
	id, owner string
	session   string
	t         *terminal
	exec      *aiExec
	tap       *capture.Tap
	state     gate.JobState
	exitCode  int
	finished  time.Time
}

// finish records how the command ended. It runs from finishExec.
func (j *aiJob) finish(end execEnd, exitCode int) {
	switch end {
	case endMarker:
		j.state, j.exitCode = gate.JobCompleted, exitCode
	case endStop:
		j.state = gate.JobCancelled
	case endShell:
		j.state = gate.JobDisconnected
	default:
		j.state = gate.JobInterrupted
	}
	j.finished = time.Now()
}

// terminalState says whether the next command typed would reach a prompt.
func (j *aiJob) terminalState() string {
	t := j.t
	switch {
	case t.shell == nil || t.ended:
		return gate.TerminalClosed
	case t.exec != nil:
		return gate.TerminalBusy
	case j.state == gate.JobCompleted:
		return gate.TerminalReady
	case t.commandReporting:
		if t.promptReady {
			return gate.TerminalReady
		}
		return gate.TerminalBusy
	}
	return gate.TerminalUnconfirmed
}

func (j *aiJob) status(offset, maxBytes int) gate.JobStatus {
	page := j.tap.Page(offset, maxBytes)
	return gate.JobStatus{
		ID: j.id, Session: j.session, State: j.state, ExitCode: j.exitCode, Terminal: j.terminalState(),
		Data: page.Data, Start: page.Start, Next: page.Next, Total: page.Total, Pending: page.Pending,
	}
}

func (t *terminal) releasePromptWait() {
	if t.promptWait != nil {
		close(t.promptWait)
		t.promptWait = nil
	}
}

// startJob types the command into the session's terminal. Window thread.
func (state *State) startJob(owner string, args tools.TerminalExecArgs) (gate.JobStatus, error) {
	args.Background = true
	exec, err := state.beginExec(args)
	if err != nil {
		return gate.JobStatus{}, err
	}
	var random [18]byte
	if _, err := rand.Read(random[:]); err != nil {
		exec.t.finishExec(endStop)
		return gate.JobStatus{}, err
	}
	job := &aiJob{
		id: base64.RawURLEncoding.EncodeToString(random[:]), owner: owner, session: args.Session,
		t: exec.t, exec: exec, tap: exec.tap, state: gate.JobRunning,
	}
	exec.job = job
	state.pruneJobs()
	if state.ai.jobs == nil {
		state.ai.jobs = map[string]*aiJob{}
	}
	state.ai.jobs[job.id] = job
	return job.status(0, 4), nil
}

// pruneJobs drops finished jobs that expired and, when the table is full, the
// oldest finished ones. A running job is never dropped.
func (state *State) pruneJobs() {
	now := time.Now()
	for id, job := range state.ai.jobs {
		if job.state != gate.JobRunning && now.Sub(job.finished) >= jobTTL {
			delete(state.ai.jobs, id)
		}
	}
	for len(state.ai.jobs) >= maxJobs {
		var oldest *aiJob
		for _, job := range state.ai.jobs {
			if job.state != gate.JobRunning && (oldest == nil || job.finished.Before(oldest.finished)) {
				oldest = job
			}
		}
		if oldest == nil {
			return
		}
		delete(state.ai.jobs, oldest.id)
	}
}

// job finds a live or recently finished job of this owner.
func (state *State) job(owner, id string) (*aiJob, error) {
	job := state.ai.jobs[id]
	if job == nil || owner == "" || job.owner != owner ||
		job.state != gate.JobRunning && time.Since(job.finished) >= jobTTL {
		return nil, gate.ErrNoJob
	}
	return job, nil
}

// stopJob interrupts a running job and, when the terminal reports prompts,
// returns a channel that closes when the prompt is back. Window thread.
func (state *State) stopJob(owner, id string) (gate.JobStatus, <-chan struct{}, error) {
	job, err := state.job(owner, id)
	if err != nil {
		return gate.JobStatus{}, nil, err
	}
	t := job.t
	if job.state != gate.JobRunning || t.exec != job.exec {
		return job.stopped(false), nil, nil
	}
	var wait chan struct{}
	if t.commandReporting {
		wait = make(chan struct{})
		t.releasePromptWait()
		t.promptWait = wait
	}
	t.finishExec(endStop)
	return job.stopped(true), wait, nil
}

// stopped is the status a stop reports: no output page, positioned at the end.
func (j *aiJob) stopped(sent bool) gate.JobStatus {
	status := j.status(1<<62, 4)
	status.Interrupted = sent
	return status
}
