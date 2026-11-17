package workbench

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"motty/internal/ai/capture"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/sshconn"
)

const (
	// agentJobRetainWindow is how much of a headless job's newest output is kept
	// for polling. Between it and twice as much is held before the oldest is
	// dropped, so offsets stay absolute.
	agentJobRetainWindow = 2 << 20
	agentJobTTL          = 10 * time.Minute
	agentMaxJobs         = 64
	// agentMaxRunningJobs bounds the SSH command sessions and output buffers
	// held at once. An SSH server commonly allows ten sessions per connection,
	// and each running job holds one channel open on its host's connection.
	agentMaxRunningJobs = 8
)

var (
	// errAgentNoJobOwner is what a job call gets when it did not arrive on an
	// MCP client session, which a job needs to be read again later.
	errAgentNoJobOwner = errors.New("a background job needs an MCP client session that can read it later")
	// errAgentTooManyJobs is what starting a job gets when every job slot is
	// held by a command that has not finished.
	errAgentTooManyJobs = errors.New("too many background jobs are running; wait for one to finish")
)

// agentJob is a command an agent started on a saved host without waiting for
// it, and the bounded window of what it printed. It runs on its own SSH exec
// channel rather than in a terminal, so several jobs can run against one host.
// It belongs to one MCP client session and lives until it has finished and
// expired.
type agentJob struct {
	id, owner string
	entry     *agentEntry
	connect   *sshconn.Conn
	backend   *agentBackend
	cancel    context.CancelFunc

	mu       sync.Mutex // guards the output tap and how the job ended
	tap      *capture.Tap
	state    gate.JobState
	exitCode int
	finished time.Time
}

// startJob begins a command on the host the arguments name and returns its
// first status. The connection is held until the command ends, however long
// that is.
func (b *agentBackend) startJob(ctx context.Context, args tools.TerminalStartArgs) (gate.JobStatus, error) {
	owner := gate.OutputOwner(ctx)
	if owner == "" {
		return gate.JobStatus{}, errAgentNoJobOwner
	}
	entry, err := b.acquire(ctx, args.Session)
	if err != nil {
		return gate.JobStatus{}, err
	}
	connect := entry.connection()
	if connect == nil {
		entry.release()
		return gate.JobStatus{}, errors.New("the connection to the host closed")
	}
	var random [18]byte
	if _, err := rand.Read(random[:]); err != nil {
		entry.release()
		return gate.JobStatus{}, err
	}
	jobCtx, cancel := context.WithCancel(context.Background())
	tap := capture.NewStreamTap(agentJobRetainWindow)
	tap.RetainWindow(agentJobRetainWindow)
	job := &agentJob{
		id: base64.RawURLEncoding.EncodeToString(random[:]), owner: owner,
		entry: entry, connect: connect, backend: b, cancel: cancel,
		tap: tap, state: gate.JobRunning, exitCode: -1,
	}
	if err := b.addJob(job); err != nil {
		cancel()
		entry.release()
		return gate.JobStatus{}, err
	}
	go job.run(jobCtx, args.Command)
	return job.status(0, 4), nil
}

func (b *agentBackend) JobStart(ctx context.Context, args tools.TerminalStartArgs) (gate.JobStatus, error) {
	return b.startJob(ctx, args)
}

func (b *agentBackend) JobPoll(ctx context.Context, args tools.TerminalPollArgs) (gate.JobStatus, error) {
	job, err := b.job(gate.OutputOwner(ctx), args.JobID)
	if err != nil {
		return gate.JobStatus{}, err
	}
	return job.status(args.Offset, args.MaxBytes), nil
}

func (b *agentBackend) JobStop(ctx context.Context, args tools.TerminalStopArgs) (gate.JobStatus, error) {
	job, err := b.job(gate.OutputOwner(ctx), args.JobID)
	if err != nil {
		return gate.JobStatus{}, err
	}
	sent := job.stop()
	status := job.status(1<<62, 4)
	status.Interrupted = sent
	return status, nil
}

// run executes the command and records how it ended. The job's connection is
// released only when the command is over.
func (j *agentJob) run(ctx context.Context, command string) {
	defer j.backend.jobGroup.Done()
	defer j.entry.release()
	result, err := j.connect.ExecStream(ctx, command, jobOutput{j})
	j.finish(err, result.ExitCode)
}

// jobOutput appends what the command prints to the job's output window. The
// SSH session may write stdout and stderr from separate goroutines.
type jobOutput struct{ job *agentJob }

func (w jobOutput) Write(p []byte) (int, error) {
	w.job.mu.Lock()
	w.job.tap.Feed(p)
	w.job.mu.Unlock()
	return len(p), nil
}

