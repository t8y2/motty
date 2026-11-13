package workbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/shellkind"
	"motty/internal/zmodem"
)

// rz/sz over the terminal the user is looking at. SFTP follows the SSH session,
// so after "ssh" from a jump host's shell it still talks to the jump. These
// commands are typed into that shell, and ZMODEM rides the same bytes.

// rzszStartWait is how long the remote may take to answer rz or sz before the
// command is interrupted. The call's own timeout still bounds the transfer.
const rzszStartWait = 20 * time.Second

// rzszProgressEvery is how often a running transfer tells the MCP client how
// many bytes have moved. The remote screen stays on "rz waiting to receive"
// until the end, and a client that hears nothing times the call out, so the
// two cases look the same unless the byte count is reported on its own.
const rzszProgressEvery = 400 * time.Millisecond

// rzszJob is one armed transfer. It is completed exactly once, on the window
// thread, including when the shell disappears or the caller gives up.
type rzszJob struct {
	sessionID uint32
	upload    bool
	engine    zmodem.Engine
	sources   []*os.File
	folder    *folderTarget
	localPath string
	remote    string
	bytes     int64
	armed     time.Duration
	reason    error
	done      chan rzszResult

	// snap is the newest progress. The window thread stores it and the call
	// that is waiting on done reads it.
	mu   sync.Mutex
	snap gate.RZSZSnapshot
	have bool
}

type rzszResult struct {
	status gate.RZSZStatus
	err    error
}

var _ gate.RZSZMover = (*aiBackend)(nil)
var _ gate.RZSZMover = hubBackend{}
var _ gate.RZSZObserver = (*aiBackend)(nil)
var _ gate.RZSZObserver = hubBackend{}

func (b *aiBackend) TransferRZSZ(ctx context.Context, req tools.RZSZRequest) (gate.RZSZStatus, error) {
	job, err := aiCall(b.state, ctx, func() (*rzszJob, error) {
		session, err := b.session(req.Session)
		if err != nil {
			return nil, err
		}
		return b.state.beginRZSZ(session, req)
	})
	if err != nil {
		return gate.RZSZStatus{}, err
	}
	timer := time.NewTimer(execTimeout(req.TimeoutSeconds))
	defer timer.Stop()
	ticker := time.NewTicker(rzszProgressEvery)
	defer ticker.Stop()
	var sent float64
	reported := false
	publish := func() { publishRZSZProgress(ctx, job, &sent, &reported) }
	publish()
	for {
		select {
		case res := <-job.done:
			publish()
			return res.status, res.err
		case <-timer.C:
			return b.state.abortRZSZWait(job, errors.New("rz/sz timed out"))
		case <-ctx.Done():
			// The caller going away is not the transfer failing. The tool says a
			// client timeout is answered by rzsz_status, so leave the job to its
			// own timer and the progress it publishes.
			return gate.RZSZStatus{}, ctx.Err()
		case <-ticker.C:
			publish()
		}
	}
}

func (b *aiBackend) RZSZStatus(ctx context.Context, sessionID string) (gate.RZSZSnapshot, bool, error) {
	type found struct {
		snap   gate.RZSZSnapshot
		active bool
	}
	got, err := aiCall(b.state, ctx, func() (found, error) {
		session, err := b.session(sessionID)
		if err != nil {
			return found{}, err
		}
		if session.rzsz == nil {
			return found{}, nil
		}
		snap, ok := session.rzsz.load()
		return found{snap, ok}, nil
	})
	return got.snap, got.active, err
}

func (b hubBackend) RZSZStatus(ctx context.Context, session string) (gate.RZSZSnapshot, bool, error) {
	backend, local, err := b.target(ctx, session)
	if err != nil {
		return gate.RZSZSnapshot{}, false, err
	}
	return backend.RZSZStatus(ctx, local)
}

func (job *rzszJob) store(snap gate.RZSZSnapshot) {
	job.mu.Lock()
	job.snap, job.have = snap, true
	job.mu.Unlock()
}

func (job *rzszJob) load() (gate.RZSZSnapshot, bool) {
	job.mu.Lock()
	defer job.mu.Unlock()
	return job.snap, job.have
}

