package workbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
	"motty/internal/sftpclient"
)

const maxAgentTransfers = 32
const maxActiveAgentTransfers = 4

var _ gate.FileTransfers = (*agentBackend)(nil)

type agentTransfer struct {
	mu     sync.Mutex
	value  gate.Transfer
	worker *sftpclient.Worker
	cancel context.CancelFunc
	done   bool
}

// workingLocked reports whether the transfer can still touch its paths, which is
// what the overlap check and the active count ask. A terminal state answers that
// on its own, and it is published before done is set: the finishing goroutine
// writes the terminal state, then records the audit, and only then marks done. A
// caller that has already been told "completed" must not be told that the same
// path is still busy, so done alone cannot decide this.
func (task *agentTransfer) workingLocked() bool {
	if task.done {
		return false
	}
	switch task.value.State {
	case "completed", "failed", "canceled":
		return false
	}
	return true
}

type agentTransferRoot struct {
	configured string
	resolved   string
}

func validateTransferRoots(roots []string) ([]agentTransferRoot, error) {
	validated := make([]agentTransferRoot, 0, len(roots))
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("transfer root must be absolute: %s", root)
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("resolve transfer root %s: %w", root, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("transfer root is not a directory: %s", root)
		}
		validated = append(validated, agentTransferRoot{configured: filepath.Clean(root), resolved: filepath.Clean(resolved)})
	}
	return validated, nil
}

func (b *agentBackend) transferPath(local string, upload, recursive bool) (string, error) {
	if b.transferRootError != nil {
		return "", b.transferRootError
	}
	if !filepath.IsAbs(local) {
		return "", errors.New("local_path must be absolute")
	}
	local = filepath.Clean(local)
	for _, allowed := range b.transferRoots {
		relative, err := filepath.Rel(allowed.configured, local)
		if err != nil || relative != "." && !filepath.IsLocal(relative) {
			relative, err = filepath.Rel(allowed.resolved, local)
			if err != nil || relative != "." && !filepath.IsLocal(relative) {
				continue
			}
		}
		root := allowed.resolved
		local = filepath.Join(root, relative)
		current := root
		parts := []string{""}
		if relative != "." {
			parts = append(parts, strings.Split(relative, string(filepath.Separator))...)
		}
		var info os.FileInfo
		for _, part := range parts {
			current = filepath.Join(current, part)
			info, err = os.Lstat(current)
			if errors.Is(err, os.ErrNotExist) && !upload && current != root {
				return local, nil
			}
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("local symbolic links are refused: %s", current)
			}
			if current != local && !info.IsDir() {
				return "", fmt.Errorf("local parent is not a directory: %s", current)
			}
		}
		if upload && !info.Mode().IsRegular() && (!recursive || !info.IsDir()) {
			return "", errors.New("upload source must be a regular file, or a directory with recursive=true")
		}
		return local, nil
	}
	return "", errors.New("local_path is outside the configured --transfer-root directories")
}

