package sftpclient

import (
	"errors"
	"fmt"
	"github.com/vivaui/viva/leaks"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
)

const (
	maxQueuedRequests = 32
	maxTransferItems  = 100_000
	maxTransferDepth  = 128
)

var (
	// ErrCanceled reports an operation interrupted by Cancel, CancelTransfer, or Close.
	ErrCanceled = errors.New("sftp: operation canceled")
	// ErrDestinationExists reports that an explicit overwrite decision is required.
	ErrDestinationExists = errors.New("sftp: destination exists")
	// ErrPathTraversal reports an unsafe name returned by a remote server.
	ErrPathTraversal = errors.New("sftp: path escapes transfer root")
	// ErrSymbolicLink reports a symbolic link that a recursive transfer refuses to follow.
	ErrSymbolicLink = errors.New("sftp: symbolic links are not transferred")
	// ErrResumeMismatch reports that an existing partial file is not a prefix of the source.
	ErrResumeMismatch = errors.New("sftp: partial file does not match source")
)

type ConflictError struct {
	Path string
}

func (err *ConflictError) Error() string {
	return fmt.Sprintf("%s: %s", ErrDestinationExists, err.Path)
}
func (err *ConflictError) Is(target error) bool { return target == ErrDestinationExists }

// Operation identifies the request that produced a Result.
type Operation uint8

const (
	OperationList Operation = iota + 1
	OperationDownload
	OperationUpload
	OperationCreateDirectory
	OperationRename
	OperationRemoveFile
	OperationRemoveDirectory
	OperationChmod
	OperationOpenText
	OperationSaveText
	OperationSymlink
	OperationOpenDocument
	OperationSaveDocument
)

// ExistingPolicy controls what a transfer does when its final destination or
// same-directory partial file already exists.
type ExistingPolicy uint8

const (
	ExistingFail ExistingPolicy = iota
	ExistingReplace
	ExistingResume
	// ExistingSkip leaves files that are already there alone and merges
	// folders: what is missing is copied, what exists is kept.
	ExistingSkip
)

type TransferState uint8

const (
	TransferQueued TransferState = iota + 1
	TransferRunning
	TransferCompleted
	TransferFailed
	TransferCanceled
	// TransferPaused is a transfer the user stopped, started or not; it keeps its
	// place and resumes where it was.
	TransferPaused
)

type TransferSnapshot struct {
	ID          uint64
	Operation   Operation
	Source      string
	Destination string
	Recursive   bool
	State       TransferState
	FilesDone   int
	FilesTotal  int
	BytesDone   int64
	BytesTotal  int64
	// BytesPerSecond is the recent speed of a running transfer, smoothed; zero
	// until there is something to measure and while paused.
	BytesPerSecond int64
	Resumed        int64
	Error          string
	Retryable      bool
	QueuedAt       time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
}

// Entry is one remote directory item prepared for presentation.
type Entry struct {
	Name    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	// UID and GID are the numeric owner and group when the server sent them
	// (HasOwner); Owner and Group are their names when the server's account
	// files had them.
	UID, GID     uint32
	HasOwner     bool
	Owner, Group string
	// For a symbolic link, LinkTarget is what it points at and LinkDir says the
	// target is a folder, so the link is opened like one. LinkBroken says the
	// target does not exist. Resolved only for the first maxLinkLookups links of
	// a folder.
	LinkTarget string
	LinkDir    bool
	LinkBroken bool
}

// IsDir reports a folder, or a link that points at one.
func (entry Entry) IsDir() bool { return entry.Mode.IsDir() || entry.LinkDir }

// IsLink reports a symbolic link.
func (entry Entry) IsLink() bool { return entry.Mode&os.ModeSymlink != 0 }

// maxLinkLookups bounds the round trips a listing spends on resolving links.
const maxLinkLookups = 200

// Result is the immutable completion of one worker request.
type Result struct {
	Data []byte // immutable bounded bytes for independent document windows
	// Background marks the result of Prefetch: a listing the caller asked for in
	// advance. It is the caller's to cache, not to show as the outcome of an
	// action, and its failure is not worth reporting.
	Background  bool
	Operation   Operation
	TransferID  uint64
	Path        string
	Entries     []Entry
	LocalPath   string
	Destination string
	Mode        os.FileMode
	Bytes       int64
	Files       int
	Resumed     int64
	Document    TextDocument
	// Approximate marks a listing read from `ls` output because the server has
	// no SFTP: names are exact, times and link details may not be.
	Approximate bool
	// LinksPending marks a listing sent before its symbolic links were resolved;
	// a FollowUp result with the same Path carries the resolved entries.
	LinksPending bool
	// FollowUp marks that later listing. It is the same directory as an earlier
	// result; the caller updates what it shows without moving the selection.
	FollowUp bool
	Err      error

	// links is the deferred resolution run publishes after this result, so the
	// first listing always reaches the caller before the follow-up.
	links *linkJob
}

// linkJob is the symbolic-link resolution one listing defers to its own
// goroutine. entries is a clone: the first result may be read by the caller
// while this mutates the clone.
type linkJob struct {
	client      Remote
	directory   string
	entries     []Entry
	generation  uint64
	background  bool
	approximate bool
}

type request struct {
	data        []byte
	id          uint64
	background  bool // a listing nobody is waiting on: its Result says so
	operation   Operation
	remote      string
	local       string
	destination string
	mode        os.FileMode
	text        string
	revision    Revision
	utf8BOM     bool
	recursive   bool
	policy      ExistingPolicy
}

type transferRecord struct {
	request  request
	snapshot TransferSnapshot
	settled  bool
	// paused holds the transfer back; active says a goroutine is running it.
	paused, active bool
	// The speed is measured over at least rateWindow, then smoothed.
	rateAt    time.Time
	rateBytes int64
	rate      float64
}

// rateWindow is the shortest interval a transfer's speed is measured over.
const rateWindow = 400 * time.Millisecond

// Progress counters remain exact for every chunk. UI notifications need at
// most one wake per display frame, even when a fast download writes thousands
// of 32 KiB chunks per second. State changes bypass this limit.
const progressFrameInterval = time.Second / 60

// DefaultParallel is how many transfers run at once unless told otherwise;
// MaxParallel is the most that can be asked for.
const (
	DefaultParallel = 2
	MaxParallel     = 6
)

// bufferPool holds the copy buffers, so a transfer borrows 64 KiB while it runs
// and nothing is kept between transfers.
var bufferPool = sync.Pool{New: func() any { return new([2][32 * 1024]byte) }}

type remoteCreator interface {
	Create(path string) (io.WriteCloser, error)
}

type remoteDirectoryCreator interface {
	Mkdir(path string) error
}

type remoteRenamer interface {
	Rename(oldPath, newPath string) error
}

type remoteFileRemover interface {
	Remove(path string) error
}

type remoteDirectoryRemover interface {
	RemoveDirectory(path string) error
}

type remoteChmod interface {
	Chmod(path string, mode os.FileMode) error
}

type remoteLinks interface {
	ReadLink(path string) (string, error)
	Stat(path string) (os.FileInfo, error)
	Symlink(target, link string) error
}

type remoteChtimes interface {
	Chtimes(path string, atime, mtime time.Time) error
}

// Worker runs SFTP operations on one explicitly opened subsystem. Everything but
// transfers goes through one goroutine and one fixed-capacity request queue, in
// order. A transfer runs on a goroutine of its own, at most Parallel of them at a
// time, which exists only while the transfer does; there is no timer. The
// subsystem is opened only after an explicit request.
type Worker struct {
	open     func() (Remote, error)
	notify   func()
	requests chan request
	results  chan Result
	done     chan struct{}
	finished chan struct{}
	close    sync.Once

	mu         sync.Mutex
	client     Remote
	generation uint64
	nextID     uint64
	transfers  []transferRecord
	parallel   int
	// resume is closed and replaced whenever a paused transfer may go on or be
	// stopped, which wakes those waiting in a copy loop.
	resume chan struct{}
	// transferGroup counts the goroutines running transfers.
	transferGroup sync.WaitGroup
	// linksGroup counts the goroutines resolving a listing's symbolic links after
	// the listing itself was published.
	linksGroup sync.WaitGroup
	clock      func() time.Time
	progressAt time.Time // last progress-only notification, protected by mu
	// ids are the user and group names the server's account files gave, read
	// once, on the first listing.
	ids       IDNames
	idsTried  bool
	dropTimes bool // the zero value keeps times
}