// publishRZSZProgress tells the MCP client the newest byte count. A repeat of
// the same count is skipped: a progress notification has to move forward.
func publishRZSZProgress(ctx context.Context, job *rzszJob, sent *float64, reported *bool) {
	snap, ok := job.load()
	if !ok {
		return
	}
	progress := float64(snap.BytesDone)
	if *reported && progress <= *sent {
		return
	}
	*reported, *sent = true, progress
	report := gate.ProgressReport{Progress: progress, Message: gate.FormatRZSZSnapshot(snap)}
	if snap.BytesTotal >= 0 {
		report.HasTotal, report.Total = true, float64(snap.BytesTotal)
	}
	gate.ReportProgress(ctx, report)
}

// noteRZSZ copies the engine's progress onto the armed call. Window thread.
func (state *State) noteRZSZ(session *Session) {
	job := session.rzsz
	if job == nil {
		return
	}
	job.store(rzszSnapshot(session, job))
}

func rzszSnapshot(session *Session, job *rzszJob) gate.RZSZSnapshot {
	snap := gate.RZSZSnapshot{
		Direction:  "download",
		Phase:      gate.RZSZPhaseWaiting,
		Name:       job.remote,
		BytesTotal: -1,
	}
	if job.upload {
		snap.Direction = "upload"
		snap.BytesTotal = job.bytes
	}
	if session != nil && session.transfer.phase != transferNone {
		progress := session.transfer.progress
		snap.Phase = gate.RZSZPhaseTransferring
		snap.BytesDone = progress.Done
		if progress.Name != "" {
			snap.Name = progress.Name
		}
		if progress.Size >= 0 {
			snap.BytesTotal = progress.Size
		}
	}
	return snap
}

func (b hubBackend) TransferRZSZ(ctx context.Context, req tools.RZSZRequest) (gate.RZSZStatus, error) {
	backend, local, err := b.target(ctx, req.Session)
	if err != nil {
		return gate.RZSZStatus{}, err
	}
	req.Session = local
	return backend.TransferRZSZ(ctx, req)
}

func (state *State) abortRZSZWait(job *rzszJob, reason error) (gate.RZSZStatus, error) {
	finished := make(chan rzszResult, 1)
	if !state.aiPost(func(*State) {
		state.abortRZSZ(job, reason)
		select {
		case res := <-job.done:
			finished <- res
		default:
			finished <- rzszResult{err: reason}
		}
	}) {
		return gate.RZSZStatus{}, reason
	}
	select {
	case res := <-finished:
		return res.status, res.err
	case <-time.After(5 * time.Second):
		return gate.RZSZStatus{}, reason
	}
}

// beginRZSZ types rz or sz. The engine starts when that program's ZMODEM header
// arrives. Window thread.
func (state *State) beginRZSZ(session *Session, req tools.RZSZRequest) (*rzszJob, error) {
	term := session.term
	if term == nil || term.shell == nil || !term.live {
		return nil, errNoShell
	}
	switch term.shellKind() {
	case shellkind.PowerShell, shellkind.Console:
		return nil, fmt.Errorf("rz/sz needs a POSIX shell in this terminal, not %s", term.shellKind())
	}
	if term.exec != nil {
		return nil, errExecBusy
	}
	if session.transferBusy() {
		return nil, errors.New("a file transfer is already running")
	}
	command, job, err := state.prepareRZSZ(session, req)
	if err != nil {
		return nil, err
	}
	job.armed = state.now()
	session.rzsz = job
	if !term.sendAssistant([]byte(command + "\n")) {
		session.rzsz = nil
		state.releaseRZSZ(session, job)
		return nil, errExecRefuse
	}
	// The command is in the shell now, so the shell belongs to this job until it
	// finishes, as it does once the engine starts.
	term.transferHoldsInput = true
	return job, nil
}

func (state *State) prepareRZSZ(session *Session, req tools.RZSZRequest) (string, *rzszJob, error) {
	job := &rzszJob{
		sessionID: session.ID,
		upload:    req.Upload,
		done:      make(chan rzszResult, 1),
	}
	if req.Upload {
		file, info, err := openRZSZUpload(req.LocalPath)
		if err != nil {
			return "", nil, err
		}
		name := filepath.Base(req.LocalPath)
		job.sources = []*os.File{file}
		job.localPath = filepath.Clean(req.LocalPath)
		job.remote = name
		job.bytes = info.Size()
		job.engine = zmodem.NewSender([]zmodem.SendFile{{
			Name: name, Size: info.Size(), ModTime: info.ModTime(), Mode: uint32(info.Mode().Perm()), Source: file,
		}}, zmodem.SendOptions{})
		job.store(rzszSnapshot(session, job))
		command, err := rzszShellCommand(true, req.Remote, req.Overwrite)
		if err != nil {
			state.releaseRZSZ(nil, job)
			return "", nil, err
		}
		return command, job, nil
	}
	dir, name, err := prepareRZSZDownload(req.LocalPath, req.Remote, req.Overwrite)
	if err != nil {
		return "", nil, err
	}
	job.localPath = filepath.Join(dir, name)
	job.remote = name
	job.folder = &folderTarget{dir: dir}
	job.engine = zmodem.NewReceiver(&rzszDownload{dir: dir, name: name, overwrite: req.Overwrite})
	job.store(rzszSnapshot(session, job))
	command, err := rzszShellCommand(false, req.Remote, false)
	if err != nil {
		job.engine.Close()
		return "", nil, err
	}
	return command, job, nil
}