func (b *agentBackend) StartTransfer(ctx context.Context, args tools.TransferArgs, upload bool) (gate.Transfer, error) {
	if err := ctx.Err(); err != nil {
		return gate.Transfer{}, err
	}
	if !b.allowWrites || len(b.transferRoots) == 0 {
		return gate.Transfer{}, errors.New("transfers require full access and at least one --transfer-root")
	}
	host, err := b.resolve(args.Session)
	if err != nil {
		return gate.Transfer{}, err
	}
	args.LocalPath, err = b.transferPath(args.LocalPath, upload, args.Recursive)
	if err != nil {
		return gate.Transfer{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return gate.Transfer{}, errAgentClosed
	}
	active, oldestDone := 0, -1
	for index, task := range b.transfers {
		task.mu.Lock()
		if task.workingLocked() {
			conflict := (task.value.Direction == "download" || !upload) && localTransferOverlap(task.value.LocalPath, args.LocalPath)
			if task.value.Session == host.CatalogID && (task.value.Direction == "upload" || upload) {
				conflict = conflict || remoteTransferOverlap(task.value.RemotePath, args.RemotePath)
			}
			if conflict {
				id := task.value.ID
				task.mu.Unlock()
				return gate.Transfer{}, fmt.Errorf("paths overlap active transfer %s; wait for it to settle", id)
			}
			active++
		} else if oldestDone < 0 {
			oldestDone = index
		}
		task.mu.Unlock()
	}
	if active >= maxActiveAgentTransfers {
		return gate.Transfer{}, errors.New("four transfers are already active; wait for one to settle")
	}
	if len(b.transfers) >= maxAgentTransfers {
		if oldestDone < 0 {
			return gate.Transfer{}, errors.New("transfer history is full")
		}
		b.transfers = slices.Delete(b.transfers, oldestDone, oldestDone+1)
	}
	b.nextTransfer++
	direction := "download"
	if upload {
		direction = "upload"
	}
	value := gate.Transfer{ID: b.transferPrefix + "-" + strconv.FormatUint(b.nextTransfer, 10), Session: host.CatalogID, Direction: direction, LocalPath: args.LocalPath, RemotePath: args.RemotePath, State: "queued"}
	if b.auditLog != nil {
		if err := b.auditLog.RecordAudit(store.AuditEntry{At: time.Now(), HostID: host.CatalogID, HostLabel: host.Label, Tool: "transfer_queued", Summary: value.ID + " " + direction + " " + args.LocalPath + " <-> " + args.RemotePath, Outcome: store.AuditOK}); err != nil {
			return gate.Transfer{}, fmt.Errorf("cannot audit transfer; nothing was started: %w", err)
		}
	}
	taskCtx, cancel := context.WithCancel(b.transferCtx)
	task := &agentTransfer{value: value, cancel: cancel}
	b.transfers = append(b.transfers, task)
	b.transferGroup.Add(1)
	go func() {
		defer b.transferGroup.Done()
		defer cancel()
		err := b.runTransfer(taskCtx, task, host, args, upload)
		task.mu.Lock()
		if task.value.State != "completed" {
			switch {
			case taskCtx.Err() != nil:
				task.value.State, task.value.Error = "canceled", taskCtx.Err().Error()
			case err != nil:
				task.value.State, task.value.Error = "failed", err.Error()
			}
		}
		task.value.BytesPerSecond = 0
		finished := task.value
		task.worker = nil
		task.mu.Unlock()
		if b.auditLog != nil {
			outcome := store.AuditOK
			if finished.State != "completed" {
				outcome = store.AuditFailed
			}
			if auditErr := b.auditLog.RecordAudit(store.AuditEntry{At: time.Now(), HostID: host.CatalogID, HostLabel: host.Label, Tool: "transfer_finished", Summary: finished.ID + " " + direction + " " + args.LocalPath + " <-> " + args.RemotePath + " [" + finished.State + "]", Outcome: outcome}); auditErr != nil {
				task.mu.Lock()
				task.value.Error = strings.TrimSpace(task.value.Error + " audit failed: " + auditErr.Error())
				task.mu.Unlock()
			}
		}
		task.mu.Lock()
		task.done = true
		task.mu.Unlock()
	}()
	return value, nil
}

func localTransferOverlap(left, right string) bool {
	for _, pair := range [][2]string{{left, right}, {right, left}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && (relative == "." || filepath.IsLocal(relative)) {
			return true
		}
	}
	return false
}

func remoteTransferOverlap(left, right string) bool {
	left, right = path.Clean(left), path.Clean(right)
	return left == right || strings.HasPrefix(left, strings.TrimSuffix(right, "/")+"/") || strings.HasPrefix(right, strings.TrimSuffix(left, "/")+"/")
}

func (b *agentBackend) runTransfer(ctx context.Context, task *agentTransfer, host Host, args tools.TransferArgs, upload bool) error {
	task.mu.Lock()
	task.value.State = "connecting"
	task.mu.Unlock()
	conn, err := b.dial(ctx, host)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, conn.Close)
	defer stop()
	remote, err := openRemoteFiles(realConn{conn}, host.Transfer)
	if err != nil {
		return err
	}
	protocol := "sftp"
	if sftpclient.IsSCP(remote) {
		protocol = "scp"
		if args.Existing == "resume" {
			return errors.New("SCP does not support resume; choose fail or replace explicitly")
		}
	}
	// Re-check the path once the connection is up: the local tree may have
	// changed while dialing. The result is what the worker is given, so the
	// checked path is the transferred one.
	if args.LocalPath, err = b.transferPath(args.LocalPath, upload, args.Recursive); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	worker := sftpclient.NewWorker(func() (sftpclient.Remote, error) { return remote, nil }, nil)
	worker.SetParallel(1)
	defer func() { worker.Close(); <-worker.Done() }()
	policy := sftpclient.ExistingFail
	switch args.Existing {
	case "replace":
		policy = sftpclient.ExistingReplace
	case "resume":
		policy = sftpclient.ExistingResume
	}
	var queued bool
	if upload {
		_, queued = worker.QueueUpload(args.LocalPath, args.RemotePath, args.Recursive, policy)
	} else {
		_, queued = worker.QueueDownload(args.RemotePath, args.LocalPath, args.Recursive, policy)
	}
	if !queued {
		return errors.New("transfer worker refused the task")
	}
	task.mu.Lock()
	task.worker = worker
	task.value.Protocol = protocol
	task.value.State = "running"
	task.mu.Unlock()
	var transferErr error
	select {
	case result := <-worker.Results():
		transferErr = result.Err
	case <-ctx.Done():
		worker.Close()
		<-worker.Done()
		transferErr = ctx.Err()
	}
	task.mu.Lock()
	task.updateLocked()
	task.mu.Unlock()
	return transferErr
}