func NewWorker(open func() (Remote, error), notify func()) *Worker {
	worker := &Worker{
		open: open, notify: notify,
		requests: make(chan request, maxQueuedRequests),
		results:  make(chan Result, maxQueuedRequests),
		done:     make(chan struct{}), finished: make(chan struct{}),
		parallel: DefaultParallel, resume: make(chan struct{}), clock: time.Now,
	}
	liveWorkers.Acquire()
	go worker.run()
	return worker
}

// liveWorkers counts workers that were created and not yet closed.
var liveWorkers = leaks.New("sftp workers")

func (worker *Worker) List(remotePath string) bool {
	return worker.enqueue(request{operation: OperationList, remote: remotePath})
}

// Prefetch lists a directory ahead of need, for a caller that keeps what it
// learns: it runs in the same order as every other request and its Result is
// marked Background.
func (worker *Worker) Prefetch(remotePath string) bool {
	return worker.enqueue(request{operation: OperationList, remote: remotePath, background: true})
}

// Download preserves the original single-file API. It still stages and
// atomically commits the destination, while retaining the historical replace
// behavior for callers that already made their own overwrite decision.
func (worker *Worker) Download(remotePath, localPath string) bool {
	_, queued := worker.QueueDownload(remotePath, localPath, false, ExistingReplace)
	return queued
}

// Upload preserves the original single-file API with safe staged replacement.
func (worker *Worker) Upload(localPath, remotePath string) bool {
	_, queued := worker.QueueUpload(localPath, remotePath, false, ExistingReplace)
	return queued
}

func (worker *Worker) QueueDownload(remotePath, localPath string, recursive bool, policy ExistingPolicy) (uint64, bool) {
	return worker.enqueueTransfer(request{
		operation: OperationDownload, remote: remotePath, local: localPath,
		recursive: recursive, policy: policy,
	})
}

func (worker *Worker) QueueUpload(localPath, remotePath string, recursive bool, policy ExistingPolicy) (uint64, bool) {
	return worker.enqueueTransfer(request{
		operation: OperationUpload, local: localPath, remote: remotePath,
		recursive: recursive, policy: policy,
	})
}

func (worker *Worker) CreateDirectory(remotePath string) bool {
	return worker.enqueue(request{operation: OperationCreateDirectory, remote: remotePath})
}

func (worker *Worker) Rename(oldPath, newPath string) bool {
	return worker.enqueue(request{operation: OperationRename, remote: oldPath, destination: newPath})
}

func (worker *Worker) RemoveFile(remotePath string) bool {
	return worker.enqueue(request{operation: OperationRemoveFile, remote: remotePath})
}

func (worker *Worker) RemoveDirectory(remotePath string) bool {
	return worker.enqueue(request{operation: OperationRemoveDirectory, remote: remotePath})
}

// Symlink creates a symbolic link at linkPath that points at target.
func (worker *Worker) Symlink(linkPath, target string) bool {
	return worker.enqueue(request{operation: OperationSymlink, remote: linkPath, destination: target})
}

// SetPreserveTimes chooses whether a transferred file keeps its modification
// time (permissions are always kept). Takes effect for files not yet started.
func (worker *Worker) SetPreserveTimes(keep bool) {
	worker.mu.Lock()
	worker.dropTimes = !keep
	worker.mu.Unlock()
}

func (worker *Worker) preserveTimes() bool {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	return !worker.dropTimes
}

func (worker *Worker) Chmod(remotePath string, mode os.FileMode) bool {
	return worker.enqueue(request{operation: OperationChmod, remote: remotePath, mode: mode})
}

// OpenText reads one explicitly selected remote file into the bounded UTF-8
// editor path. It does not start work until called.
func (worker *Worker) OpenText(remotePath string) bool {
	return worker.enqueue(request{operation: OperationOpenText, remote: remotePath})
}

// OpenDocument reads bounded raw bytes without treating an image as UTF-8.
func (worker *Worker) OpenDocument(remotePath string) bool {
	return worker.enqueue(request{operation: OperationOpenDocument, remote: remotePath})
}

// SaveDocument replaces a revision-checked document without reinterpreting its
// bytes. Encoding and image/text policy belong to the document editor.
func (worker *Worker) SaveDocument(remotePath string, data []byte, revision Revision) bool {
	if len(data) > MaxPreviewFileBytes {
		return false
	}
	return worker.enqueue(request{operation: OperationSaveDocument, remote: remotePath, data: append([]byte(nil), data...), revision: revision})
}

// SaveText atomically replaces one previously opened remote text file when its
// contents still match revision. The caller keeps ownership of the editor text.
func (worker *Worker) SaveText(remotePath, value string, revision Revision, utf8BOM bool) bool {
	return worker.enqueue(request{
		operation: OperationSaveText, remote: remotePath, text: value,
		revision: revision, utf8BOM: utf8BOM,
	})
}

func (worker *Worker) enqueueTransfer(next request) (uint64, bool) {
	worker.mu.Lock()
	if worker.closedLocked() || !worker.makeTransferRoomLocked() {
		worker.mu.Unlock()
		return 0, false
	}
	worker.nextID++
	next.id = worker.nextID
	source, destination := next.remote, next.local
	if next.operation == OperationUpload {
		source, destination = next.local, next.remote
	}
	worker.transfers = append(worker.transfers, transferRecord{
		request: next,
		snapshot: TransferSnapshot{
			ID: next.id, Operation: next.operation, Source: source,
			Destination: destination, Recursive: next.recursive, State: TransferQueued,
			QueuedAt: worker.clock(),
		},
	})
	worker.mu.Unlock()
	worker.schedule()
	worker.changed()
	return next.id, true
}

// SetParallel sets how many transfers may run at once (1 to MaxParallel).
func (worker *Worker) SetParallel(count int) {
	worker.mu.Lock()
	worker.parallel = min(max(count, 1), MaxParallel)
	worker.mu.Unlock()
	worker.schedule()
}

// schedule starts queued transfers while there is room for them.
func (worker *Worker) schedule() {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.closedLocked() {
		return
	}
	running := 0
	for index := range worker.transfers {
		if worker.transfers[index].active {
			running++
		}
	}
	for index := range worker.transfers {
		if running >= worker.parallel {
			return
		}
		record := &worker.transfers[index]
		if record.snapshot.State != TransferQueued || record.paused || record.active || record.settled {
			continue
		}
		record.active = true
		record.snapshot.State = TransferRunning
		record.snapshot.StartedAt = worker.clock()
		record.rateAt, record.rateBytes, record.rate = worker.clock(), 0, 0
		running++
		worker.transferGroup.Add(1)
		go worker.runTransfer(record.request)
	}
}

// runTransfer is the goroutine of one transfer.
func (worker *Worker) runTransfer(next request) {
	defer worker.transferGroup.Done()
	result := worker.execute(next)
	worker.finishTransfer(next.id, result)
	worker.publish(result)
	worker.schedule()
}

// Pause holds a transfer back: one that has not started stays where it is, one
// that is running stops moving data and keeps its files open.
func (worker *Worker) Pause(id uint64) bool {
	worker.mu.Lock()
	record := worker.transferLocked(id)
	if record == nil || record.settled || record.paused ||
		(record.snapshot.State != TransferQueued && record.snapshot.State != TransferRunning) {
		worker.mu.Unlock()
		return false
	}
	record.paused = true
	record.snapshot.State = TransferPaused
	record.snapshot.BytesPerSecond, record.rate = 0, 0
	worker.mu.Unlock()
	worker.changed()
	return true
}