// finish records how the command ended and closes the output window. A stop
// that already marked the job cancelled keeps that state. Why a command ended
// early is written to its output, since the status carries no message.
func (j *agentJob) finish(err error, exitCode int) {
	j.mu.Lock()
	if j.state == gate.JobRunning {
		switch {
		case err == nil:
			j.state, j.exitCode = gate.JobCompleted, exitCode
		case errors.Is(err, sshconn.ErrCanceled):
			j.state, j.exitCode = gate.JobDisconnected, -1
			j.note("the host connection closed before the command ended")
		default:
			j.state, j.exitCode = gate.JobInterrupted, -1
			j.note("the command did not finish: " + strings.ReplaceAll(err.Error(), "\n", " "))
		}
	}
	j.tap.FinishConsole("", j.exitCode)
	j.finished = time.Now()
	j.mu.Unlock()
	j.backend.scheduleExpiry(j)
}

// note adds one line to the job's output. The caller holds mu.
func (j *agentJob) note(text string) {
	j.tap.Feed([]byte("\n[" + text + "]\n"))
}

// stop marks a running job cancelled and interrupts its command. It reports
// whether the command was still running.
func (j *agentJob) stop() bool {
	j.mu.Lock()
	if j.state != gate.JobRunning {
		j.mu.Unlock()
		return false
	}
	j.state, j.exitCode, j.finished = gate.JobCancelled, -1, time.Now()
	j.mu.Unlock()
	j.cancel()
	return true
}

// status is the job's state and a page of its output. Offsets are UTF-8 byte
// positions in the recorded text, as for a job in a visible terminal.
func (j *agentJob) status(offset, maxBytes int) gate.JobStatus {
	j.mu.Lock()
	page := j.tap.Page(offset, maxBytes)
	state, code := j.state, j.exitCode
	j.mu.Unlock()
	return gate.JobStatus{
		ID: j.id, Session: j.entry.host.CatalogID, State: state, ExitCode: code,
		Terminal: j.entry.connectionState(),
		Data:     page.Data, Start: page.Start, Next: page.Next, Total: page.Total, Pending: page.Pending,
	}
}

// endedAt reports when the job stopped running, and false while it still runs.
func (j *agentJob) endedAt() (time.Time, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.finished, j.state != gate.JobRunning
}

// addJob registers a job, first dropping expired ones and then the oldest
// finished ones when the table is full. A running job is never dropped.
func (b *agentBackend) addJob(job *agentJob) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errAgentClosed
	}
	now := time.Now()
	running := 0
	for id, other := range b.jobs {
		if ended, ok := other.endedAt(); ok {
			if now.Sub(ended) >= agentJobTTL {
				delete(b.jobs, id)
			}
		} else {
			running++
		}
	}
	if running >= agentMaxRunningJobs {
		return errAgentTooManyJobs
	}
	for len(b.jobs) >= agentMaxJobs {
		var oldestID string
		var oldest time.Time
		for id, other := range b.jobs {
			if ended, ok := other.endedAt(); ok && (oldestID == "" || ended.Before(oldest)) {
				oldestID, oldest = id, ended
			}
		}
		if oldestID == "" {
			return errAgentTooManyJobs
		}
		delete(b.jobs, oldestID)
	}
	if b.jobs == nil {
		b.jobs = map[string]*agentJob{}
	}
	b.jobGroup.Add(1)
	b.jobs[job.id] = job
	return nil
}

// job finds a live or recently finished job of this client.
func (b *agentBackend) job(owner, id string) (*agentJob, error) {
	b.mu.Lock()
	job := b.jobs[id]
	b.mu.Unlock()
	if job == nil || owner == "" || job.owner != owner {
		return nil, gate.ErrNoJob
	}
	if ended, ok := job.endedAt(); ok && time.Since(ended) >= agentJobTTL {
		return nil, gate.ErrNoJob
	}
	return job, nil
}

// findJob returns a job by id whatever its owner, for the audit log.
func (b *agentBackend) findJob(id string) *agentJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.jobs[id]
}

// scheduleExpiry drops a finished job's output once its retention time is up,
// so an idle server does not keep output nobody can read any more.
func (b *agentBackend) scheduleExpiry(job *agentJob) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return
	}
	time.AfterFunc(agentJobTTL, func() { b.expireJob(job.id) })
}

func (b *agentBackend) expireJob(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	job := b.jobs[id]
	if job == nil {
		return
	}
	if ended, ok := job.endedAt(); ok && time.Since(ended) >= agentJobTTL {
		delete(b.jobs, id)
	}
}

// connection is the host's connection as it stands now. The caller must hold a
// use of the entry, as the job does, so it is not dropped underneath.
func (entry *agentEntry) connection() *sshconn.Conn {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.conn
}

// connectionState is what a headless job reports as terminal_ready: it runs
// without a terminal, so whether the host is still connected stands in for it.
func (entry *agentEntry) connectionState() string {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.conn != nil && entry.conn.Status().Phase == sshconn.PhaseConnected {
		return gate.TerminalReady
	}
	return gate.TerminalClosed
}
