package sftpclient

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/vivaui/viva/leaks"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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
	// ErrUploadFinalizeTimeout reports that the server stopped answering while
	// an upload was being committed. The partial file is kept for a resume.
	ErrUploadFinalizeTimeout = errors.New("sftp: upload finalization timed out")
	// ErrInterrupted reports a transfer a lost connection stopped. Its partial
	// file is kept, and a worker opened on the same server can continue it.
	ErrInterrupted = errors.New("sftp: connection interrupted the transfer")
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
	OperationChown
	OperationOpenText
	OperationSaveText
	OperationSymlink
	OperationOpenDocument
	OperationSaveDocument
	OperationReadDocument
	OperationMoveSelection
	// OperationRemoteCopy copies files from another server into this worker's
	// server. The source is read through its own subsystem opener; the data
	// never goes through this computer's disk.
	OperationRemoteCopy
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
	// TransferInterrupted is a transfer a lost connection stopped. Its partial
	// file stays, and a worker on the same server continues it.
	TransferInterrupted
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
	// SourceModTime is the local source's modification time when this attempt
	// read it. A resume adopts the acknowledged prefix only while the source is
	// still that file: the size alone cannot tell an in-place edit apart.
	SourceModTime time.Time
	// BytesPerSecond is the recent speed of a running transfer, smoothed; zero
	// until there is something to measure and while paused.
	BytesPerSecond int64
	Resumed        int64
	Error          string
	Retryable      bool
	QueuedAt       time.Time
	// QueueOrder breaks timestamp ties across workers. A retry enters again.
	QueueOrder uint64
	StartedAt  time.Time
	FinishedAt time.Time
}

var transferQueueOrder atomic.Uint64

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
	// ListID echoes the caller's opaque listing token, including deferred links.
	// Legacy List/Prefetch calls use zero; transfer task identity is independent.
	ListID uint64
	Data   []byte // immutable bounded bytes for independent document windows
	// Background marks the result of Prefetch: a listing the caller asked for in
	// advance. It is the caller's to cache, not to show as the outcome of an
	// action, and its failure is not worth reporting.
	Background bool
	// NamesOnly marks a listing made by ListNames: owner names and link targets
	// were not resolved.
	NamesOnly   bool
	Operation   Operation
	TransferID  uint64
	Path        string
	Entries     []Entry
	LocalPath   string
	Destination string
	Mode        os.FileMode
	// Owner and Group echo the user and group an ownership change was asked
	// for, as the caller named them.
	Owner, Group string
	Bytes        int64
	Files        int
	// Removed and Failures describe a removal: how many entries left the
	// server, and how many a forced removal could not delete. A forced
	// removal keeps going after a failure, so both can be non-zero.
	Removed  int
	Failures int
	Resumed  int64
	Document TextDocument
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
	listID      uint64
	client      Remote
	directory   string
	entries     []Entry
	generation  uint64
	background  bool
	approximate bool
}

// RemoteSource opens one file subsystem on the server a remote copy reads from.
// Every attempt opens its own subsystem, so a transfer history never keeps a
// connection open. Closing it must end a read that is waiting on the network.
type RemoteSource func() (Remote, error)

// RemoteCopyRequest is one batch of source paths copied into a folder on this
// worker's server. Sources are absolute paths under SourceRoot, which names the
// folder the transfer's description speaks of.
type RemoteCopyRequest struct {
	Source      RemoteSource
	SourceRoot  string
	Sources     []string
	Destination string
	Recursive   bool
	Policy      ExistingPolicy
}

type request struct {
	sources    []string
	listID     uint64
	data       []byte
	id         uint64
	background bool // a listing nobody is waiting on: its Result says so
	namesOnly  bool // a listing for path completion: names and the directory bit
	operation  Operation
	remote     string
	// force makes a removal keep going after an entry fails and treat an
	// entry that was already gone as removed. It is the panel's forced
	// delete, not a permission bypass: the server still decides every unlink.
	force       bool
	local       string
	destination string
	mode        os.FileMode
	owner       string
	group       string
	text        string
	revision    Revision
	utf8BOM     bool
	recursive   bool
	policy      ExistingPolicy
	// limit is how many bytes a document read may return; the caller sizes it
	// from the preview setting.
	limit int
	// trusted is how many leading bytes of an interrupted upload's partial
	// file this process wrote in an earlier attempt. The worker resumes from
	// there without reading those bytes back to verify them.
	trusted int64
	// trustedTotal is the source size the trusted prefix was recorded against,
	// checked when the upload starts so a source that changed size meanwhile
	// is verified instead.
	trustedTotal int64
	// trustedModTime is the source modification time the trusted prefix was
	// recorded against. The size check cannot see an in-place edit, so the
	// time is compared as well.
	trustedModTime time.Time
	// copySource opens a subsystem on the server to read from, and
	// copySources/copyRoot name the batch copied into destination.
	copySource  RemoteSource
	copySources []string
	copyRoot    string
}

type transferRecord struct {
	request  request
	snapshot TransferSnapshot
	settled  bool
	// paused holds the transfer back; active says a goroutine is running it.
	paused, active bool
	// canceledByUser is set by CancelTransfer, the user's own decision to give
	// the transfer up. Cancel and Close end a transfer too, but they are about
	// the connection, and what the transfer left behind is kept for a retry.
	canceledByUser bool
	// interrupted is set by Interrupt, which a lost connection calls before it
	// closes the worker. The goroutine that was running the transfer finishes
	// without overwriting the state, so a new worker can continue it.
	interrupted bool
	// conflict marks a failure that needs the user's overwrite decision. Such a
	// failure is not turned into an interrupted transfer.
	conflict bool
	// source is the live subsystem of a remote copy, so a cancel can close the
	// read it is waiting on. It is nil for every other operation, and for a
	// remote copy between attempts.
	source io.Closer
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

// remoteLinker reports what a path itself is, rather than what a link at it
// points to, so a link to a folder is not emptied through the link.
type remoteLinker interface {
	Lstat(path string) (os.FileInfo, error)
}

type remoteChmod interface {
	Chmod(path string, mode os.FileMode) error
}

type remoteChown interface {
	Chown(path string, uid, gid uint32) error
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
// time, which exists only while the transfer does; an idle worker has no timer.
// The subsystem is opened only after an explicit request.
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
	// cancelGrace is how long a canceled upload may take to stop on its own
	// before the connection is dropped (uploadCancelGrace).
	cancelGrace time.Duration
	// finalizeTimeout bounds each request that commits an upload: flush, close,
	// chmod, times and rename. A server that never answers one of them must not
	// leave the transfer at "finishing" forever.
	finalizeTimeout time.Duration
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
		cancelGrace: uploadCancelGrace, finalizeTimeout: uploadFinalizeTimeout,
	}
	liveWorkers.Acquire()
	go worker.run()
	return worker
}

// liveWorkers counts workers that were created and not yet closed.
var liveWorkers = leaks.New("sftp workers")

func (worker *Worker) List(remotePath string) bool {
	return worker.ListWithID(remotePath, 0)
}

// ListNames asks for a directory listing for path completion: names and the
// directory bit only. Owner names and symbolic-link targets are not resolved,
// so the listing costs one round trip however many links the directory holds.
// Its Result says NamesOnly.
func (worker *Worker) ListNames(remotePath string) bool {
	return worker.enqueue(request{operation: OperationList, remote: remotePath, namesOnly: true})
}

func (worker *Worker) ListWithID(remotePath string, id uint64) bool {
	return worker.enqueue(request{operation: OperationList, remote: remotePath, listID: id})
}

// Prefetch lists a directory ahead of need, for a caller that keeps what it
// learns: it runs in the same order as every other request and its Result is
// marked Background.
func (worker *Worker) Prefetch(remotePath string) bool {
	return worker.PrefetchWithID(remotePath, 0)
}