// Resume lets a paused transfer go on.
func (worker *Worker) Resume(id uint64) bool {
	worker.mu.Lock()
	record := worker.transferLocked(id)
	if record == nil || record.settled || !record.paused {
		worker.mu.Unlock()
		return false
	}
	record.paused = false
	if record.active {
		record.snapshot.State = TransferRunning
		record.rateAt, record.rateBytes = worker.clock(), record.snapshot.BytesDone
	} else {
		record.snapshot.State = TransferQueued
	}
	worker.wakeLocked()
	worker.mu.Unlock()
	worker.schedule()
	worker.changed()
	return true
}

// wakeLocked wakes every transfer waiting while paused.
func (worker *Worker) wakeLocked() {
	close(worker.resume)
	worker.resume = make(chan struct{})
}

// stopped reports whether work should end: the subsystem was discarded, or the
// transfer (id, when not zero) was cancelled.
func (worker *Worker) stopped(generation, id uint64) bool {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.generation != generation {
		return true
	}
	if id != 0 {
		if record := worker.transferLocked(id); record != nil && record.snapshot.State == TransferCanceled {
			return true
		}
	}
	return false
}

// holdWhilePaused blocks a transfer's copy loop while it is paused. It reports
// false when the transfer was stopped instead.
func (worker *Worker) holdWhilePaused(generation, id uint64) bool {
	for {
		worker.mu.Lock()
		record := worker.transferLocked(id)
		paused := record != nil && record.paused
		wait := worker.resume
		worker.mu.Unlock()
		if !paused {
			return !worker.stopped(generation, id)
		}
		select {
		case <-wait:
		case <-worker.done:
			return false
		}
		if worker.stopped(generation, id) {
			return false
		}
	}
}

func (worker *Worker) makeTransferRoomLocked() bool {
	if len(worker.transfers) < maxQueuedRequests {
		return true
	}
	for index := range worker.transfers {
		record := &worker.transfers[index]
		state := record.snapshot.State
		if record.settled && !record.active && (state == TransferCompleted || state == TransferFailed || state == TransferCanceled) {
			worker.transfers = slices.Delete(worker.transfers, index, index+1)
			return true
		}
	}
	return false
}

func (worker *Worker) enqueue(next request) bool {
	select {
	case <-worker.done:
		return false
	default:
	}
	select {
	case worker.requests <- next:
		return true
	default:
		return false
	}
}

func (worker *Worker) Results() <-chan Result { return worker.results }
func (worker *Worker) Done() <-chan struct{}  { return worker.finished }

// ForgetTransfer removes an ended task from history, never its files. An active
// copy keeps its record until its goroutine has settled.
func (worker *Worker) ForgetTransfer(id uint64) bool {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	for index := range worker.transfers {
		record := &worker.transfers[index]
		if record.snapshot.ID == id {
			if record.active || (record.snapshot.State != TransferCompleted && record.snapshot.State != TransferFailed && record.snapshot.State != TransferCanceled) {
				return false
			}
			worker.transfers = slices.Delete(worker.transfers, index, index+1)
			return true
		}
	}
	return false
}

// Transfers copies the bounded transfer history into dst.
func (worker *Worker) Transfers(dst []TransferSnapshot) []TransferSnapshot {
	worker.mu.Lock()
	count := len(worker.transfers)
	if cap(dst) < count {
		dst = make([]TransferSnapshot, count)
	} else {
		dst = dst[:count]
	}
	for index := range worker.transfers {
		dst[index] = worker.transfers[index].snapshot
	}
	worker.mu.Unlock()
	return dst
}

// Retry requeues a failed or canceled transfer with an explicit destination policy.
func (worker *Worker) Retry(id uint64, policy ExistingPolicy) bool {
	worker.mu.Lock()
	record := worker.transferLocked(id)
	if record == nil || !record.settled || (record.snapshot.State != TransferFailed && record.snapshot.State != TransferCanceled) {
		worker.mu.Unlock()
		return false
	}
	record.request.policy = policy
	record.snapshot.State = TransferQueued
	record.snapshot.Error = ""
	record.snapshot.FilesDone = 0
	record.snapshot.BytesDone = 0
	record.snapshot.Resumed = 0
	record.snapshot.Retryable = false
	record.snapshot.QueuedAt = worker.clock()
	record.snapshot.StartedAt, record.snapshot.FinishedAt = time.Time{}, time.Time{}
	record.snapshot.BytesPerSecond, record.rate = 0, 0
	record.settled = false
	record.paused = false
	worker.mu.Unlock()
	worker.schedule()
	worker.changed()
	return true
}

func (worker *Worker) CancelTransfer(id uint64) bool {
	worker.mu.Lock()
	record := worker.transferLocked(id)
	if record == nil || record.snapshot.State == TransferCompleted || record.snapshot.State == TransferCanceled {
		worker.mu.Unlock()
		return false
	}
	record.snapshot.State = TransferCanceled
	record.snapshot.FinishedAt = worker.clock()
	record.snapshot.Error = ErrCanceled.Error()
	record.snapshot.Retryable = false
	record.snapshot.BytesPerSecond, record.rate = 0, 0
	record.paused = false
	active := record.active
	if !active {
		// A queued task has no copy goroutine to finish its cancellation.
		record.settled, record.snapshot.Retryable = true, true
	}
	others := 0
	for index := range worker.transfers {
		if worker.transfers[index].active && worker.transfers[index].snapshot.ID != id {
			others++
		}
	}
	worker.wakeLocked()
	worker.mu.Unlock()
	if active && others == 0 {
		// Nothing else is using the subsystem, and a read that is waiting on the
		// network only ends when it goes.
		if client := worker.invalidate(); client != nil {
			_ = client.Close()
		}
	}
	worker.changed()
	return true
}

// Cancel interrupts everything under way and discards the current subsystem.
func (worker *Worker) Cancel() {
	worker.mu.Lock()
	for index := range worker.transfers {
		record := &worker.transfers[index]
		if record.active && record.snapshot.State != TransferCanceled {
			record.snapshot.State = TransferCanceled
			record.snapshot.FinishedAt = worker.clock()
			record.snapshot.Error = ErrCanceled.Error()
			record.snapshot.Retryable = true
			record.snapshot.BytesPerSecond, record.rate = 0, 0
			record.paused = false
		}
	}
	worker.wakeLocked()
	worker.mu.Unlock()
	if client := worker.invalidate(); client != nil {
		_ = client.Close()
	}
	worker.changed()
}

func (worker *Worker) Close() {
	worker.close.Do(func() {
		liveWorkers.Release()
		close(worker.done)
		worker.mu.Lock()
		for index := range worker.transfers {
			state := worker.transfers[index].snapshot.State
			if state == TransferQueued || state == TransferRunning || state == TransferPaused {
				worker.transfers[index].snapshot.State = TransferCanceled
				worker.transfers[index].snapshot.Error = ErrCanceled.Error()
				worker.transfers[index].snapshot.FinishedAt = worker.clock()
				worker.transfers[index].snapshot.BytesPerSecond = 0
			}
		}
		worker.mu.Unlock()
		worker.Cancel()
	})
}

func (worker *Worker) transferLocked(id uint64) *transferRecord {
	for index := range worker.transfers {
		if worker.transfers[index].snapshot.ID == id {
			return &worker.transfers[index]
		}
	}
	return nil
}

func (worker *Worker) closedLocked() bool {
	select {
	case <-worker.done:
		return true
	default:
		return false
	}
}

func (worker *Worker) changed() {
	if worker.notify != nil {
		worker.notify()
	}
}

func (worker *Worker) invalidate() Remote {
	worker.mu.Lock()
	worker.generation++
	client := worker.client
	worker.client = nil
	worker.mu.Unlock()
	return client
}

func (worker *Worker) snapshot() uint64 {
	worker.mu.Lock()
	generation := worker.generation
	worker.mu.Unlock()
	return generation
}

func (worker *Worker) canceled(generation uint64) bool {
	worker.mu.Lock()
	canceled := worker.generation != generation
	worker.mu.Unlock()
	return canceled
}