func openRZSZUpload(local string) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(local) {
		return nil, nil, errors.New("local_path must be absolute")
	}
	local = filepath.Clean(local)
	info, err := os.Lstat(local)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("local symbolic links are refused")
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("local_path must be a regular file")
	}
	if info.Size() > maxSendSize {
		return nil, nil, errors.New("the file is too large for one ZMODEM transfer")
	}
	name := filepath.Base(local)
	if name == "." || name == ".." {
		return nil, nil, errors.New("local_path must name a file")
	}
	file, err := os.Open(local)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}

func prepareRZSZDownload(dir, remote string, overwrite bool) (string, string, error) {
	if !filepath.IsAbs(dir) {
		return "", "", errors.New("local_dir must be absolute")
	}
	dir = filepath.Clean(dir)
	info, err := os.Lstat(dir)
	if err != nil {
		return "", "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("local symbolic links are refused")
	}
	if !info.IsDir() {
		return "", "", errors.New("local_dir must be a directory")
	}
	name := path.Base(remote)
	if name == "." || name == ".." || name == "/" {
		return "", "", errors.New("remote_path must name a file")
	}
	dest := filepath.Join(dir, name)
	if _, err := os.Lstat(dest); err == nil && !overwrite {
		return "", "", fmt.Errorf("%s already exists", dest)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	return dir, name, nil
}

// rzszShellCommand is the line typed into a POSIX shell. remote is a directory
// for upload and a file for download.
func rzszShellCommand(upload bool, remote string, overwrite bool) (string, error) {
	if remote != "" {
		if err := toolsPath(remote); err != nil {
			return "", err
		}
	}
	flag := "-p"
	if overwrite {
		flag = "-y"
	}
	if upload {
		command := "rz -b " + flag
		if remote != "" {
			command = "cd -- " + shellQuote(remote) + " && " + command
		}
		return command, nil
	}
	if remote == "" {
		return "", errors.New("remote_path is required")
	}
	return "sz -b -- " + shellQuote(remote), nil
}

func toolsPath(remote string) error {
	if len(remote) > tools.MaxPathLen {
		return fmt.Errorf("path is longer than %d bytes", tools.MaxPathLen)
	}
	for i := 0; i < len(remote); i++ {
		if remote[i] < 0x20 || remote[i] == 0x7f {
			return errors.New("path contains a control character")
		}
	}
	return nil
}

// startArmedRZSZ takes a ZMODEM offer that belongs to the armed call. It
// reports whether the offer was consumed.
func (state *State) startArmedRZSZ(session *Session, peerSends bool) bool {
	job := session.rzsz
	if job == nil || job.sessionID != session.ID {
		return false
	}
	if job.upload == peerSends {
		job.reason = errors.New("the remote ZMODEM offer did not match rz/sz")
		state.declineTransfer(session)
		state.showNextPrompt()
		return true
	}
	session.transfer.sources = job.sources
	session.transfer.target = job.folder
	job.sources = nil
	state.startTransfer(session, job.engine)
	return true
}

func (state *State) abortRZSZ(job *rzszJob, reason error) {
	session, ok := state.sessionByIDPointer(job.sessionID)
	if !ok || session.rzsz != job {
		return
	}
	job.reason = reason
	// A transfer this job did not start owns the shell instead; leave it and
	// finish only the call.
	foreign := session.transfer.engine != nil && session.transfer.engine != job.engine
	if !foreign {
		if session.transfer.phase != transferNone && session.transfer.engine != nil {
			session.transfer.engine.Cancel()
			state.afterTransferFeed(session)
		}
		if session.rzsz != job {
			return
		}
		if session.term != nil {
			session.term.sendProtocol([]byte{3})
			if session.term.shell != nil {
				session.term.shell.Write([]byte("\x18\x18\x18\x18\x18\x18\x18\x18\b\b\b\b\b\b\b\b\b\b"))
			}
		}
		if session.transfer.phase != transferNone {
			state.endTransfer(session)
			return
		}
	}
	state.finishRZSZ(session)
}

// watchRZSZ interrupts a command that never entered ZMODEM, and finishes a
// call whose shell has gone.
func (state *State) watchRZSZ() {
	for index := range state.sessions {
		session := &state.sessions[index]
		job := session.rzsz
		if job == nil || session.transfer.phase != transferNone {
			continue
		}
		if session.term == nil || session.term.shell == nil {
			job.reason = errors.New("the shell ended before rz/sz started")
			state.finishRZSZ(session)
			continue
		}
		if now := state.now(); now > 0 && now-job.armed >= rzszStartWait {
			job.reason = errors.New("rz/sz did not start; the shell may not be at a prompt, or rz and sz are not installed")
			session.term.sendProtocol([]byte{3})
			state.finishRZSZ(session)
		}
	}
}

func (state *State) finishRZSZ(session *Session) {
	if session == nil {
		return
	}
	job := session.rzsz
	if job == nil {
		return
	}
	session.rzsz = nil
	state.releaseRZSZ(session, job)
	if session.transfer.phase == transferNone {
		// An armed job holds the shell before its engine starts; a running
		// transfer releases the same hold when it ends.
		state.releaseTransferInput(session)
	}
	err := job.reason
	files := 0
	engine := session.transfer.engine
	if engine != job.engine {
		// Only the engine this job started says how the call went. A transfer
		// that belongs to other work must not report itself as this one.
		engine = nil
	}
	if engine != nil {
		if err == nil {
			err = engine.Err()
		}
		files = engine.Progress().Files
	}
	if err == nil && files < 1 {
		err = errors.New("rz/sz finished without transferring the file")
	}
	status := gate.RZSZStatus{Direction: "download", Files: files, LocalPath: job.localPath, RemoteName: job.remote}
	if job.upload {
		status.Direction = "upload"
	}
	if err == nil {
		if info, statErr := os.Stat(job.localPath); statErr == nil {
			status.Bytes = info.Size()
		} else {
			status.Bytes = job.bytes
		}
	}
	select {
	case job.done <- rzszResult{status: status, err: err}:
	default:
	}
}

func (state *State) releaseRZSZ(session *Session, job *rzszJob) {
	for _, file := range job.sources {
		file.Close()
	}
	job.sources = nil
	var engine zmodem.Engine
	if session != nil {
		engine = session.transfer.engine
	}
	if engine != job.engine && job.engine != nil {
		job.engine.Close()
	}
}

// rzszDownload writes one received file under a name chosen here, never under
// a name the remote picks, and replaces the destination only after the bytes
// are complete.
type rzszDownload struct {
	dir       string
	name      string
	overwrite bool
}

func (target *rzszDownload) Open(info zmodem.FileInfo) (io.WriteCloser, error) {
	if info.Name != target.name {
		return nil, fmt.Errorf("remote offered %s", info.Name)
	}
	final := filepath.Join(target.dir, target.name)
	if !target.overwrite {
		if _, err := os.Lstat(final); err == nil {
			return nil, fmt.Errorf("%s already exists", final)
		}
	}
	partial := final + ".motty-partial"
	if info, err := os.Lstat(partial); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("local symbolic links are refused")
		}
		if err := os.Remove(partial); err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	return &rzszFile{File: file, partial: partial, final: final, size: info.Size, overwrite: target.overwrite}, nil
}

type rzszFile struct {
	*os.File
	partial   string
	final     string
	size      int64
	written   int64
	overwrite bool
}

func (file *rzszFile) Write(p []byte) (int, error) {
	n, err := file.File.Write(p)
	file.written += int64(n)
	return n, err
}

func (file *rzszFile) Close() error {
	err := file.File.Close()
	if file.size < 0 {
		os.Remove(file.partial)
		if err == nil {
			err = errors.New("the remote did not say the file size")
		}
		return err
	}
	if file.written >= file.size {
		if file.overwrite {
			if removeErr := os.Remove(file.final); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				os.Remove(file.partial)
				if err == nil {
					err = removeErr
				}
				return err
			}
		}
		if renameErr := os.Rename(file.partial, file.final); renameErr != nil {
			os.Remove(file.partial)
			if err == nil {
				err = renameErr
			}
		}
		return err
	}
	os.Remove(file.partial)
	return err
}