func (task *agentTransfer) updateLocked() {
	if task.worker == nil {
		return
	}
	snapshots := task.worker.Transfers(nil)
	if len(snapshots) == 0 {
		return
	}
	snapshot := snapshots[0]
	task.value.BytesDone, task.value.BytesTotal = snapshot.BytesDone, snapshot.BytesTotal
	task.value.FilesDone, task.value.FilesTotal = snapshot.FilesDone, snapshot.FilesTotal
	task.value.BytesPerSecond, task.value.Error = snapshot.BytesPerSecond, snapshot.Error
	switch snapshot.State {
	case sftpclient.TransferCompleted:
		task.value.State = "completed"
	case sftpclient.TransferFailed:
		task.value.State = "failed"
	case sftpclient.TransferCanceled:
		task.value.State = "canceling"
	}
}

func (b *agentBackend) Transfers(ctx context.Context, id string) ([]gate.Transfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	values := []gate.Transfer{}
	for _, task := range b.transfers {
		task.mu.Lock()
		if id == "" || id == task.value.ID {
			task.updateLocked()
			value := task.value
			if value.CancelRequested && task.workingLocked() {
				value.State = "canceling"
			}
			values = append(values, value)
		}
		task.mu.Unlock()
	}
	if id != "" && len(values) == 0 {
		return nil, fmt.Errorf("unknown transfer %q (history is local to this MCP process)", id)
	}
	return values, nil
}

func (b *agentBackend) CancelTransfer(ctx context.Context, id string) (gate.Transfer, error) {
	if err := ctx.Err(); err != nil {
		return gate.Transfer{}, err
	}
	if !b.allowWrites {
		return gate.Transfer{}, errAgentNotOffered
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, task := range b.transfers {
		task.mu.Lock()
		if task.value.ID != id {
			task.mu.Unlock()
			continue
		}
		task.updateLocked()
		if task.workingLocked() {
			task.value.CancelRequested = true
			task.cancel()
		}
		value := task.value
		if value.CancelRequested && task.workingLocked() {
			value.State = "canceling"
		}
		task.mu.Unlock()
		return value, nil
	}
	return gate.Transfer{}, fmt.Errorf("unknown transfer %q", id)
}