func (worker *Worker) run() {
	defer close(worker.finished)
	defer close(worker.results)
	defer worker.transferGroup.Wait() // runs before the results close: transfers publish there
	defer worker.linksGroup.Wait()    // and so do the deferred link resolutions
	defer worker.Cancel()
	for {
		select {
		case <-worker.done:
			return
		case next := <-worker.requests:
			result := worker.execute(next)
			worker.publish(result)
			if result.links != nil {
				job := *result.links
				worker.linksGroup.Add(1)
				go func() {
					defer worker.linksGroup.Done()
					worker.resolveLinks(job)
				}()
			}
		}
	}
}

func (worker *Worker) publish(result Result) {
	select {
	case worker.results <- result:
		worker.changed()
	case <-worker.done:
	}
}

func (worker *Worker) finishTransfer(id uint64, result Result) {
	worker.mu.Lock()
	record := worker.transferLocked(id)
	if record != nil {
		record.snapshot.FilesDone = result.Files
		record.snapshot.BytesDone = result.Bytes
		record.snapshot.Resumed = result.Resumed
		switch {
		case errors.Is(result.Err, ErrCanceled):
			record.snapshot.State = TransferCanceled
			record.snapshot.Error = ErrCanceled.Error()
			record.snapshot.Retryable = true
		case result.Err != nil:
			record.snapshot.State = TransferFailed
			record.snapshot.Error = result.Err.Error()
			record.snapshot.Retryable = true
		default:
			record.snapshot.State = TransferCompleted
			record.snapshot.Retryable = false
		}
		record.settled = true
		record.snapshot.FinishedAt = worker.clock()
		record.active, record.paused = false, false
		record.snapshot.BytesPerSecond, record.rate = 0, 0
	}
	worker.mu.Unlock()
	worker.changed()
}

func (worker *Worker) setTotals(id uint64, files int, bytes int64) {
	worker.mu.Lock()
	if record := worker.transferLocked(id); record != nil {
		record.snapshot.FilesTotal = files
		record.snapshot.BytesTotal = bytes
	}
	worker.mu.Unlock()
	worker.changed()
}

func (worker *Worker) setProgress(id uint64, files int, bytes, resumed int64) {
	notify := false
	worker.mu.Lock()
	if record := worker.transferLocked(id); record != nil && (record.snapshot.State == TransferRunning || record.snapshot.State == TransferPaused) {
		fileChanged := record.snapshot.FilesDone != files
		changed := fileChanged || record.snapshot.BytesDone != bytes || record.snapshot.Resumed != resumed
		now := worker.clock()
		record.snapshot.FilesDone = files
		record.snapshot.BytesDone = bytes
		record.snapshot.Resumed = resumed
		if record.snapshot.State == TransferRunning && now.Sub(record.rateAt) >= rateWindow {
			instant := float64(bytes-record.rateBytes) / now.Sub(record.rateAt).Seconds()
			if record.rate == 0 {
				record.rate = instant
			} else {
				record.rate = 0.6*record.rate + 0.4*instant
			}
			record.rateAt, record.rateBytes = now, bytes
			record.snapshot.BytesPerSecond = int64(max(record.rate, 0))
		}
		finishedBytes := record.snapshot.BytesTotal > 0 && bytes >= record.snapshot.BytesTotal
		if changed && (worker.progressAt.IsZero() || now.Sub(worker.progressAt) >= progressFrameInterval || fileChanged || finishedBytes) {
			notify = true
			worker.progressAt = now
		}
	}
	worker.mu.Unlock()
	if notify {
		worker.changed()
	}
}

func (worker *Worker) execute(next request) Result {
	result := Result{
		Background: next.background,
		Operation:  next.operation, TransferID: next.id, Path: next.remote,
		LocalPath: next.local, Destination: next.destination, Mode: next.mode,
	}
	generation := worker.snapshot()
	client, err := worker.remote(generation)
	if err != nil {
		result.Err = err
		return result
	}
	switch next.operation {
	case OperationList:
		result.Path, result.Entries, result.Err = worker.list(client, next.remote, generation)
		result.Approximate = IsSCP(client)
		if result.Err == nil && hasLinks(result.Entries) {
			if _, ok := client.(remoteLinks); ok {
				result.LinksPending = true
				result.links = &linkJob{
					client: client, directory: result.Path, entries: slices.Clone(result.Entries),
					generation: generation, background: next.background, approximate: result.Approximate,
				}
			}
		}
	case OperationDownload:
		result.Files, result.Bytes, result.Resumed, result.Err = worker.download(client, next, generation)
	case OperationUpload:
		result.Files, result.Bytes, result.Resumed, result.Err = worker.upload(client, next, generation)
	case OperationCreateDirectory:
		result.Err = createDirectory(client, next.remote)
	case OperationRename:
		result.Err = rename(client, next.remote, next.destination)
	case OperationRemoveFile:
		result.Err = removeFile(client, next.remote)
	case OperationRemoveDirectory:
		result.Err = removeDirectory(client, next.remote)
	case OperationChmod:
		result.Err = chmod(client, next.remote, next.mode)
	case OperationSymlink:
		result.Err = symlink(client, next.remote, next.destination)
	case OperationOpenText:
		result.Document, result.Err = worker.openText(client, next.remote, generation)
	case OperationSaveText:
		result.Document, result.Err = worker.saveText(client, next.remote, next.text, next.revision, next.utf8BOM, generation)
	case OperationOpenDocument:
		result.Data, result.Err = worker.readFileBytes(client, next.remote, generation, MaxPreviewFileBytes)
	case OperationSaveDocument:
		result.Err = worker.replaceDocumentBytes(client, next.remote, next.data, next.revision, generation)
		if result.Err == nil {
			result.Data = next.data
		}
	default:
		result.Err = fmt.Errorf("sftp: unknown operation %d", next.operation)
	}
	if worker.canceled(generation) {
		result.Err = ErrCanceled
	}
	return result
}

func (worker *Worker) remote(generation uint64) (Remote, error) {
	worker.mu.Lock()
	client := worker.client
	worker.mu.Unlock()
	if client != nil {
		return client, nil
	}
	client, err := worker.open()
	if err != nil {
		return nil, err
	}
	worker.mu.Lock()
	if worker.generation != generation {
		worker.mu.Unlock()
		_ = client.Close()
		return nil, ErrCanceled
	}
	worker.client = client
	worker.mu.Unlock()
	return client, nil
}

func (worker *Worker) list(client Remote, remotePath string, generation uint64) (string, []Entry, error) {
	if remotePath == "" {
		resolved, err := client.Getwd()
		if err != nil {
			return "", nil, err
		}
		remotePath = resolved
	}
	infos, err := client.ReadDir(remotePath)
	if err != nil {
		return remotePath, nil, err
	}
	if worker.canceled(generation) {
		return remotePath, nil, ErrCanceled
	}
	entries := entriesFrom(infos)
	worker.describeOwners(client, entries)
	return remotePath, entries, nil
}

// describeOwners fills in the names of the owners and groups a listing carries
// as numbers. It reads the account files once, on the first listing.
func (worker *Worker) describeOwners(client Remote, entries []Entry) {
	worker.mu.Lock()
	tried := worker.idsTried
	worker.idsTried = true
	worker.mu.Unlock()
	if !tried {
		ids := loadIDNames(client)
		worker.mu.Lock()
		worker.ids = ids
		worker.mu.Unlock()
	}
	worker.mu.Lock()
	ids := worker.ids
	worker.mu.Unlock()
	for index := range entries {
		if entry := &entries[index]; entry.HasOwner {
			entry.Owner, entry.Group = ids.Users[entry.UID], ids.Groups[entry.GID]
		}
	}
}

// resolveLinks resolves the symbolic links of a published listing and then
// publishes the same directory once more, as a FollowUp. It runs after the
// listing went out, so a directory with hundreds of links opens at once instead
// of waiting two round trips per link.
func (worker *Worker) resolveLinks(job linkJob) {
	worker.describeLinks(job.client, job.directory, job.entries, job.generation)
	if worker.canceled(job.generation) {
		return
	}
	worker.publish(Result{
		Background: job.background, Operation: OperationList, Path: job.directory,
		Entries: job.entries, Approximate: job.approximate, FollowUp: true,
	})
}