func (worker *Worker) PrefetchWithID(remotePath string, id uint64) bool {
	return worker.enqueue(request{operation: OperationList, remote: remotePath, background: true, listID: id})
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

// QueueUploadResume continues an upload over the partial file an interrupted
// attempt left. trusted is the length of the prefix that attempt wrote and had
// acknowledged on the server; those bytes are already on the server and are not
// read back to be compared, so a resume starts sending at once. trustedTotal is
// the source size that prefix was recorded against, and sourceModTime the
// source's modification time; if the source is no longer that file the upload
// verifies the partial instead.
func (worker *Worker) QueueUploadResume(localPath, remotePath string, trusted, trustedTotal int64, sourceModTime time.Time) (uint64, bool) {
	if trusted <= 0 || trustedTotal <= 0 || trusted > trustedTotal || sourceModTime.IsZero() {
		// Nothing pins the prefix to a known source, so let the upload verify
		// the partial instead of adopting it.
		return worker.QueueUpload(localPath, remotePath, false, ExistingResume)
	}
	return worker.enqueueTransfer(request{
		operation: OperationUpload, local: localPath, remote: remotePath,
		policy: ExistingResume, trusted: trusted, trustedTotal: trustedTotal,
		trustedModTime: sourceModTime,
	})
}

// QueueRemoteCopy queues one batch that reads files from another server and
// writes them into the destination folder of this worker's server. The data is
// relayed through memory; no local file is part of the transfer.
func (worker *Worker) QueueRemoteCopy(copy RemoteCopyRequest) (uint64, bool) {
	if copy.Source == nil || copy.SourceRoot == "" || len(copy.Sources) == 0 || copy.Destination == "" {
		return 0, false
	}
	return worker.enqueueTransfer(request{
		operation: OperationRemoteCopy, copySource: copy.Source, copyRoot: copy.SourceRoot,
		copySources: slices.Clone(copy.Sources), destination: copy.Destination,
		recursive: copy.Recursive, policy: copy.Policy,
	})
}

func (worker *Worker) CreateDirectory(remotePath string) bool {
	return worker.enqueue(request{operation: OperationCreateDirectory, remote: remotePath})
}

// CreateDirectories creates a matching navigation path, including missing
// parents. It is queued only after the application confirms that creation.
func (worker *Worker) CreateDirectories(remotePath string) bool {
	return worker.enqueue(request{operation: OperationCreateDirectory, remote: remotePath, recursive: true})
}

func (worker *Worker) Rename(oldPath, newPath string) bool {
	return worker.enqueue(request{operation: OperationRename, remote: oldPath, destination: newPath})
}

// MoveSelection moves each source into the destination folder using Rename.
func (worker *Worker) MoveSelection(sources []string, folder string) bool {
	return worker.enqueue(request{operation: OperationMoveSelection, sources: slices.Clone(sources), destination: folder})
}

func (worker *Worker) RemoveFile(remotePath string) bool {
	return worker.enqueue(request{operation: OperationRemoveFile, remote: remotePath})
}

// RemoveFileForce removes one file with the semantics of rm -f: a file that is
// already gone is not an error. Other failures are still reported.
func (worker *Worker) RemoveFileForce(remotePath string) bool {
	return worker.enqueue(request{operation: OperationRemoveFile, remote: remotePath, force: true})
}

func (worker *Worker) RemoveDirectory(remotePath string) bool {
	return worker.enqueue(request{operation: OperationRemoveDirectory, remote: remotePath})
}

// RemoveDirectoryForce removes a folder with the semantics of rm -rf: the walk
// keeps going after an entry fails, and entries that are already gone are not
// errors. The result says how many entries were removed and how many failed.
func (worker *Worker) RemoveDirectoryForce(remotePath string) bool {
	return worker.enqueue(request{operation: OperationRemoveDirectory, remote: remotePath, force: true})
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

// Chown changes a remote path's owner and group. owner and group are names the
// server's account files list or numeric ids; names are resolved on the worker,
// where reading those files costs no round trip the caller has to wait for.
func (worker *Worker) Chown(remotePath, owner, group string) bool {
	return worker.enqueue(request{operation: OperationChown, remote: remotePath, owner: owner, group: group})
}

// OpenText reads one explicitly selected remote file into the bounded UTF-8
// editor path. It does not start work until called.
func (worker *Worker) OpenText(remotePath string) bool {
	return worker.enqueue(request{operation: OperationOpenText, remote: remotePath})
}

// OpenDocument reads bounded raw bytes without treating an image as UTF-8.
// limit is the caller's bound on the document.
func (worker *Worker) OpenDocument(remotePath string, limit int) bool {
	return worker.enqueue(request{operation: OperationOpenDocument, remote: remotePath, limit: limit})
}

// ReadDocument reads the current bounded text bytes for comparison without
// replacing the editor's revision or content. The caller remains responsible
// for decoding and for presenting the result.
func (worker *Worker) ReadDocument(remotePath string) bool {
	return worker.enqueue(request{operation: OperationReadDocument, remote: remotePath})
}

// SaveDocument replaces a revision-checked document without reinterpreting its
// bytes. Encoding and image/text policy belong to the document editor; limit is
// the bound its window was opened under, checked when the server copy is read
// back for the revision comparison.
func (worker *Worker) SaveDocument(remotePath string, data []byte, revision Revision, limit int) bool {
	return worker.enqueue(request{operation: OperationSaveDocument, remote: remotePath, data: append([]byte(nil), data...), revision: revision, limit: limit})
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
	switch next.operation {
	case OperationUpload:
		source, destination = next.local, next.remote
	case OperationRemoteCopy:
		source, destination = next.copyRoot, next.destination
	}
	worker.transfers = append(worker.transfers, transferRecord{
		request: next,
		snapshot: TransferSnapshot{
			ID: next.id, Operation: next.operation, Source: source,
			Destination: destination, Recursive: next.recursive, State: TransferQueued,
			QueuedAt: worker.clock(), QueueOrder: transferQueueOrder.Add(1),
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

// wakeChannel returns the channel closed by the next wakeLocked. Take it before
// looking at the state it announces a change of.
func (worker *Worker) wakeChannel() <-chan struct{} {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	return worker.resume
}

// canceledByUser reports whether the user gave up transfer id with
// CancelTransfer, as opposed to the connection being discarded under it.
func (worker *Worker) canceledByUser(id uint64) bool {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	record := worker.transferLocked(id)
	return record != nil && record.canceledByUser
}

// interruptIfAlone discards the subsystem of generation when transfer id is the
// only thing using it, which is what ends requests that wait on a link that no
// longer answers, and reports whether the subsystem is gone. A subsystem other
// transfers share is left alone.
//
// The subsystem is closed on a goroutine of its own: pkg/sftp's Close waits for
// the lock a blocked write holds, so on a stalled link it does not return until
// the connection gives up, and the caller must not be held by it.
func (worker *Worker) interruptIfAlone(generation, id uint64) bool {
	worker.mu.Lock()
	if worker.generation != generation {
		worker.mu.Unlock()
		return true
	}
	for index := range worker.transfers {
		if record := &worker.transfers[index]; record.active && record.snapshot.ID != id {
			worker.mu.Unlock()
			return false
		}
	}
	worker.generation++
	client := worker.client
	worker.client = nil
	worker.mu.Unlock()
	if client != nil {
		go func() { _ = client.Close() }()
	}
	return true
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
		if record.settled && !record.active && (state == TransferCompleted || state == TransferFailed || state == TransferCanceled || state == TransferInterrupted) {
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
			if record.active || (record.snapshot.State != TransferCompleted && record.snapshot.State != TransferFailed && record.snapshot.State != TransferCanceled && record.snapshot.State != TransferInterrupted) {
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
	index := worker.transferIndexLocked(id)
	if index < 0 {
		worker.mu.Unlock()
		return false
	}
	record := &worker.transfers[index]
	if worker.closedLocked() || !record.settled || (record.snapshot.State != TransferFailed && record.snapshot.State != TransferCanceled) {
		worker.mu.Unlock()
		return false
	}
	record.request.policy = policy
	record.request.trusted = 0
	record.request.trustedTotal = 0
	record.request.trustedModTime = time.Time{}
	if policy == ExistingResume && record.request.operation == OperationUpload && !record.request.recursive {
		// The bytes this attempt acknowledged are on the server and came from
		// this source, so the continuation need not read them back — but only
		// while the source is still the file that was being sent. A source
		// changed since then, even to the same size, must fall back to the
		// verification the partial normally gets. The size and modification
		// time are rechecked when the upload starts, where the source is
		// already stat'd, so this does no file I/O under the lock.
		record.request.trusted = record.snapshot.BytesDone
		record.request.trustedTotal = record.snapshot.BytesTotal
		record.request.trustedModTime = record.snapshot.SourceModTime
	}
	record.snapshot.State = TransferQueued
	record.snapshot.Error = ""
	record.snapshot.FilesDone = 0
	record.snapshot.BytesDone = 0
	record.snapshot.Resumed = 0
	record.snapshot.Retryable = false
	record.snapshot.QueuedAt = worker.clock()
	record.snapshot.QueueOrder = transferQueueOrder.Add(1)
	record.snapshot.StartedAt, record.snapshot.FinishedAt = time.Time{}, time.Time{}
	record.snapshot.BytesPerSecond, record.rate = 0, 0
	record.settled = false
	record.paused = false
	record.canceledByUser = false
	record.conflict = false
	// schedule walks this slice in order. Keep a retry behind tasks that
	// were already waiting rather than letting its old slot cut the queue.
	retried := *record
	copy(worker.transfers[index:], worker.transfers[index+1:])
	worker.transfers[len(worker.transfers)-1] = retried
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
	record.canceledByUser = true
	active, operation := record.active, record.request.operation
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
	// A remote copy reads from another server: closing that subsystem is what
	// ends a read waiting on the network.
	source := worker.takeSourceLocked(id)
	worker.wakeLocked()
	worker.mu.Unlock()
	if source != nil {
		_ = source.Close()
	}
	if active && others == 0 && operation != OperationUpload && operation != OperationRemoteCopy {
		// Nothing else is using the subsystem, and a read that is waiting on the
		// network only ends when it goes. An upload is given a moment to stop on
		// its own first, because the subsystem is also what removes its partial
		// file (see uploadPipelined). A remote copy needs the same moment, for the
		// same partial file, on its destination side.
		if client := worker.invalidate(); client != nil {
			_ = client.Close()
		}
	}
	worker.changed()
	return true
}

// Interrupt marks the uploads and downloads a lost connection is about to stop
// as interrupted instead of canceled. Their partial files stay where they are,
// and a worker opened on the same server later continues them.
//
// A transfer the user gave up on is left canceled, and a failure that needs an
// overwrite decision is left failed: both are decisions, not accidents of the
// connection. Server-to-server copies are not marked: continuing one needs both
// endpoints, which is outside what a single reconnected worker can promise.
//
// Call it before Close. Close discards the subsystem, and the goroutine of a
// transfer that was running ends soon after; finishTransfer keeps the
// interrupted state rather than turning it into a cancellation.
func (worker *Worker) Interrupt() {
	worker.mu.Lock()
	now := worker.clock()
	for index := range worker.transfers {
		record := &worker.transfers[index]
		if record.canceledByUser || record.interrupted {
			continue
		}
		if operation := record.request.operation; operation != OperationUpload && operation != OperationDownload {
			continue
		}
		switch record.snapshot.State {
		case TransferQueued, TransferRunning, TransferPaused:
		case TransferFailed:
			if !record.snapshot.Retryable || record.conflict {
				continue
			}
		default:
			continue
		}
		record.interrupted = true
		record.snapshot.State = TransferInterrupted
		record.snapshot.Error = ErrInterrupted.Error()
		record.snapshot.Retryable = false
		record.snapshot.BytesPerSecond, record.rate = 0, 0
		record.snapshot.FinishedAt = now
		record.paused = false
	}
	worker.mu.Unlock()
	worker.changed()
}

// Cancel interrupts everything under way and discards the current subsystem.
func (worker *Worker) Cancel() {
	worker.mu.Lock()
	var sources []io.Closer
	for index := range worker.transfers {
		record := &worker.transfers[index]
		if record.active && record.snapshot.State != TransferCanceled && record.snapshot.State != TransferInterrupted {
			record.snapshot.State = TransferCanceled
			record.snapshot.FinishedAt = worker.clock()
			record.snapshot.Error = ErrCanceled.Error()
			record.snapshot.Retryable = true
			record.snapshot.BytesPerSecond, record.rate = 0, 0
			record.paused = false
		}
		if record.source != nil {
			sources = append(sources, record.source)
			record.source = nil
		}
	}
	worker.wakeLocked()
	worker.mu.Unlock()
	for _, source := range sources {
		_ = source.Close()
	}
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
	if index := worker.transferIndexLocked(id); index >= 0 {
		return &worker.transfers[index]
	}
	return nil
}

func (worker *Worker) transferIndexLocked(id uint64) int {
	for index := range worker.transfers {
		if worker.transfers[index].snapshot.ID == id {
			return index
		}
	}
	return -1
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
		case record.interrupted:
			// The connection ended the transfer after Interrupt already told the
			// caller to keep it; the reason it stopped here is the lost link.
			record.snapshot.State = TransferInterrupted
			record.snapshot.Error = ErrInterrupted.Error()
			record.snapshot.Retryable = false
		case errors.Is(result.Err, ErrCanceled):
			record.snapshot.State = TransferCanceled
			record.snapshot.Error = ErrCanceled.Error()
			record.snapshot.Retryable = true
		case result.Err != nil:
			record.snapshot.State = TransferFailed
			record.snapshot.Error = result.Err.Error()
			record.snapshot.Retryable = true
			record.conflict = errors.Is(result.Err, ErrDestinationExists)
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

// setSourceModTime records the local source's modification time when an upload
// starts, so a later resume can tell whether the source is still that file.
func (worker *Worker) setSourceModTime(id uint64, mod time.Time) {
	worker.mu.Lock()
	if record := worker.transferLocked(id); record != nil {
		record.snapshot.SourceModTime = mod
	}
	worker.mu.Unlock()
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
		ListID:     next.listID,
		Background: next.background,
		Operation:  next.operation, TransferID: next.id, Path: next.remote,
		LocalPath: next.local, Destination: next.destination, Mode: next.mode,
		Owner: next.owner, Group: next.group,
	}
	generation := worker.snapshot()
	client, err := worker.remote(generation)
	if err != nil {
		result.Err = err
		return result
	}
	switch next.operation {
	case OperationList:
		result.NamesOnly = next.namesOnly
		if next.namesOnly {
			result.Path, result.Entries, result.Err = worker.listNames(client, next.remote, generation)
		} else {
			result.Path, result.Entries, result.Err = worker.list(client, next.remote, generation)
		}
		result.Approximate = IsSCP(client)
		if result.Err == nil && !next.namesOnly && hasLinks(result.Entries) {
			if _, ok := client.(remoteLinks); ok {
				result.LinksPending = true
				result.links = &linkJob{
					listID: next.listID,
					client: client, directory: result.Path, entries: slices.Clone(result.Entries),
					generation: generation, background: next.background, approximate: result.Approximate,
				}
			}
		}
	case OperationDownload:
		result.Files, result.Bytes, result.Resumed, result.Err = worker.download(client, next, generation)
	case OperationUpload:
		result.Files, result.Bytes, result.Resumed, result.Err = worker.upload(client, next, generation)
	case OperationRemoteCopy:
		result.Files, result.Bytes, result.Resumed, result.Err = worker.remoteCopy(client, next, generation)
		result.Path = next.destination
	case OperationCreateDirectory:
		if next.recursive {
			result.Err = createDirectories(client, next.remote)
		} else {
			result.Err = createDirectory(client, next.remote)
		}
	case OperationRename:
		result.Err = rename(client, next.remote, next.destination)
	case OperationMoveSelection:
		var failures []error
		for _, source := range next.sources {
			if worker.canceled(generation) {
				break
			}
			destination := path.Join(next.destination, path.Base(source))
			if err := rename(client, source, destination); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", source, err))
				continue
			}
			result.Files++
		}
		result.Err = errors.Join(failures...)
	case OperationRemoveFile:
		removed, err := removeFile(client, next.remote, next.force)
		if removed {
			result.Removed = 1
		}
		if err != nil {
			result.Failures = 1
		}
		result.Err = err
	case OperationRemoveDirectory:
		removed, err := worker.removeDirectory(client, next.remote, next.force, generation)
		result.Removed = removed
		var partial *PartialRemoveError
		if errors.As(err, &partial) {
			result.Failures = partial.Failed
		} else if err != nil {
			result.Failures = 1
		}
		result.Err = err
	case OperationChmod:
		result.Err = chmod(client, next.remote, next.mode)
	case OperationChown:
		result.Err = worker.chown(client, next.remote, next.owner, next.group, generation)
	case OperationSymlink:
		result.Err = symlink(client, next.remote, next.destination)
	case OperationOpenText:
		result.Document, result.Err = worker.openText(client, next.remote, generation)
	case OperationSaveText:
		result.Document, result.Err = worker.saveText(client, next.remote, next.text, next.revision, next.utf8BOM, generation)
	case OperationOpenDocument:
		result.Data, result.Err = worker.readFileBytes(client, next.remote, generation, next.limit)
	case OperationReadDocument:
		result.Data, result.Err = worker.readFileBytes(client, next.remote, generation, MaxEditFileBytes)
	case OperationSaveDocument:
		result.Err = worker.replaceDocumentBytes(client, next.remote, next.data, next.revision, next.limit, generation)
		if result.Err == nil {
			result.Data = next.data
		}
	default:
		result.Err = fmt.Errorf("sftp: unknown operation %d", next.operation)
	}
	if worker.canceled(generation) {
		// A finalization timeout invalidates the subsystem itself; that must not
		// turn the reason the user saw into a user cancellation. An explicit
		// cancellation still wins over the timeout.
		if !errors.Is(result.Err, ErrUploadFinalizeTimeout) || worker.canceledByUser(result.TransferID) {
			result.Err = ErrCanceled
		}
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

// maxNameEntries bounds a names-only listing. Completion holds a bounded set of
// rows, and a directory past this is not worth the memory.
const maxNameEntries = 2048

// listNames is list for path completion. It reads the directory once and keeps
// only what a candidate row needs: a name and whether it is a folder. A
// symbolic link is not followed, so it is offered as a name without a trailing
// slash; opening it after typing the slash still works.
func (worker *Worker) listNames(client Remote, remotePath string, generation uint64) (string, []Entry, error) {
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
	if len(entries) > maxNameEntries {
		entries = entries[:maxNameEntries]
	}
	for index := range entries {
		entries[index].UID, entries[index].GID, entries[index].HasOwner = 0, 0, false
		entries[index].Owner, entries[index].Group = "", ""
		entries[index].LinkTarget, entries[index].LinkDir, entries[index].LinkBroken = "", false, false
	}
	return remotePath, entries, nil
}

// describeOwners fills in the names of the owners and groups a listing carries
// as numbers. It reads the account files once, on the first listing.
func (worker *Worker) describeOwners(client Remote, entries []Entry) {
	ids := worker.idNames(client)
	for index := range entries {
		if entry := &entries[index]; entry.HasOwner {
			entry.Owner, entry.Group = ids.Users[entry.UID], ids.Groups[entry.GID]
		}
	}
}

// Accounts returns the user and group names the server's account files gave,
// for a picker that offers them. It is the same snapshot the listing used for
// displaying owners; the maps are read-only and empty until a listing read the
// files.
func (worker *Worker) Accounts() IDNames {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	return worker.ids
}

// idNames returns the account files' user and group names, reading them on
// first use. A server that does not expose them leaves the maps empty, and
// numeric ids keep working.
func (worker *Worker) idNames(client Remote) IDNames {
	worker.mu.Lock()
	tried, ids := worker.idsTried, worker.ids
	worker.mu.Unlock()
	if tried {
		return ids
	}
	ids = loadIDNames(client)
	worker.mu.Lock()
	worker.ids, worker.idsTried = ids, true
	worker.mu.Unlock()
	return ids
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
	// Resolving a link can reveal that it points at a folder. The listing's
	// order puts folders first, so the follow-up has to be ordered again now
	// that those links are known.
	sortEntries(job.entries)
	worker.publish(Result{
		ListID:     job.listID,
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
	sortEntries(entries)
	return entries
}

// sortEntries orders a listing the way the file panel shows it: folders first,
// then by name. A symbolic link counts as a folder once describeLinks found
// that its target is one, so the follow-up that carries the resolved links is
// ordered again.
func sortEntries(entries []Entry) {
	slices.SortFunc(entries, func(left, right Entry) int {
		if left.IsDir() != right.IsDir() {
			if left.IsDir() {
				return -1
			}
			return 1
		}
		return strings.Compare(left.Name, right.Name)
	})
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
	info, plan, err := planLocal(next.local, next.recursive)
	if err != nil {
		return 0, 0, 0, err
	}
	totalFiles, totalBytes := planTotals(plan)
	worker.setTotals(next.id, totalFiles, totalBytes)
	if !info.IsDir() {
		worker.setSourceModTime(next.id, info.ModTime())
		trusted := next.trusted
		if trusted > 0 && next.trustedTotal > 0 &&
			(info.Size() != next.trustedTotal || !info.ModTime().Equal(next.trustedModTime)) {
			// The source is not the file the trusted prefix was measured on,
			// so the partial is verified rather than adopted.
			trusted = 0
		}
		sink := newFileSink(worker, next.id, 0, 0)
		bytes, resumed, err = worker.uploadFile(advanced, next.local, next.remote, info, next.policy, generation, next.id, trusted, sink)
		if err == nil {
			files = 1
		}
		return files, bytes, resumed, err
	}
	if err := ensureRemoteRoot(advanced, next.remote, next.policy); err != nil {
		return 0, 0, 0, err
	}
	// Directories first, in the plan's order (a parent precedes its children),
	// so the file workers that follow always find theirs.
	filesPlan := make([]plannedUpload, 0, len(plan))
	for _, item := range plan {
		if worker.stopped(generation, next.id) {
			return 0, 0, 0, ErrCanceled
		}
		remotePath, err := joinRemoteWithin(next.remote, item.relative)
		if err != nil {
			return 0, 0, 0, err
		}
		if item.directory {
			if err := ensureRemoteDirectory(advanced, remotePath); err != nil {
				return 0, 0, 0, err
			}
			continue
		}
		if item.link != "" {
			if err := putRemoteLink(advanced, remotePath, item.link, next.policy); err != nil {
				return 0, 0, 0, err
			}
			continue
		}
		filesPlan = append(filesPlan, plannedUpload{local: item.local, remote: remotePath, info: item.info})
	}
	if len(filesPlan) == 0 {
		return 0, 0, 0, nil
	}
	progress := &transferProgress{worker: worker, id: next.id}
	if err := worker.uploadPlanned(advanced, next, generation, filesPlan, progress); err != nil {
		files, bytes, resumed = progress.counts()
		return files, bytes, resumed, err
	}
	files, bytes, resumed = progress.counts()
	return files, bytes, resumed, nil
}

// plannedUpload is one file of a directory upload, with the remote path its
// plan already validated.
type plannedUpload struct {
	local  string
	remote string
	info   os.FileInfo
}

// uploadFilesInFlight is how many files of one directory transfer are uploaded
// at once. A small file costs several round trips (open, write, close, chmod,
// rename) that its own sliding window cannot overlap with the next file's, so
// without this a folder of KB-sized files is paced by one file at a time. KB
// files hold one chunk each; the window bounds the cost of a folder of large
// files at this many times uploadWindow.
const uploadFilesInFlight = 8

// uploadPlanned uploads a directory's files with a few in flight. The first
// failure stops new files from starting; files already in flight finish and
// keep the partials their policy allows, and their bytes stay counted.
func (worker *Worker) uploadPlanned(client transferRemote, next request, generation uint64, files []plannedUpload, progress *transferProgress) error {
	workers := min(uploadFilesInFlight, len(files))
	tasks := make(chan plannedUpload)
	var (
		group sync.WaitGroup
		stop  = make(chan struct{})
		once  sync.Once
		fail  error
	)
	stopWith := func(err error) {
		once.Do(func() {
			fail = err
			close(stop)
		})
	}
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for task := range tasks {
				if worker.stopped(generation, next.id) {
					stopWith(ErrCanceled)
					return
				}
				if _, _, err := worker.uploadFile(client, task.local, task.remote, task.info, next.policy, generation, next.id, 0, progress); err != nil {
					stopWith(err)
					return
				}
				progress.fileFinished()
			}
		}()
	}
sending:
	for _, task := range files {
		select {
		case <-stop:
			break sending
		case tasks <- task:
		}
	}
	close(tasks)
	group.Wait()
	return fail
}

// transferProgress counts one directory transfer from the several file uploads
// that move it. Every update is an increment under the lock, so the snapshot it
// reports never goes backwards when files finish out of order.
type transferProgress struct {
	worker *Worker
	id     uint64

	mu      sync.Mutex
	files   int
	bytes   int64
	resumed int64
}

func (progress *transferProgress) addResumed(n int64) {
	progress.add(n, true)
}

func (progress *transferProgress) addBytes(n int64) {
	progress.add(n, false)
}

func (progress *transferProgress) add(n int64, resumed bool) {
	if n == 0 {
		return
	}
	progress.mu.Lock()
	defer progress.mu.Unlock()
	progress.bytes += n
	if resumed {
		progress.resumed += n
	}
	progress.reportLocked()
}

func (progress *transferProgress) fileFinished() {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	progress.files++
	progress.reportLocked()
}

// reportLocked publishes the counters under the lock, so two files finishing
// out of order cannot leave an older snapshot showing.
func (progress *transferProgress) reportLocked() {
	if progress.id != 0 {
		progress.worker.setProgress(progress.id, progress.files, progress.bytes, progress.resumed)
	}
}

func (progress *transferProgress) counts() (files int, bytes, resumed int64) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	return progress.files, progress.bytes, progress.resumed
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

// localPlanItem is one entry a local directory upload will move, with the
// local path and the slash-separated path it takes below the transfer root. A
// directory's plan is in walk order, so a parent precedes its children. A
// symbolic link inside the directory carries its target text in link.
type localPlanItem struct {
	local     string
	relative  string
	info      os.FileInfo
	directory bool
	link      string
}

// planLocal validates a local source and lists what an upload moves. A single
// file is a plan of one item; a directory holds every entry below it but the
// root itself.
func planLocal(localPath string, recursive bool) (os.FileInfo, []localPlanItem, error) {
	info, err := os.Lstat(localPath)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrSymbolicLink, localPath)
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("sftp: unsupported local file type: %s", localPath)
		}
		return info, []localPlanItem{{local: localPath, relative: ".", info: info}}, nil
	}
	if !recursive {
		return nil, nil, fmt.Errorf("sftp: directory upload requires recursive mode")
	}
	root := filepath.Clean(localPath)
	var plan []localPlanItem
	items := 0
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
		relative = filepath.ToSlash(relative)
		if depth := pathDepth(relative); depth > maxTransferDepth {
			return fmt.Errorf("sftp: directory exceeds transfer depth %d", maxTransferDepth)
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			// WalkDir does not follow it; the link travels as the text it holds.
			target, linkErr := os.Readlink(current)
			if linkErr != nil {
				return linkErr
			}
			plan = append(plan, localPlanItem{local: current, relative: relative, info: entryInfo, link: target})
			return nil
		}
		if !entryInfo.IsDir() && !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("sftp: unsupported local file type: %s", current)
		}
		if relative == "." {
			return nil
		}
		plan = append(plan, localPlanItem{local: current, relative: relative, info: entryInfo, directory: entryInfo.IsDir()})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return info, plan, nil
}

func scanLocal(localPath string, recursive bool) (os.FileInfo, int, int64, error) {
	info, plan, err := planLocal(localPath, recursive)
	if err != nil {
		return info, 0, 0, err
	}
	files, bytes := planTotals(plan)
	return info, files, bytes, nil
}

// planTotals counts the files and bytes a plan moves. Directories and links
// take no bytes of their own.
func planTotals(plan []localPlanItem) (files int, bytes int64) {
	for _, item := range plan {
		if !item.directory && item.link == "" {
			files++
			bytes += item.info.Size()
		}
	}
	return files, bytes
}

func (worker *Worker) uploadFile(client transferRemote, localPath, remotePath string, sourceInfo os.FileInfo, policy ExistingPolicy, generation, transferID uint64, trusted int64, sink progressSink) (written, resumed int64, err error) {
	return worker.uploadStream(client, func() (io.ReadCloser, error) { return os.Open(localPath) }, sourceInfo, localPath, remotePath, policy, generation, transferID, trusted, sink)
}

// uploadStream writes sourceInfo's content into remotePath through a staged
// partial file, as uploadFile does for a local file. openSource runs once the
// destination rules are settled, so a conflict costs no read; a remote copy
// passes the source server's file handle here.
func (worker *Worker) uploadStream(client transferRemote, openSource func() (io.ReadCloser, error), sourceInfo os.FileInfo, sourceIdentity, remotePath string, policy ExistingPolicy, generation, transferID uint64, trusted int64, sink progressSink) (written, resumed int64, err error) {
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
			sink.addResumed(sourceInfo.Size())
			return sourceInfo.Size(), sourceInfo.Size(), nil
		}
	}
	partial := remotePartialPath(remotePath, sourceIdentity)
	partialInfo, partialExists, err := remoteLstat(client, partial)
	if err != nil {
		return 0, 0, err
	}
	if partialExists && (partialInfo.IsDir() || partialInfo.Mode()&os.ModeSymlink != 0) {
		return 0, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, partial)
	}
	source, err := openSource()
	if err != nil {
		return 0, 0, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	destination, err := client.OpenTransferFile(partial, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return 0, 0, err
	}
	closed := false
	// A partial file is only worth keeping for a resume. One the user gave up
	// on, and that they did not ask to continue, is removed; one a failure or a
	// lost connection left behind stays, as does every partial under
	// ExistingResume, however the transfer ended.
	discard := false
	defer func() {
		// An abandoned upload has requests in flight on the handle, and closing
		// it would wait for them; it goes with the connection. The same is true
		// when a finalization request timed out: the goroutine inside it still
		// owns the handle until the discarded subsystem releases it.
		if errors.Is(err, errUploadAbandoned) {
			err = ErrCanceled
			return
		}
		if closed || errors.Is(err, ErrUploadFinalizeTimeout) {
			if discard {
				worker.removeRemotePartial(client, generation, partial)
			}
			return
		}
		cleanup := func() error {
			closeErr := destination.Close()
			if discard {
				worker.removeRemotePartial(client, generation, partial)
			}
			return closeErr
		}
		if errors.Is(err, ErrCanceled) {
			// No write was in flight when the upload stopped, so it was not
			// abandoned, but the link may be the very thing that stalled: closing
			// the handle and removing the partial are requests it never answers.
			err = errors.Join(err, worker.settleCanceled(generation, transferID, cleanup))
			return
		}
		err = errors.Join(err, cleanup())
	}()
	if policy == ExistingResume && partialExists {
		if partialInfo.Size() > sourceInfo.Size() {
			return 0, 0, fmt.Errorf("%w: %s", ErrResumeMismatch, partial)
		}
		resumed = partialInfo.Size()
		if trusted <= 0 || trusted > partialInfo.Size() {
			// Nothing proves the partial continues this source: read it back
			// and compare it with the source before going on from its end.
			if err := worker.verifyPrefix(source, destination, partialInfo.Size(), generation, transferID); err != nil {
				return 0, 0, fmt.Errorf("%w: %s: %v", ErrResumeMismatch, partial, err)
			}
		} else {
			// The first trusted bytes were written and acknowledged by an
			// earlier attempt of this very upload, so they are a prefix of the
			// source already on the server. Reading them back would mean
			// downloading the whole prefix again; instead drop anything past
			// them and start the copy at trusted.
			if partialInfo.Size() > trusted {
				if err := destination.Truncate(trusted); err != nil {
					return 0, 0, err
				}
			}
			// The copy starts at trusted on both sides. A source that cannot
			// seek would be sent from its start while the writes land at
			// trusted, corrupting the result, so it must not reach here.
			sourceSeek, ok := source.(io.Seeker)
			if !ok {
				return 0, 0, fmt.Errorf("sftp: resume requires a seekable source: %s", remotePath)
			}
			if _, err := sourceSeek.Seek(trusted, io.SeekStart); err != nil {
				return 0, 0, err
			}
			if _, err := destination.Seek(trusted, io.SeekStart); err != nil {
				return 0, 0, err
			}
			resumed = trusted
		}
	} else {
		if err := destination.Truncate(0); err != nil {
			return 0, 0, err
		}
		if _, err := destination.Seek(0, io.SeekStart); err != nil {
			return 0, 0, err
		}
	}
	sink.addResumed(resumed)
	newBytes, err := worker.uploadCopyTo(destination, source, generation, transferID, resumed, sink)
	written = resumed + newBytes
	if err != nil {
		if errors.Is(err, errUploadAbandoned) {
			return written, resumed, err
		}
		if policy != ExistingResume && worker.canceledByUser(transferID) {
			discard = true
		} else {
			// Writes after the one that failed may have landed, leaving
			// zero-filled gaps that the next resume could not verify. Keep only
			// the bytes that are known to be there.
			_ = destination.Truncate(written)
		}
		return written, resumed, err
	}
	if err := worker.finalizeUpload(generation, transferID, "flush", destination.Sync); err != nil && !isUnsupportedSFTP(err) {
		return written, resumed, err
	}
	if err := worker.finalizeUpload(generation, transferID, "close", destination.Close); err != nil {
		return written, resumed, err
	}
	closed = true
	// The bytes are already on the server. Carrying the source permissions over
	// is a courtesy that servers fronting an account through a gateway often
	// refuse with a generic failure; OpenSSH's sftp does not fail a put for it
	// either. A refusal leaves the server's default mode and the upload goes
	// on; a timeout is fatal because the subsystem was discarded to end it.
	if err := worker.finalizeUpload(generation, transferID, "chmod", func() error {
		return client.Chmod(partial, sourceInfo.Mode().Perm())
	}); errors.Is(err, ErrUploadFinalizeTimeout) {
		return written, resumed, err
	}
	if timer, ok := client.(remoteChtimes); ok && worker.preserveTimes() {
		// Best effort: some servers refuse SetStat. Keep the wait bounded so a
		// server that ignores the request cannot hold up the commit; a timeout
		// is fatal because the subsystem was discarded to end the request.
		if err := worker.finalizeUpload(generation, transferID, "times", func() error {
			return timer.Chtimes(partial, sourceInfo.ModTime(), sourceInfo.ModTime())
		}); errors.Is(err, ErrUploadFinalizeTimeout) {
			return written, resumed, err
		}
	}
	if err := worker.finalizeUpload(generation, transferID, "rename", func() error {
		return commitRemote(client, partial, remotePath, finalExists)
	}); err != nil {
		return written, resumed, err
	}
	return written, resumed, nil
}

// plannedFileInfo is the metadata a remote copy's plan carries for one source
// file, in the shape uploadStream asks for.
type plannedFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (info plannedFileInfo) Name() string       { return info.name }
func (info plannedFileInfo) Size() int64        { return info.size }
func (info plannedFileInfo) Mode() os.FileMode  { return info.mode }
func (info plannedFileInfo) ModTime() time.Time { return info.modTime }
func (info plannedFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info plannedFileInfo) Sys() any           { return nil }

// closeOnce keeps a remote copy's source from being closed twice: a cancel and
// the transfer's own cleanup both end it.
type closeOnce struct{ close func() error }

func (source closeOnce) Close() error { return source.close() }

// remoteCopy reads each source from the source server's subsystem and writes it
// into the destination folder of this worker's server. The source is opened
// once per attempt; a cancel closes it so a read waiting on the network ends.
func (worker *Worker) remoteCopy(destination Remote, next request, generation uint64) (files int, bytes, resumed int64, err error) {
	client, ok := destination.(transferRemote)
	if !ok || IsSCP(destination) {
		return 0, 0, 0, fmt.Errorf("sftp: server-to-server copy requires SFTP on both servers")
	}
	if next.copySource == nil || len(next.copySources) == 0 || next.destination == "" {
		return 0, 0, 0, fmt.Errorf("sftp: server-to-server copy requires a source and a destination")
	}
	source, err := next.copySource()
	if err != nil {
		return 0, 0, 0, err
	}
	tracked := closeOnce{close: sync.OnceValue(source.Close)}
	if !worker.holdSource(next.id, tracked) {
		_ = tracked.Close()
		return 0, 0, 0, ErrCanceled
	}
	defer func() {
		worker.releaseSource(next.id)
		err = errors.Join(err, tracked.Close())
	}()
	remoteSource, ok := source.(transferRemote)
	if !ok || IsSCP(source) {
		return 0, 0, 0, fmt.Errorf("sftp: server-to-server copy requires SFTP on both servers")
	}
	type copyPlan struct {
		root  string
		items []remotePlanItem
	}
	plans := make([]copyPlan, 0, len(next.copySources))
	var totalBytes int64
	var totalFiles, totalItems int
	for _, root := range next.copySources {
		items, size, err := scanRemote(remoteSource, root, next.recursive)
		if err != nil {
			return 0, 0, 0, err
		}
		totalItems += len(items)
		if totalItems > maxTransferItems {
			return 0, 0, 0, fmt.Errorf("sftp: transfer exceeds %d items", maxTransferItems)
		}
		for _, item := range items {
			if !item.directory && item.link == "" {
				totalFiles++
			}
		}
		totalBytes += size
		plans = append(plans, copyPlan{root: root, items: items})
	}
	worker.setTotals(next.id, totalFiles, totalBytes)
	for _, plan := range plans {
		// A name a server made up must not move the target out of the folder
		// the user chose.
		name := path.Base(plan.root)
		if !safeRemoteName(name) {
			return files, bytes, resumed, fmt.Errorf("%w: %q", ErrPathTraversal, name)
		}
		target := path.Join(next.destination, name)
		if plan.items[0].directory {
			if err := ensureRemoteRoot(client, target, next.policy); err != nil {
				return files, bytes, resumed, err
			}
		}
		for _, item := range plan.items {
			if worker.stopped(generation, next.id) {
				return files, bytes, resumed, ErrCanceled
			}
			targetPath, err := joinRemoteWithin(target, item.relative)
			if err != nil {
				return files, bytes, resumed, err
			}
			if item.directory {
				if item.relative != "." {
					if err := ensureRemoteDirectory(client, targetPath); err != nil {
						return files, bytes, resumed, err
					}
				}
				continue
			}
			if item.link != "" {
				if err := putRemoteLink(client, targetPath, item.link, next.policy); err != nil {
					return files, bytes, resumed, err
				}
				continue
			}
			sourcePath := item.remote
			open := func() (io.ReadCloser, error) {
				return remoteSource.OpenTransferFile(sourcePath, os.O_RDONLY)
			}
			info := plannedFileInfo{name: path.Base(sourcePath), size: item.size, mode: item.mode, modTime: item.modTime}
			sink := newFileSink(worker, next.id, files, bytes)
			written, continued, copyErr := worker.uploadStream(client, open, info, sourcePath, targetPath, next.policy, generation, next.id, 0, sink)
			bytes += written
			resumed += continued
			if copyErr != nil {
				if worker.canceledByUser(next.id) {
					copyErr = ErrCanceled
				}
				return files, bytes, resumed, copyErr
			}
			files++
			worker.setProgress(next.id, files, bytes, resumed)
		}
	}
	if worker.stopped(generation, next.id) {
		return files, bytes, resumed, ErrCanceled
	}
	return files, bytes, resumed, nil
}

// holdSource puts a remote copy's live source where a cancel can close it. It
// reports false when the transfer was already given up, so no read starts.
func (worker *Worker) holdSource(id uint64, source io.Closer) bool {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	record := worker.transferLocked(id)
	if record == nil || record.settled || record.snapshot.State == TransferCanceled || worker.closedLocked() {
		return false
	}
	record.source = source
	return true
}

// releaseSource forgets the source of a remote copy that is finishing itself.
func (worker *Worker) releaseSource(id uint64) {
	worker.mu.Lock()
	if record := worker.transferLocked(id); record != nil {
		record.source = nil
	}
	worker.mu.Unlock()
}

// takeSourceLocked removes and returns the live source of transfer id. The
// caller closes it outside the lock.
func (worker *Worker) takeSourceLocked(id uint64) io.Closer {
	record := worker.transferLocked(id)
	if record == nil {
		return nil
	}
	source := record.source
	record.source = nil
	return source
}

// settleCanceled runs the requests that tidy up after a canceled transfer, and
// gives them cancelGrace to answer. The user has already given up, so a link that
// does not answer must not hold the transfer: when the upload is the only user of
// the subsystem it is discarded, which is what ends a request that cannot return,
// and the cleanup is left to finish or fail with the connection.
func (worker *Worker) settleCanceled(generation, transferID uint64, action func() error) error {
	done := make(chan error, 1)
	go func() { done <- action() }()
	timer := time.NewTimer(worker.cancelGrace)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		worker.interruptIfAlone(generation, transferID)
		return nil
	}
}

// finalizeUpload bounds one request of the upload commit. pkg/sftp waits for a
// reply without a deadline, so an extension the server silently ignores would
// otherwise keep the copy at "finishing" for the life of the connection. When
// the upload is the only user of the subsystem it is discarded so the blocked
// request can end; a shared subsystem is left to the other transfers.
func (worker *Worker) finalizeUpload(generation, transferID uint64, stage string, action func() error) error {
	timeout := worker.finalizeTimeout
	if timeout <= 0 {
		timeout = uploadFinalizeTimeout
	}
	done := make(chan error, 1)
	go func() { done <- action() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		worker.interruptIfAlone(generation, transferID)
		return fmt.Errorf("%w: %s did not answer within %s", ErrUploadFinalizeTimeout, stage, timeout)
	}
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
		if !plan[index].directory && plan[index].link == "" {
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
		if item.link != "" {
			if err := putLocalLink(localPath, item.link, next.policy); err != nil {
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
	// link is the target text of a symbolic link inside the folder; the link
	// is recreated at the other end and never followed.
	link string
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
				links, ok := client.(remoteLinkStore)
				if !ok {
					return fmt.Errorf("%w: %s", ErrSymbolicLink, path.Join(current, name))
				}
				childRemote, err := joinRemoteWithin(root, path.Join(relative, name))
				if err != nil {
					return err
				}
				target, err := links.ReadLink(childRemote)
				if err != nil {
					return err
				}
				plan = append(plan, remotePlanItem{remote: childRemote, relative: path.Join(relative, name), mode: child.Mode(), modTime: child.ModTime(), link: target})
				if len(plan) > maxTransferItems {
					return fmt.Errorf("sftp: directory exceeds %d transfer items", maxTransferItems)
				}
				continue
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

// uploadChunkSize is the payload of one SFTP write: the packet size pkg/sftp
// uses unless told otherwise, so a chunk is exactly one request.
const uploadChunkSize = 32 * 1024

// uploadWindow is how many chunks one file keeps unacknowledged: 2 MiB, the
// receive window an SSH channel starts with. More could not be sent before the
// first acknowledgements return anyway, and the window is what bounds the
// memory of an upload.
const uploadWindow = 64

// uploadCancelGrace is how long a canceled upload gets to finish its writes in
// flight before the connection is dropped under them. Writes that are only
// waiting for acknowledgements end in a round trip, and ending them leaves the
// subsystem usable to remove the partial file; writes on a link that no longer
// answers would hold the transfer for as long as the link takes to give up.
const uploadCancelGrace = 3 * time.Second

// uploadFinalizeTimeout bounds every request that turns a completed partial
// file into the final one. The requests carry tiny payloads after the data was
// acknowledged, so a server that needs longer than this is not making progress;
// without the bound an unadvertised or ignored rename extension holds the
// transfer at "finishing" indefinitely.
const uploadFinalizeTimeout = 30 * time.Second

const posixRenameExtension = "posix-rename@openssh.com"

// errUploadAbandoned accompanies ErrCanceled when an upload was left with
// writes still in flight: the file handle cannot be used or closed safely.
var errUploadAbandoned = errors.New("sftp: upload left with requests in flight")

var uploadChunkPool = sync.Pool{New: func() any { return new([uploadChunkSize]byte) }}

// uploadChunk travels from the reader to a writer as a request and back as its
// acknowledgement.
type uploadChunk struct {
	seq    int64
	offset int64
	buffer *[uploadChunkSize]byte
	n      int
	err    error
	landed bool
}

// uploadPipeline is one file's upload: a reader cuts the source into numbered
// chunks, up to uploadWindow writers send them to explicit offsets, and the
// goroutine that owns the pipeline counts acknowledgements in order.
type uploadPipeline struct {
	worker      *Worker
	destination io.WriterAt
	source      io.Reader
	generation  uint64
	transferID  uint64
	resumed     int64

	tasks chan uploadChunk
	acks  chan uploadChunk
	// free holds buffers whose chunk was acknowledged in order. A buffer comes
	// back only then, so no more than uploadWindow chunks are ever between
	// being read and being counted, which is also what lets run keep their
	// results in a ring of uploadWindow slots.
	free chan *[uploadChunkSize]byte
	stop chan struct{}
	once sync.Once

	writers    sync.WaitGroup
	readerDone chan struct{}

	// The reader owns these until readerDone is closed.
	dispatched int64
	atEOF      bool
	readErr    error
	buffers    [uploadWindow]*[uploadChunkSize]byte
	taken      int
}

func (pipeline *uploadPipeline) halt() { pipeline.once.Do(func() { close(pipeline.stop) }) }

// acquire returns a buffer to read into, or nil once the pipeline has stopped.
// The pool is only asked for as many buffers as the window ever uses, so a small
// file does not borrow 2 MiB.
func (pipeline *uploadPipeline) acquire() *[uploadChunkSize]byte {
	select {
	case buffer := <-pipeline.free:
		return buffer
	default:
	}
	if pipeline.taken < uploadWindow {
		buffer := uploadChunkPool.Get().(*[uploadChunkSize]byte)
		pipeline.buffers[pipeline.taken] = buffer
		pipeline.taken++
		return buffer
	}
	select {
	case buffer := <-pipeline.free:
		return buffer
	case <-pipeline.stop:
		return nil
	}
}

func (pipeline *uploadPipeline) read() {
	defer close(pipeline.readerDone)
	defer close(pipeline.tasks)
	offset := pipeline.resumed
	for seq := int64(0); ; seq++ {
		select {
		case <-pipeline.stop:
			return
		default:
		}
		buffer := pipeline.acquire()
		if buffer == nil {
			return
		}
		worker := pipeline.worker
		if pipeline.transferID != 0 {
			if !worker.holdWhilePaused(pipeline.generation, pipeline.transferID) {
				pipeline.readErr = ErrCanceled
				pipeline.halt()
				return
			}
		} else if worker.canceled(pipeline.generation) {
			pipeline.readErr = ErrCanceled
			pipeline.halt()
			return
		}
		n, err := io.ReadFull(pipeline.source, buffer[:])
		if n > 0 {
			if seq < uploadWindow {
				// Writers are started as the chunks need them, so a file of a
				// few chunks does not start sixty-four goroutines.
				pipeline.writers.Add(1)
				go pipeline.write()
			}
			// Never blocks: at most uploadWindow chunks are outstanding.
			pipeline.tasks <- uploadChunk{seq: seq, offset: offset, buffer: buffer, n: n}
			pipeline.dispatched++
			offset += int64(n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				pipeline.atEOF = true
			} else {
				pipeline.readErr = err
				pipeline.halt()
			}
			return
		}
	}
}

func (pipeline *uploadPipeline) write() {
	defer pipeline.writers.Done()
	for chunk := range pipeline.tasks {
		// Looked at before every request. Requests already in flight cannot be
		// called back, so after a failure some later offsets can still land;
		// uploadFile cuts the file back to what was counted.
		select {
		case <-pipeline.stop:
			return
		default:
		}
		n, err := pipeline.destination.WriteAt(chunk.buffer[:chunk.n], chunk.offset)
		if err == nil && n < chunk.n {
			err = io.ErrShortWrite
		}
		chunk.err = err
		pipeline.acks <- chunk // never blocks: see free
	}
}

// run counts acknowledgements until every goroutine of the pipeline has ended
// and returns the bytes that are acknowledged without a gap before them, which
// is all that progress, resume and the partial file's length may rely on.
func (pipeline *uploadPipeline) run(sink progressSink) (int64, error) {
	worker := pipeline.worker
	go func() {
		<-pipeline.readerDone
		pipeline.writers.Wait()
		close(pipeline.acks)
	}()

	var (
		slots     [uploadWindow]uploadChunk
		next      int64
		confirmed int64
		reported  int64
		failed    error
		aborted   bool
		abandoned bool
		grace     <-chan time.Time
		abandon   <-chan time.Time
	)
	lastNotify := worker.clock()
	wake := worker.wakeChannel()
	check := true // the channel is taken before the state is looked at
	for running := true; running; {
		if check && !aborted {
			check = false
			if worker.stopped(pipeline.generation, pipeline.transferID) {
				aborted = true
				pipeline.halt()
				timer := time.NewTimer(worker.cancelGrace)
				defer timer.Stop()
				grace = timer.C
			}
		}
		select {
		case chunk, ok := <-pipeline.acks:
			if !ok {
				running = false
				break
			}
			chunk.landed = true
			slots[chunk.seq%uploadWindow] = chunk
			if chunk.err != nil && failed == nil {
				failed = chunk.err
				pipeline.halt()
			}
			advanced := false
			for {
				slot := &slots[next%uploadWindow]
				if !slot.landed || slot.seq != next || slot.err != nil {
					break
				}
				confirmed += int64(slot.n)
				pipeline.free <- slot.buffer // never blocks: at most uploadWindow buffers exist
				slot.landed = false
				next++
				advanced = true
			}
			if advanced && pipeline.transferID != 0 {
				if now := worker.clock(); now.Sub(lastNotify) >= progressFrameInterval {
					lastNotify = now
					sink.addBytes(confirmed - reported)
					reported = confirmed
				}
			}
		case <-wake:
			wake = worker.wakeChannel()
			check = true
		case <-grace:
			grace = nil
			if worker.interruptIfAlone(pipeline.generation, pipeline.transferID) {
				// Requests that still do not return are given up on below.
				timer := time.NewTimer(worker.cancelGrace)
				defer timer.Stop()
				abandon = timer.C
			}
		case <-abandon:
			abandoned = true
			running = false
		}
	}

	if pipeline.transferID != 0 {
		sink.addBytes(confirmed - reported)
	}
	if abandoned {
		// The reader never waits on the network and has been told to stop; the
		// source is closed once this returns, so wait for it.
		<-pipeline.readerDone
		// Writers are still inside requests that cannot be called back. They end
		// with the connection; until then they own their buffers, which are
		// left to the garbage collector rather than lent to the next upload.
		return confirmed, errors.Join(ErrCanceled, errUploadAbandoned)
	}
	for _, buffer := range pipeline.buffers[:pipeline.taken] {
		uploadChunkPool.Put(buffer)
	}
	switch {
	case failed == nil && pipeline.readErr == nil && pipeline.atEOF && next == pipeline.dispatched:
		return confirmed, nil
	case aborted:
		// Write errors after the user stopped it are the connection being
		// dropped, not a finding.
		return confirmed, ErrCanceled
	case failed != nil:
		return confirmed, failed
	case pipeline.readErr != nil:
		return confirmed, pipeline.readErr
	}
	return confirmed, ErrCanceled
}

// uploadPipelined sends source to destination with a sliding window of writes
// to explicit offsets. One write at a time per file costs a round trip per
// 32 KiB, and a batch that waits for its last acknowledgement before starting
// the next leaves the link idle once per batch; here the next chunk goes out as
// soon as one of the window is acknowledged.
//
// The result counts only bytes acknowledged without a gap before them. Writes
// beyond a failed one may still have landed, leaving the remote file longer than
// that and zero-filled in between; the caller truncates it to the result.
func (worker *Worker) uploadPipelined(destination io.WriterAt, source io.Reader, generation, transferID uint64, filesDone int, bytesBefore, resumed int64) (int64, error) {
	sink := newFileSink(worker, transferID, filesDone, bytesBefore)
	sink.addResumed(resumed)
	return worker.uploadPipelinedTo(destination, source, generation, transferID, resumed, sink)
}

// uploadPipelinedTo is uploadPipelined for a copy whose progress another file
// may be reporting at the same time: the sink counts the bytes this file moves
// as increments, so several files can share one transfer's counters.
func (worker *Worker) uploadPipelinedTo(destination io.WriterAt, source io.Reader, generation, transferID uint64, resumed int64, sink progressSink) (int64, error) {
	pipeline := &uploadPipeline{
		worker: worker, destination: destination, source: source,
		generation: generation, transferID: transferID, resumed: resumed,
		tasks:      make(chan uploadChunk, uploadWindow),
		acks:       make(chan uploadChunk, uploadWindow),
		free:       make(chan *[uploadChunkSize]byte, uploadWindow),
		stop:       make(chan struct{}),
		readerDone: make(chan struct{}),
	}
	go pipeline.read()
	return pipeline.run(sink)
}

// uploadCopy moves a local file to a remote writer. A destination that takes
// writes at offsets, which is every SFTP file, gets the sliding window; any other
// is written sequentially.
func (worker *Worker) uploadCopy(destination io.Writer, source io.Reader, generation, transferID uint64, filesDone int, bytesBefore, resumed int64) (int64, error) {
	sink := newFileSink(worker, transferID, filesDone, bytesBefore)
	sink.addResumed(resumed)
	return worker.uploadCopyTo(destination, source, generation, transferID, resumed, sink)
}

func (worker *Worker) uploadCopyTo(destination io.Writer, source io.Reader, generation, transferID uint64, resumed int64, sink progressSink) (int64, error) {
	if writerAt, ok := destination.(io.WriterAt); ok {
		return worker.uploadPipelinedTo(writerAt, source, generation, transferID, resumed, sink)
	}
	return worker.copyTo(destination, source, generation, transferID, sink)
}

// downloadCopy keeps pipelined reads when the source supports WriteTo. Fallback
// is only safe before the destination was tried; a local write failure must not
// be hidden by retrying from the source's already advanced offset.
func (worker *Worker) downloadCopy(destination io.Writer, source io.Reader, generation, transferID uint64, filesDone int, bytesBefore, resumed int64) (int64, error) {
	sink := newFileSink(worker, transferID, filesDone, bytesBefore)
	sink.addResumed(resumed)
	return worker.downloadCopyTo(destination, source, generation, transferID, sink)
}

func (worker *Worker) downloadCopyTo(destination io.Writer, source io.Reader, generation, transferID uint64, sink progressSink) (int64, error) {
	writerTo, ok := source.(io.WriterTo)
	if !ok {
		return worker.copyTo(destination, source, generation, transferID, sink)
	}
	progress := &copyProgress{
		worker: worker, destination: destination, generation: generation, transferID: transferID, sink: sink,
	}
	_, err := writerTo.WriteTo(progress)
	if err != nil && !progress.started {
		// A server that refuses a stat on an open file still reads fine one
		// request at a time; WriteTo gave up before moving any byte, so the
		// source is still where it started.
		return worker.copyTo(destination, source, generation, transferID, sink)
	}
	return progress.written, err
}

// progressSink receives the bytes a copy adds to its transfer: addResumed for
// bytes already on the server (a resumed prefix or a skipped file), addBytes
// for bytes just written. A copy shared with other files adds increments, so
// one transfer's counters stay right however many files report at once.
type progressSink interface {
	addResumed(n int64)
	addBytes(n int64)
}

// fileSink is the sink of a copy that owns its transfer's counters, as a single
// file or a sequential download does.
type fileSink struct {
	worker    *Worker
	id        uint64
	filesDone int
	bytes     int64
	resumed   int64
}

func newFileSink(worker *Worker, id uint64, filesDone int, bytes int64) *fileSink {
	return &fileSink{worker: worker, id: id, filesDone: filesDone, bytes: bytes}
}

func (sink *fileSink) addResumed(n int64) {
	if n == 0 {
		return
	}
	sink.bytes += n
	sink.resumed += n
	sink.report()
}

func (sink *fileSink) addBytes(n int64) {
	if n == 0 {
		return
	}
	sink.bytes += n
	sink.report()
}

func (sink *fileSink) report() {
	if sink.id != 0 {
		sink.worker.setProgress(sink.id, sink.filesDone, sink.bytes, sink.resumed)
	}
}

// copyProgress counts a pipelined download, keeps its progress fresh on the
// window, and is the point where pause and cancel stop it.
type copyProgress struct {
	worker      *Worker
	destination io.Writer
	generation  uint64
	transferID  uint64
	sink        progressSink
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
		progress.sink.addBytes(int64(written))
	}
	return written, err
}

func (worker *Worker) copy(destination io.Writer, source io.Reader, generation, transferID uint64, filesDone int, bytesBefore, resumed int64) (int64, error) {
	sink := newFileSink(worker, transferID, filesDone, bytesBefore)
	sink.addResumed(resumed)
	return worker.copyTo(destination, source, generation, transferID, sink)
}

func (worker *Worker) copyTo(destination io.Writer, source io.Reader, generation, transferID uint64, sink progressSink) (int64, error) {
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
				if transferID != 0 {
					sink.addBytes(int64(count))
				}
				if writeErr != nil {
					return written, writeErr
				}
				if count == 0 {
					return written, io.ErrShortWrite
				}
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
	if supportsPosixRename(client) {
		if err := client.PosixRename(partial, final); err == nil {
			return nil
		} else if finalExists {
			return fmt.Errorf("sftp: atomic remote replacement is unavailable: %w", err)
		}
	}
	return client.Rename(partial, final)
}

// supportsPosixRename reports whether the server announced the OpenSSH
// extension. Sending it to a server that does not announce it makes some
// servers ignore the packet instead of answering with an error, which leaves
// the upload waiting forever.
func supportsPosixRename(client transferRemote) bool {
	extended, ok := client.(interface {
		HasExtension(string) (string, bool)
	})
	if !ok {
		// Minimal test and legacy remotes do not expose the negotiated map;
		// preserve their previous behavior.
		return true
	}
	version, ok := extended.HasExtension(posixRenameExtension)
	return ok && version == "1"
}

// removeRemotePartial deletes the partial file of an upload that was given up.
// It is best effort and never opens a subsystem of its own: when the connection
// was discarded the file stays, and the goroutine that ran the transfer is not
// made to wait on a link that is already gone.
func (worker *Worker) removeRemotePartial(client transferRemote, generation uint64, partial string) {
	if worker.canceled(generation) {
		return
	}
	_ = client.Remove(partial)
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

// remotePartialPath names the partial of an upload. The source identity keeps
// two different sources that target the same remote path from adopting each
// other's partial: the destination alone cannot tell them apart.
func remotePartialPath(remotePath, sourceIdentity string) string {
	name := path.Base(remotePath)
	if sourceIdentity != "" {
		digest := sha256.Sum256([]byte(sourceIdentity))
		name = fmt.Sprintf("%x.%s", digest[:4], name)
	}
	return path.Join(path.Dir(remotePath), transferPartialName(name, false))
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
	return joinLocalWithinFor(root, relative, runtime.GOOS == "windows")
}

func joinLocalWithinFor(root, relative string, windows bool) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, relative)
	}
	joined := filepath.Join(root, localRelativePath(relative, windows))
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

const sftpStatusCodeNoSuchPath = 10

// IsPathNotFound reports a missing remote path across the error forms used by
// SFTP and SCP backends. Some SFTP servers use the v4-v6 NO_SUCH_PATH status
// even though pkg/sftp only exports the v3 NO_SUCH_FILE constant.
func IsPathNotFound(err error) bool {
	if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
		return true
	}
	var status *sftp.StatusError
	return errors.As(err, &status) &&
		(status.FxCode() == sftp.ErrSSHFxNoSuchFile || status.Code == sftpStatusCodeNoSuchPath)
}

func createDirectory(client Remote, remotePath string) error {
	if remotePath == "" {
		return fmt.Errorf("sftp: create directory requires a remote path")
	}
	creator, ok := client.(remoteDirectoryCreator)
	if !ok {
		return fmt.Errorf("sftp: create directory is not supported by the remote")
	}
	err := creator.Mkdir(remotePath)
	if err != nil {
		if linker, ok := client.(remoteLinker); ok {
			if _, statErr := linker.Lstat(remotePath); statErr == nil {
				return fmt.Errorf("%w: %s", ErrDestinationExists, remotePath)
			}
		} else if _, statErr := client.ReadDir(remotePath); statErr == nil {
			return fmt.Errorf("%w: %s", ErrDestinationExists, remotePath)
		}
	}
	return err
}

func createDirectories(client Remote, remotePath string) error {
	if remotePath == "" {
		return errors.New("sftp: create directories requires a remote path")
	}
	if creator, ok := client.(interface{ MkdirAll(string) error }); ok {
		err := creator.MkdirAll(remotePath)
		if err != nil {
			if linker, ok := client.(remoteLinker); ok {
				if _, statErr := linker.Lstat(remotePath); statErr == nil {
					return fmt.Errorf("%w: %s", ErrDestinationExists, remotePath)
				}
			}
		}
		return err
	}
	// SCP and minimal backends can still create a tree using their existing
	// directory primitives. Permission and non-directory errors are never
	// treated as missing paths, and existing entries are not replaced.
	var missing []string
	for current := path.Clean(remotePath); ; current = path.Dir(current) {
		if _, err := client.ReadDir(current); err == nil {
			break
		} else {
			if !IsPathNotFound(err) || path.Dir(current) == current {
				return err
			}
		}
		missing = append(missing, current)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := createDirectory(client, missing[i]); err != nil {
			if _, check := client.ReadDir(missing[i]); check != nil {
				return err
			}
		}
	}
	return nil
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

// PartialRemoveError says a forced removal deleted what it could, but could not
// delete every entry. Failed counts entries, not only the top-level paths the
// caller selected.
type PartialRemoveError struct {
	Failed int
	First  error
}

func (err *PartialRemoveError) Error() string {
	if err.First == nil {
		return fmt.Sprintf("sftp: %d items could not be removed", err.Failed)
	}
	return fmt.Sprintf("sftp: %d items could not be removed: %v", err.Failed, err.First)
}

func (err *PartialRemoveError) Unwrap() error { return err.First }

// errRemoveLimit stops a tree walk that is larger than the worker will delete.
// Even a forced removal must not keep walking an unbounded tree or a loop.
var errRemoveLimit = errors.New("sftp: removal tree is too large")

// IsFatalRemoveError reports whether a removal error must stop a forced queue:
// cancellation, a path that escaped the selected tree, or the work limit.
func IsFatalRemoveError(err error) bool {
	return errors.Is(err, ErrCanceled) || errors.Is(err, ErrPathTraversal) || errors.Is(err, errRemoveLimit)
}

func removeFile(client Remote, remotePath string, force bool) (bool, error) {
	if remotePath == "" {
		return false, fmt.Errorf("sftp: remove file requires a remote path")
	}
	remover, ok := client.(remoteFileRemover)
	if !ok {
		return false, fmt.Errorf("sftp: remove file is not supported by the remote")
	}
	err := remover.Remove(remotePath)
	if err == nil {
		return true, nil
	}
	// rm -f treats a path that is already gone as removed. The row still has
	// to leave the listing, and the caller has nothing left to retry.
	if force && IsPathNotFound(err) {
		return true, nil
	}
	return false, err
}

// maxRemoveDepth bounds the tree a directory removal walks. A server that
// reported a folder as its own child, or a mount that loops, would otherwise
// recurse until the stack ran out.
const maxRemoveDepth = 96

// removeDirectory deletes a folder with everything in it. SFTP's own remove is a
// plain rmdir and refuses a folder that is not empty, so the tree is walked here:
// each folder on the way down is listed, its files are removed, its folders are
// emptied the same way, and the folder itself goes last.
//
// A normal removal stops at the first failure. A forced one keeps going and
// returns a PartialRemoveError that says how many entries were left behind.
func (worker *Worker) removeDirectory(client Remote, remotePath string, force bool, generation uint64) (int, error) {
	if remotePath == "" {
		return 0, fmt.Errorf("sftp: remove directory requires a remote path")
	}
	// What was asked for may be a link to a folder: a listing of it would be the
	// listing of the folder it points at, and emptying that is not what the user
	// chose. Only the link goes. A remote that cannot tell a link from a folder,
	// or fails to say, does not get the benefit of the doubt: nothing is walked.
	linker, ok := client.(remoteLinker)
	if !ok {
		return 0, fmt.Errorf("sftp: remove directory is not supported by the remote")
	}
	info, err := linker.Lstat(remotePath)
	if err != nil {
		if force && IsPathNotFound(err) {
			return 1, nil
		}
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		removed, err := removeFile(client, remotePath, force)
		if removed {
			return 1, err
		}
		return 0, err
	}
	remover, ok := client.(remoteDirectoryRemover)
	if !ok {
		return 0, fmt.Errorf("sftp: remove directory is not supported by the remote")
	}
	items := 0
	removed, failed, firstErr := worker.removeTree(client, remotePath, remover, generation, force, 0, &items)
	if firstErr != nil && IsFatalRemoveError(firstErr) {
		return removed, firstErr
	}
	if failed > 0 && force {
		return removed, &PartialRemoveError{Failed: failed, First: firstErr}
	}
	if firstErr != nil {
		return removed, firstErr
	}
	return removed, nil
}

// removeTree empties one folder and then removes it. items counts everything
// attempted across the whole walk, so the work stays bounded even if entries
// keep failing. removed and failed count entries that left the server and
// entries that could not; a normal removal returns at the first failure.
func (worker *Worker) removeTree(client Remote, remotePath string, remover remoteDirectoryRemover, generation uint64, force bool, depth int, items *int) (removed int, failed int, firstErr error) {
	if depth > maxRemoveDepth {
		return 0, 0, fmt.Errorf("%w: %s is nested deeper than %d folders", errRemoveLimit, remotePath, maxRemoveDepth)
	}
	infos, err := client.ReadDir(remotePath)
	if err != nil {
		if IsPathNotFound(err) {
			// It vanished under us: there is nothing left to remove, and the
			// already-gone folder is one entry the removal does not have to
			// retry.
			return 1, 0, nil
		}
		// The folder cannot be read, so what is in it is unknown. Report why it
		// could not be listed, not the server's "not empty" to removing it
		// anyway, and lead with the cause so a status line that cuts the long
		// path still shows it. An empty folder that merely cannot be listed goes.
		if removeErr := remover.RemoveDirectory(remotePath); removeErr != nil {
			return 0, 1, fmt.Errorf("sftp: %w, cannot list %s", err, remotePath)
		}
		return 1, 0, nil
	}
	for _, info := range infos {
		if info == nil {
			continue
		}
		// A tree of any size is started and stopped by the user, so the walk
		// asks between entries rather than finishing a folder it no longer wants.
		if worker.canceled(generation) {
			return removed, failed, ErrCanceled
		}
		name := info.Name()
		if !safeRemoteName(name) {
			return removed, failed, fmt.Errorf("%w: %q", ErrPathTraversal, name)
		}
		*items++
		if *items > maxTransferItems {
			return removed, failed, fmt.Errorf("%w: %s holds more than %d items", errRemoveLimit, remotePath, maxTransferItems)
		}
		child := path.Join(remotePath, name)
		// A link is removed as a link, never followed: its target is elsewhere.
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			childRemoved, childFailed, err := worker.removeTree(client, child, remover, generation, force, depth+1, items)
			removed += childRemoved
			failed += childFailed
			if err != nil {
				fatal := IsFatalRemoveError(err)
				if firstErr == nil {
					firstErr = err
				}
				if !force || fatal {
					return removed, failed, err
				}
			}
			continue
		}
		childRemoved, err := removeFile(client, child, force)
		if childRemoved {
			removed++
		} else {
			failed++
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if !force {
				return removed, failed, err
			}
		}
	}
	if worker.canceled(generation) {
		return removed, failed, ErrCanceled
	}
	if err := remover.RemoveDirectory(remotePath); err != nil {
		failed++
		if firstErr == nil {
			firstErr = err
		}
		if !force {
			return removed, failed, err
		}
		return removed, failed, firstErr
	}
	removed++
	return removed, failed, nil
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

// chown resolves the user and group the caller named and changes one path's
// owner. SFTP carries ids, not names, so a name is looked up in the account
// files the listing already read; a numeric id needs no lookup.
func (worker *Worker) chown(client Remote, remotePath, owner, group string, generation uint64) error {
	if remotePath == "" {
		return fmt.Errorf("sftp: chown requires a remote path")
	}
	if owner == "" || group == "" {
		return fmt.Errorf("sftp: chown requires an owner and a group")
	}
	changer, ok := client.(remoteChown)
	if !ok {
		return fmt.Errorf("sftp: ownership is not supported by the remote")
	}
	ids := worker.idNames(client)
	uid, err := namedRequestID(ids.Users, owner, "user")
	if err != nil {
		return err
	}
	gid, err := namedRequestID(ids.Groups, group, "group")
	if err != nil {
		return err
	}
	if worker.canceled(generation) {
		return ErrCanceled
	}
	return changer.Chown(remotePath, uid, gid)
}

// namedRequestID accepts a numeric id as typed, or the name the account file
// lists for it.
func namedRequestID(names map[uint32]string, value, kind string) (uint32, error) {
	if id, err := strconv.ParseUint(value, 10, 32); err == nil {
		return uint32(id), nil
	}
	for id, name := range names {
		if name == value {
			return id, nil
		}
	}
	return 0, fmt.Errorf("sftp: the server has no %s named %q", kind, value)
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

// Removing a folder refuses to walk a remote that cannot tell a link from a
// folder, so both backends must be able to.
var (
	_ remoteLinker = remote{}
	_ remoteLinker = (*scpRemote)(nil)
)
