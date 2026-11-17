package workbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

type aiUploadFile struct {
	path     string
	name     string
	size     int64
	modified time.Time
	info     os.FileInfo
}

var _ gate.SelectedUploader = (*aiBackend)(nil)

func (backend *aiBackend) SelectedFileName(id string) (string, bool) {
	file, ok := backend.uploads[id]
	return file.name, ok
}

func (backend *aiBackend) UploadSelected(ctx context.Context, args tools.SelectedUploadArgs) (gate.Transfer, error) {
	file, ok := backend.uploads[args.FileID]
	if !ok {
		return gate.Transfer{}, errors.New("file was not selected for this request")
	}
	info, err := os.Lstat(file.path)
	if err != nil {
		return gate.Transfer{}, err
	}
	if file.info == nil || !info.Mode().IsRegular() || info.Size() != file.size || !info.ModTime().Equal(file.modified) || !os.SameFile(info, file.info) {
		return gate.Transfer{}, errors.New("selected local file changed; select it again")
	}
	type connection struct {
		conn     Connection
		transfer transferMode
	}
	target, err := aiCall(backend.state, ctx, func() (connection, error) {
		session, err := backend.session(args.Session)
		if err != nil {
			return connection{}, err
		}
		if !session.hasServer() || session.conn == nil || session.phase != sshconn.PhaseConnected {
			return connection{}, errors.New(errSFTPNeedsSSH)
		}
		return connection{conn: session.conn, transfer: session.host.Transfer}, nil
	})
	if err != nil {
		return gate.Transfer{}, err
	}
	policy := sftpclient.ExistingFail
	switch args.Existing {
	case "replace":
		policy = sftpclient.ExistingReplace
	case "resume":
		policy = sftpclient.ExistingResume
	}
	var worker *sftpclient.Worker
	var id uint64
	worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) {
		return openRemoteFiles(target.conn, target.transfer)
	}, func() {
		if backend.uploadRun == nil {
			return
		}
		for _, snapshot := range worker.Transfers(nil) {
			if snapshot.State != sftpclient.TransferRunning {
				continue
			}
			progress := formatBytes(snapshot.BytesDone)
			if snapshot.BytesTotal > 0 {
				progress += " / " + formatBytes(snapshot.BytesTotal)
			}
			backend.state.aiPost(func(state *State) {
				if state.ai.run != backend.uploadRun {
					return
				}
				for index := len(state.ai.items) - 1; index >= 0; index-- {
					item := &state.ai.items[index]
					if item.kind == aiTool && item.call.Name == tools.NameUploadSelected && item.status == aiToolRunning {
						item.result = progress
						break
					}
				}
			})
			break
		}
	})
	defer worker.Close()
	var queued bool
	id, queued = worker.QueueUpload(file.path, args.RemotePath, false, policy)
	if !queued {
		return gate.Transfer{}, errors.New("upload queue is unavailable")
	}
	for {
		select {
		case <-ctx.Done():
			worker.CancelTransfer(id)
			return gate.Transfer{}, ctx.Err()
		case result := <-worker.Results():
			if result.TransferID != id {
				continue
			}
			if result.Err != nil {
				return gate.Transfer{}, result.Err
			}
			return gate.Transfer{ID: strconv.FormatUint(id, 10), Session: args.Session, Direction: "upload", LocalPath: file.name, RemotePath: args.RemotePath, State: "completed", BytesDone: result.Bytes, BytesTotal: result.Bytes, FilesDone: 1, FilesTotal: 1}, nil
		}
	}
}

func (state *State) aiPickUploads() {
	if state.ai.mode != agent.ModeAgent {
		state.ai.notice = state.messages.AIPanel.UploadNeedsAgent
		return
	}
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: state.messages.AIPanel.UploadPickerTitle, AllowMultiple: true})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.ai.notice = fmt.Sprintf(state.messages.AIPanel.AttachFailedFmt, state.messages.AIPanel.Upload, err)
		return
	}
	room := state.aiAttachRoom(len(paths))
	if room == 0 {
		return
	}
	paths = append([]string(nil), paths[:room]...)
	state.ai.attaching += len(paths)
	go func() {
		loaded := make([]aiLoaded, len(paths))
		for index, path := range paths {
			loaded[index].path, loaded[index].upload = path, true
			info, err := os.Lstat(path)
			if err == nil && !info.Mode().IsRegular() {
				err = errors.New("only regular files can be uploaded")
			}
			loaded[index].err = err
			if err == nil {
				loaded[index].file.Name = filepath.Base(path)
				loaded[index].size, loaded[index].modified = info.Size(), info.ModTime()
				loaded[index].info = info
			}
		}
		state.aiPost(func(state *State) { state.aiAttached(loaded) })
	}()
}