// describeLinks resolves up to maxLinkLookups links of a listing, in place.
func (worker *Worker) describeLinks(client Remote, directory string, entries []Entry, generation uint64) {
	links, canLink := client.(remoteLinks)
	if !canLink {
		return
	}
	lookups := 0
	for index := range entries {
		entry := &entries[index]
		if !entry.IsLink() || lookups >= maxLinkLookups || worker.canceled(generation) {
			continue
		}
		lookups++
		linkPath := path.Join(directory, entry.Name)
		if target, err := links.ReadLink(linkPath); err == nil {
			entry.LinkTarget = target
		}
		if info, err := links.Stat(linkPath); err == nil {
			entry.LinkDir = info.IsDir()
		} else if errors.Is(err, os.ErrNotExist) {
			entry.LinkBroken = true
		}
	}
}

// hasLinks reports whether a listing has anything for describeLinks to do.
func hasLinks(entries []Entry) bool {
	for index := range entries {
		if entries[index].IsLink() {
			return true
		}
	}
	return false
}

func entriesFrom(infos []os.FileInfo) []Entry {
	entries := make([]Entry, 0, len(infos))
	for _, info := range infos {
		if info == nil || info.Name() == "." || info.Name() == ".." {
			continue
		}
		entry := Entry{Name: info.Name(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
		if stat, ok := info.Sys().(*sftp.FileStat); ok && stat != nil {
			entry.UID, entry.GID, entry.HasOwner = stat.UID, stat.GID, true
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(left, right Entry) int {
		if left.IsDir() != right.IsDir() {
			if left.IsDir() {
				return -1
			}
			return 1
		}
		return strings.Compare(left.Name, right.Name)
	})
	return entries
}

func (worker *Worker) upload(client Remote, next request, generation uint64) (files int, bytes, resumed int64, err error) {
	if next.local == "" || next.remote == "" {
		return 0, 0, 0, fmt.Errorf("sftp: upload requires local and remote paths")
	}
	if remote, isSCP := client.(*scpRemote); isSCP {
		return worker.scpUpload(remote, next, generation)
	}
	advanced, ok := client.(transferRemote)
	if !ok {
		if next.recursive || next.policy != ExistingReplace {
			return 0, 0, 0, fmt.Errorf("sftp: safe upload is not supported by the remote")
		}
		written, fallbackErr := worker.legacyUpload(client, next.local, next.remote, generation)
		return boolInt(fallbackErr == nil), written, 0, fallbackErr
	}
	info, totalFiles, totalBytes, err := scanLocal(next.local, next.recursive)
	if err != nil {
		return 0, 0, 0, err
	}
	worker.setTotals(next.id, totalFiles, totalBytes)
	if !info.IsDir() {
		bytes, resumed, err = worker.uploadFile(advanced, next.local, next.remote, info, next.policy, generation, next.id, 0, 0)
		if err == nil {
			files = 1
		}
		return files, bytes, resumed, err
	}
	if err := ensureRemoteRoot(advanced, next.remote, next.policy); err != nil {
		return 0, 0, 0, err
	}
	root := filepath.Clean(next.local)
	err = filepath.WalkDir(root, func(localPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if worker.stopped(generation, next.id) {
			return ErrCanceled
		}
		relative, err := filepath.Rel(root, localPath)
		if err != nil || relative == "." {
			return err
		}
		remotePath, err := joinRemoteWithin(next.remote, filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymbolicLink, localPath)
		}
		if entryInfo.IsDir() {
			return ensureRemoteDirectory(advanced, remotePath)
		}
		written, continued, err := worker.uploadFile(advanced, localPath, remotePath, entryInfo, next.policy, generation, next.id, files, bytes)
		bytes += written
		resumed += continued
		if err == nil {
			files++
			worker.setProgress(next.id, files, bytes, resumed)
		}
		return err
	})
	return files, bytes, resumed, err
}

func (worker *Worker) legacyUpload(client Remote, localPath, remotePath string, generation uint64) (written int64, err error) {
	creator, ok := client.(remoteCreator)
	if !ok {
		return 0, fmt.Errorf("sftp: upload is not supported by the remote")
	}
	source, err := os.Open(localPath)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	destination, err := creator.Create(remotePath)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, destination.Close()) }()
	return worker.uploadCopy(destination, source, generation, 0, 0, 0, 0)
}

func scanLocal(localPath string, recursive bool) (os.FileInfo, int, int64, error) {
	info, err := os.Lstat(localPath)
	if err != nil {
		return nil, 0, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, localPath)
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil, 0, 0, fmt.Errorf("sftp: unsupported local file type: %s", localPath)
		}
		return info, 1, info.Size(), nil
	}
	if !recursive {
		return nil, 0, 0, fmt.Errorf("sftp: directory upload requires recursive mode")
	}
	files := 0
	var bytes int64
	items := 0
	root := filepath.Clean(localPath)
	err = filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		items++
		if items > maxTransferItems {
			return fmt.Errorf("sftp: directory exceeds %d transfer items", maxTransferItems)
		}
		relative, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return relErr
		}
		if depth := pathDepth(filepath.ToSlash(relative)); depth > maxTransferDepth {
			return fmt.Errorf("sftp: directory exceeds transfer depth %d", maxTransferDepth)
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymbolicLink, current)
		}
		if !entryInfo.IsDir() {
			if !entryInfo.Mode().IsRegular() {
				return fmt.Errorf("sftp: unsupported local file type: %s", current)
			}
			files++
			bytes += entryInfo.Size()
		}
		return nil
	})
	return info, files, bytes, err
}

func (worker *Worker) uploadFile(client transferRemote, localPath, remotePath string, sourceInfo os.FileInfo, policy ExistingPolicy, generation, transferID uint64, filesDone int, bytesBefore int64) (written, resumed int64, err error) {
	finalInfo, finalExists, err := remoteLstat(client, remotePath)
	if err != nil {
		return 0, 0, err
	}
	if finalExists {
		if finalInfo.Mode()&os.ModeSymlink != 0 {
			return 0, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, remotePath)
		}
		if finalInfo.IsDir() {
			return 0, 0, &ConflictError{Path: remotePath}
		}
		if policy == ExistingFail {
			return 0, 0, &ConflictError{Path: remotePath}
		}
		if policy == ExistingSkip {
			// Already there: counted as done, not copied.
			worker.setProgress(transferID, filesDone, bytesBefore+sourceInfo.Size(), sourceInfo.Size())
			return sourceInfo.Size(), sourceInfo.Size(), nil
		}
	}
	partial := remotePartialPath(remotePath)
	partialInfo, partialExists, err := remoteLstat(client, partial)
	if err != nil {
		return 0, 0, err
	}
	if partialExists && (partialInfo.IsDir() || partialInfo.Mode()&os.ModeSymlink != 0) {
		return 0, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, partial)
	}
	source, err := os.Open(localPath)
	if err != nil {
		return 0, 0, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	destination, err := client.OpenTransferFile(partial, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return 0, 0, err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, destination.Close())
		}
	}()
	if policy == ExistingResume && partialExists {
		if partialInfo.Size() > sourceInfo.Size() {
			return 0, 0, fmt.Errorf("%w: %s", ErrResumeMismatch, partial)
		}
		if err := worker.verifyPrefix(source, destination, partialInfo.Size(), generation, transferID); err != nil {
			return 0, 0, fmt.Errorf("%w: %s: %v", ErrResumeMismatch, partial, err)
		}
		resumed = partialInfo.Size()
	} else {
		if err := destination.Truncate(0); err != nil {
			return 0, 0, err
		}
		if _, err := destination.Seek(0, io.SeekStart); err != nil {
			return 0, 0, err
		}
	}
	worker.setProgress(transferID, filesDone, bytesBefore+resumed, resumed)
	newBytes, err := worker.uploadCopy(destination, source, generation, transferID, filesDone, bytesBefore, resumed)
	written = resumed + newBytes
	if err != nil {
		return written, resumed, err
	}
	if err := destination.Sync(); err != nil && !isUnsupportedSFTP(err) {
		return written, resumed, err
	}
	if err := destination.Close(); err != nil {
		return written, resumed, err
	}
	closed = true
	if err := client.Chmod(partial, sourceInfo.Mode().Perm()); err != nil {
		return written, resumed, err
	}
	if timer, ok := client.(remoteChtimes); ok && worker.preserveTimes() {
		_ = timer.Chtimes(partial, sourceInfo.ModTime(), sourceInfo.ModTime()) // best effort: some servers refuse
	}
	if err := commitRemote(client, partial, remotePath, finalExists); err != nil {
		return written, resumed, err
	}
	return written, resumed, nil
}

func (worker *Worker) download(client Remote, next request, generation uint64) (files int, bytes, resumed int64, err error) {
	if next.remote == "" || next.local == "" {
		return 0, 0, 0, fmt.Errorf("sftp: download requires remote and local paths")
	}
	if remote, isSCP := client.(*scpRemote); isSCP {
		return worker.scpDownload(remote, next, generation)
	}
	advanced, ok := client.(transferRemote)
	if !ok {
		if next.recursive || next.policy != ExistingReplace {
			return 0, 0, 0, fmt.Errorf("sftp: safe download is not supported by the remote")
		}
		written, fallbackErr := worker.legacyDownload(client, next.remote, next.local, generation)
		return boolInt(fallbackErr == nil), written, 0, fallbackErr
	}
	plan, totalBytes, err := scanRemote(advanced, next.remote, next.recursive)
	if err != nil {
		return 0, 0, 0, err
	}
	totalFiles := 0
	for index := range plan {
		if !plan[index].directory {
			totalFiles++
		}
	}
	worker.setTotals(next.id, totalFiles, totalBytes)
	rootIsDirectory := len(plan) > 0 && plan[0].directory
	if rootIsDirectory {
		if err := ensureLocalRoot(next.local, next.policy); err != nil {
			return 0, 0, 0, err
		}
	}
	for _, item := range plan {
		if worker.stopped(generation, next.id) {
			return files, bytes, resumed, ErrCanceled
		}
		localPath := next.local
		if rootIsDirectory && item.relative != "." {
			localPath, err = joinLocalWithin(next.local, item.relative)
			if err != nil {
				return files, bytes, resumed, err
			}
		}
		if item.directory {
			if err := ensureLocalDirectory(localPath, item.mode); err != nil {
				return files, bytes, resumed, err
			}
			continue
		}
		written, continued, err := worker.downloadFile(advanced, item.remote, localPath, item.mode, item.modTime, item.size, next.policy, generation, next.id, files, bytes)
		bytes += written
		resumed += continued
		if err != nil {
			return files, bytes, resumed, err
		}
		files++
		worker.setProgress(next.id, files, bytes, resumed)
	}
	return files, bytes, resumed, nil
}

type remotePlanItem struct {
	remote    string
	relative  string
	mode      os.FileMode
	modTime   time.Time
	size      int64
	directory bool
}

func scanRemote(client transferRemote, remotePath string, recursive bool) ([]remotePlanItem, int64, error) {
	info, exists, err := remoteLstat(client, remotePath)
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, os.ErrNotExist
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, remotePath)
	}
	if !info.IsDir() {
		return []remotePlanItem{{remote: remotePath, relative: ".", mode: info.Mode(), modTime: info.ModTime(), size: info.Size()}}, info.Size(), nil
	}
	if !recursive {
		return nil, 0, fmt.Errorf("sftp: directory download requires recursive mode")
	}
	root := path.Clean(remotePath)
	plan := []remotePlanItem{{remote: root, relative: ".", mode: info.Mode(), directory: true}}
	var bytes int64
	var walk func(string, string, int) error
	walk = func(current, relative string, depth int) error {
		if depth > maxTransferDepth {
			return fmt.Errorf("sftp: directory exceeds transfer depth %d", maxTransferDepth)
		}
		infos, err := client.ReadDir(current)
		if err != nil {
			return err
		}
		for _, child := range infos {
			if child == nil {
				continue
			}
			name := child.Name()
			if !safeRemoteName(name) {
				return fmt.Errorf("%w: %q", ErrPathTraversal, name)
			}
			if child.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: %s", ErrSymbolicLink, path.Join(current, name))
			}
			childRemote, err := joinRemoteWithin(root, path.Join(relative, name))
			if err != nil {
				return err
			}
			childRelative := path.Join(relative, name)
			plan = append(plan, remotePlanItem{
				remote: childRemote, relative: childRelative, mode: child.Mode(), modTime: child.ModTime(),
				size: child.Size(), directory: child.IsDir(),
			})
			if len(plan) > maxTransferItems {
				return fmt.Errorf("sftp: directory exceeds %d transfer items", maxTransferItems)
			}
			if child.IsDir() {
				if err := walk(childRemote, childRelative, depth+1); err != nil {
					return err
				}
			} else {
				bytes += child.Size()
			}
		}
		return nil
	}
	err = walk(root, "", 1)
	return plan, bytes, err
}

func (worker *Worker) legacyDownload(client Remote, remotePath, localPath string, generation uint64) (written int64, err error) {
	source, err := client.Open(remotePath)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	temporary, err := os.CreateTemp(filepath.Dir(localPath), "."+filepath.Base(localPath)+".motty-*")
	if err != nil {
		return 0, err
	}
	temporaryPath := temporary.Name()
	committed := false
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, temporary.Close())
		}
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	written, err = worker.downloadCopy(temporary, source, generation, 0, 0, 0, 0)
	if err != nil {
		return written, err
	}
	if err := temporary.Sync(); err != nil {
		return written, err
	}
	if err := temporary.Close(); err != nil {
		return written, err
	}
	closed = true
	if err := replaceLocalFile(temporaryPath, localPath); err != nil {
		return written, err
	}
	committed = true
	return written, nil
}

func (worker *Worker) downloadFile(client transferRemote, remotePath, localPath string, mode os.FileMode, modTime time.Time, size int64, policy ExistingPolicy, generation, transferID uint64, filesDone int, bytesBefore int64) (written, resumed int64, err error) {
	finalInfo, finalExists, err := localLstat(localPath)
	if err != nil {
		return 0, 0, err
	}
	if finalExists {
		if finalInfo.Mode()&os.ModeSymlink != 0 {
			return 0, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, localPath)
		}
		if finalInfo.IsDir() || policy == ExistingFail {
			return 0, 0, &ConflictError{Path: localPath}
		}
		if policy == ExistingSkip {
			worker.setProgress(transferID, filesDone, bytesBefore+size, size)
			return size, size, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return 0, 0, err
	}
	partial := localPartialPath(localPath)
	partialInfo, partialExists, err := localLstat(partial)
	if err != nil {
		return 0, 0, err
	}
	if partialExists && (partialInfo.IsDir() || partialInfo.Mode()&os.ModeSymlink != 0) {
		return 0, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, partial)
	}
	source, err := client.OpenTransferFile(remotePath, os.O_RDONLY)
	if err != nil {
		return 0, 0, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	flags := os.O_RDWR | os.O_CREATE
	destination, err := os.OpenFile(partial, flags, 0o600)
	if err != nil {
		return 0, 0, err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, destination.Close())
		}
	}()
	if policy == ExistingResume && partialExists {
		if partialInfo.Size() > size {
			return 0, 0, fmt.Errorf("%w: %s", ErrResumeMismatch, partial)
		}
		if err := worker.verifyPrefix(source, destination, partialInfo.Size(), generation, transferID); err != nil {
			return 0, 0, fmt.Errorf("%w: %s: %v", ErrResumeMismatch, partial, err)
		}
		resumed = partialInfo.Size()
	} else {
		if err := destination.Truncate(0); err != nil {
			return 0, 0, err
		}
		if _, err := destination.Seek(0, io.SeekStart); err != nil {
			return 0, 0, err
		}
	}
	worker.setProgress(transferID, filesDone, bytesBefore+resumed, resumed)
	newBytes, err := worker.downloadCopy(destination, source, generation, transferID, filesDone, bytesBefore, resumed)
	written = resumed + newBytes
	if err != nil {
		return written, resumed, err
	}
	if err := destination.Sync(); err != nil {
		return written, resumed, err
	}
	if err := destination.Chmod(mode.Perm()); err != nil {
		return written, resumed, err
	}
	if err := destination.Close(); err != nil {
		return written, resumed, err
	}
	closed = true
	if err := replaceLocalFile(partial, localPath); err != nil {
		return written, resumed, err
	}
	if worker.preserveTimes() && !modTime.IsZero() {
		_ = os.Chtimes(localPath, modTime, modTime)
	}
	return written, resumed, nil
}

// uploadCopy moves a local file to a remote writer. The SFTP client can keep
// many writes in flight for one file; without that a high latency link pays one
// round trip per 32 KiB, the same limit downloads had. A destination that does
// not offer it keeps the sequential copy.
func (worker *Worker) uploadCopy(destination io.Writer, source io.Reader, generation, transferID uint64, filesDone int, bytesBefore, resumed int64) (int64, error) {
	readFrom, ok := destination.(interface {
		ReadFromWithConcurrency(io.Reader, int) (int64, error)
	})
	if !ok {
		return worker.copy(destination, source, generation, transferID, filesDone, bytesBefore, resumed)
	}
	progress := &copyReadProgress{
		worker: worker, source: source, generation: generation, transferID: transferID,
	}
	var written int64
	for {
		// ReadFrom only confirms the writes when it returns. Keep pipelining
		// bounded batches, publishing acknowledged bytes rather than read-ahead.
		n, err := readFrom.ReadFromWithConcurrency(io.LimitReader(progress, uploadProgressBatch), uploadConcurrency)
		if err != nil {
			if progress.read == 0 {
				return worker.copy(destination, source, generation, transferID, filesDone, bytesBefore, resumed)
			}
			return written, err
		}
		written += n
		if transferID != 0 {
			worker.setProgress(transferID, filesDone, bytesBefore+resumed+written, resumed)
		}
		if n < uploadProgressBatch {
			return written, nil
		}
	}
}

// uploadConcurrency is how many 32 KiB writes one uploaded file keeps in
// flight. It is high enough to cover a wide-area round trip and low enough to
// leave room for the other transfer of a panel and for other SFTP requests.
const uploadConcurrency = 16

const uploadProgressBatch = int64(uploadConcurrency * 32 * 1024)

// copyReadProgress tracks read-ahead for fallback detection and applies pause
// and cancel to the reader. Only uploadCopy publishes acknowledged progress.
type copyReadProgress struct {
	worker     *Worker
	source     io.Reader
	generation uint64
	transferID uint64
	read       int64
}

func (progress *copyReadProgress) Read(chunk []byte) (int, error) {
	if progress.transferID != 0 {
		if !progress.worker.holdWhilePaused(progress.generation, progress.transferID) {
			return 0, ErrCanceled
		}
	} else if progress.worker.canceled(progress.generation) {
		return 0, ErrCanceled
	}
	read, err := progress.source.Read(chunk)
	progress.read += int64(read)
	return read, err
}

// downloadCopy keeps pipelined reads when the source supports WriteTo. Fallback
// is only safe before the destination was tried; a local write failure must not
// be hidden by retrying from the source's already advanced offset.
func (worker *Worker) downloadCopy(destination io.Writer, source io.Reader, generation, transferID uint64, filesDone int, bytesBefore, resumed int64) (int64, error) {
	writerTo, ok := source.(io.WriterTo)
	if !ok {
		return worker.copy(destination, source, generation, transferID, filesDone, bytesBefore, resumed)
	}
	progress := &copyProgress{
		worker: worker, destination: destination, generation: generation, transferID: transferID,
		filesDone: filesDone, bytesBefore: bytesBefore, resumed: resumed,
	}
	_, err := writerTo.WriteTo(progress)
	if err != nil && !progress.started {
		// A server that refuses a stat on an open file still reads fine one
		// request at a time; WriteTo gave up before moving any byte, so the
		// source is still where it started.
		return worker.copy(destination, source, generation, transferID, filesDone, bytesBefore, resumed)
	}
	return progress.written, err
}

// copyProgress counts a pipelined download, keeps its progress fresh on the
// window, and is the point where pause and cancel stop it.
type copyProgress struct {
	worker      *Worker
	destination io.Writer
	generation  uint64
	transferID  uint64
	filesDone   int
	bytesBefore int64
	resumed     int64
	written     int64
	started     bool
}

func (progress *copyProgress) Write(chunk []byte) (int, error) {
	if progress.transferID != 0 {
		if !progress.worker.holdWhilePaused(progress.generation, progress.transferID) {
			return 0, ErrCanceled
		}
	} else if progress.worker.canceled(progress.generation) {
		return 0, ErrCanceled
	}
	progress.started = true
	written, err := progress.destination.Write(chunk)
	if err == nil && written < len(chunk) {
		err = io.ErrShortWrite
	}
	progress.written += int64(written)
	if progress.transferID != 0 {
		progress.worker.setProgress(progress.transferID, progress.filesDone, progress.bytesBefore+progress.resumed+progress.written, progress.resumed)
	}
	return written, err
}

func (worker *Worker) copy(destination io.Writer, source io.Reader, generation, transferID uint64, filesDone int, bytesBefore, resumed int64) (int64, error) {
	pair := bufferPool.Get().(*[2][32 * 1024]byte)
	defer bufferPool.Put(pair)
	buffer := pair[0][:]
	var written int64
	for {
		if transferID != 0 {
			if !worker.holdWhilePaused(generation, transferID) {
				return written, ErrCanceled
			}
		} else if worker.canceled(generation) {
			return written, ErrCanceled
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			offset := 0
			for offset < read {
				count, writeErr := destination.Write(buffer[offset:read])
				written += int64(count)
				offset += count
				if writeErr != nil {
					return written, writeErr
				}
				if count == 0 {
					return written, io.ErrShortWrite
				}
			}
			if transferID != 0 {
				worker.setProgress(transferID, filesDone, bytesBefore+resumed+written, resumed)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

func (worker *Worker) verifyPrefix(source, partial io.Reader, size int64, generation, transferID uint64) error {
	if size <= 0 {
		return nil
	}
	// Both sides of a resumed transfer are usually seekable files that can read
	// at an offset; comparing them concurrently turns a large prefix check from
	// one 32 KiB request per round trip into several in flight. Everything else
	// keeps the sequential path.
	sourceAt, sourceOK := source.(io.ReaderAt)
	partialAt, partialOK := partial.(io.ReaderAt)
	sourceSeek, sourceSeekOK := source.(io.Seeker)
	partialSeek, partialSeekOK := partial.(io.Seeker)
	if !sourceOK || !partialOK || !sourceSeekOK || !partialSeekOK {
		return worker.verifyPrefixSequential(source, partial, size, generation, transferID)
	}
	if err := worker.verifyPrefixParallel(sourceAt, partialAt, size, generation, transferID); err != nil {
		return err
	}
	// ReaderAt does not move the offset; the copy that follows starts after the
	// verified prefix, as the sequential loop left it.
	if _, err := sourceSeek.Seek(size, io.SeekStart); err != nil {
		return err
	}
	_, err := partialSeek.Seek(size, io.SeekStart)
	return err
}

func (worker *Worker) verifyPrefixSequential(source, partial io.Reader, size int64, generation, transferID uint64) error {
	pair := bufferPool.Get().(*[2][32 * 1024]byte)
	defer bufferPool.Put(pair)
	remaining := size
	for remaining > 0 {
		if worker.stopped(generation, transferID) {
			return ErrCanceled
		}
		count := min(int64(len(pair[0])), remaining)
		sourceRead, sourceErr := io.ReadFull(source, pair[0][:count])
		partialRead, partialErr := io.ReadFull(partial, pair[1][:count])
		if sourceRead != partialRead || !slices.Equal(pair[0][:sourceRead], pair[1][:partialRead]) {
			return ErrResumeMismatch
		}
		if sourceErr != nil || partialErr != nil {
			return errors.Join(sourceErr, partialErr)
		}
		remaining -= int64(sourceRead)
	}
	return nil
}

const (
	verifyChunk   = int64(64 << 10)
	verifyWorkers = 8
)

// verifyPrefixParallel compares a prefix in verifyChunk blocks, several blocks
// in flight, so a high latency link is no longer paid per block. It stops at
// the first mismatch, read error, pause that turns into a cancel, or cancel.
func (worker *Worker) verifyPrefixParallel(source, partial io.ReaderAt, size int64, generation, transferID uint64) error {
	chunks := (size + verifyChunk - 1) / verifyChunk
	workers := int(min(int64(verifyWorkers), chunks))
	var next atomic.Int64
	var firstErr error
	var errMu sync.Mutex
	failed := func() bool {
		errMu.Lock()
		defer errMu.Unlock()
		return firstErr != nil
	}
	fail := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			bufferSize := int(min(verifyChunk, size))
			sourceBuffer := make([]byte, bufferSize)
			partialBuffer := make([]byte, bufferSize)
			for {
				index := next.Add(1) - 1
				if index >= chunks || failed() {
					return
				}
				if !worker.holdWhilePaused(generation, transferID) {
					fail(ErrCanceled)
					return
				}
				offset := index * verifyChunk
				count := int(min(verifyChunk, size-offset))
				sourceRead, sourceErr := source.ReadAt(sourceBuffer[:count], offset)
				partialRead, partialErr := partial.ReadAt(partialBuffer[:count], offset)
				if sourceErr != nil || partialErr != nil {
					fail(errors.Join(sourceErr, partialErr))
					return
				}
				if sourceRead != count || partialRead != count || !slices.Equal(sourceBuffer[:count], partialBuffer[:count]) {
					fail(ErrResumeMismatch)
					return
				}
			}
		}()
	}
	group.Wait()
	return firstErr
}

func ensureRemoteRoot(client transferRemote, remotePath string, policy ExistingPolicy) error {
	info, exists, err := remoteLstat(client, remotePath)
	if err != nil {
		return err
	}
	if exists {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymbolicLink, remotePath)
		}
		if !info.IsDir() || policy == ExistingFail {
			return &ConflictError{Path: remotePath}
		}
		return nil
	}
	return client.Mkdir(remotePath)
}

func ensureRemoteDirectory(client transferRemote, remotePath string) error {
	info, exists, err := remoteLstat(client, remotePath)
	if err != nil {
		return err
	}
	if exists {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymbolicLink, remotePath)
		}
		if !info.IsDir() {
			return &ConflictError{Path: remotePath}
		}
		return nil
	}
	return client.Mkdir(remotePath)
}

func ensureLocalRoot(localPath string, policy ExistingPolicy) error {
	info, exists, err := localLstat(localPath)
	if err != nil {
		return err
	}
	if exists {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymbolicLink, localPath)
		}
		if !info.IsDir() || policy == ExistingFail {
			return &ConflictError{Path: localPath}
		}
		return nil
	}
	return os.MkdirAll(localPath, 0o755)
}

func ensureLocalDirectory(localPath string, mode os.FileMode) error {
	info, exists, err := localLstat(localPath)
	if err != nil {
		return err
	}
	if exists {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymbolicLink, localPath)
		}
		if !info.IsDir() {
			return &ConflictError{Path: localPath}
		}
		return nil
	}
	return os.MkdirAll(localPath, mode.Perm())
}

func commitRemote(client transferRemote, partial, final string, finalExists bool) error {
	if err := client.PosixRename(partial, final); err == nil {
		return nil
	} else if finalExists {
		return fmt.Errorf("sftp: atomic remote replacement is unavailable: %w", err)
	}
	return client.Rename(partial, final)
}

func remoteLstat(client transferRemote, remotePath string) (os.FileInfo, bool, error) {
	info, err := client.Lstat(remotePath)
	if err == nil {
		return info, true, nil
	}
	if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func localLstat(localPath string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(localPath)
	if err == nil {
		return info, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

func remotePartialPath(remotePath string) string {
	return path.Join(path.Dir(remotePath), transferPartialName(path.Base(remotePath), false))
}

func localPartialPath(localPath string) string {
	return filepath.Join(filepath.Dir(localPath), transferPartialName(filepath.Base(localPath), true))
}

func safeRemoteName(name string) bool {
	return name != "" && name != "." && name != ".." && path.Base(name) == name && !strings.ContainsRune(name, '\x00')
}

func joinRemoteWithin(root, relative string) (string, error) {
	if path.IsAbs(relative) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, relative)
	}
	cleaned := path.Clean(relative)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, relative)
	}
	joined := path.Join(root, cleaned)
	relativeToRoot, err := remoteRelative(path.Clean(root), joined)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, "../") {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, relative)
	}
	return joined, nil
}

func remoteRelative(root, target string) (string, error) {
	rootParts := strings.Split(strings.Trim(path.Clean(root), "/"), "/")
	targetParts := strings.Split(strings.Trim(path.Clean(target), "/"), "/")
	if path.IsAbs(root) != path.IsAbs(target) || len(targetParts) < len(rootParts) {
		return "..", nil
	}
	for index := range rootParts {
		if rootParts[index] != targetParts[index] {
			return "..", nil
		}
	}
	return strings.Join(targetParts[len(rootParts):], "/"), nil
}

func joinLocalWithin(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, relative)
	}
	joined := filepath.Join(root, filepath.FromSlash(relative))
	relativeToRoot, err := filepath.Rel(filepath.Clean(root), joined)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, relative)
	}
	return joined, nil
}

func pathDepth(relative string) int {
	cleaned := strings.Trim(path.Clean(relative), "/")
	if cleaned == "" || cleaned == "." {
		return 0
	}
	return strings.Count(cleaned, "/") + 1
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isUnsupportedSFTP(err error) bool {
	var status *sftp.StatusError
	return errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxOpUnsupported
}

func createDirectory(client Remote, remotePath string) error {
	if remotePath == "" {
		return fmt.Errorf("sftp: create directory requires a remote path")
	}
	creator, ok := client.(remoteDirectoryCreator)
	if !ok {
		return fmt.Errorf("sftp: create directory is not supported by the remote")
	}
	return creator.Mkdir(remotePath)
}

func rename(client Remote, oldPath, newPath string) error {
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("sftp: rename requires source and destination paths")
	}
	renamer, ok := client.(remoteRenamer)
	if !ok {
		return fmt.Errorf("sftp: rename is not supported by the remote")
	}
	return renamer.Rename(oldPath, newPath)
}

func removeFile(client Remote, remotePath string) error {
	if remotePath == "" {
		return fmt.Errorf("sftp: remove file requires a remote path")
	}
	remover, ok := client.(remoteFileRemover)
	if !ok {
		return fmt.Errorf("sftp: remove file is not supported by the remote")
	}
	return remover.Remove(remotePath)
}

func removeDirectory(client Remote, remotePath string) error {
	if remotePath == "" {
		return fmt.Errorf("sftp: remove directory requires a remote path")
	}
	remover, ok := client.(remoteDirectoryRemover)
	if !ok {
		return fmt.Errorf("sftp: remove directory is not supported by the remote")
	}
	return remover.RemoveDirectory(remotePath)
}

func chmod(client Remote, remotePath string, mode os.FileMode) error {
	if remotePath == "" {
		return fmt.Errorf("sftp: chmod requires a remote path")
	}
	changer, ok := client.(remoteChmod)
	if !ok {
		return fmt.Errorf("sftp: chmod is not supported by the remote")
	}
	return changer.Chmod(remotePath, mode)
}

func symlink(client Remote, linkPath, target string) error {
	links, ok := client.(remoteLinks)
	if !ok {
		return fmt.Errorf("sftp: symbolic links are not supported by the remote")
	}
	if target == "" || linkPath == "" {
		return fmt.Errorf("sftp: a symbolic link needs a name and a target")
	}
	return links.Symlink(target, linkPath)
}
