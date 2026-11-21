package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"motty/internal/trzsz"
	"motty/internal/zmodem"
)

// TRZSZ. The remote program (trz or tsz) announces itself and then waits, so
// the offer is shown before anything is written: the user chooses where the
// files go or which ones to send, and only then does the protocol start. From
// there it is the same transfer UI as a ZMODEM one: the progress card, the
// cancel button, and the shell held until the end. The protocol itself lives in
// internal/trzsz; everything here is the dialog's choice turned into a transfer.

// trzszEngine adapts a TRZSZ transfer to the engine the transfer card drives.
// Both protocols use one card; only the words for progress and cancellation
// differ, and those are translated here.
type trzszEngine struct{ *trzsz.Transfer }

func (engine trzszEngine) Progress() zmodem.Progress {
	progress := engine.Transfer.Progress()
	return zmodem.Progress{
		Name: progress.Name, Size: progress.Size, Done: progress.Done,
		Files: progress.Files, Sending: progress.Sending,
	}
}

func (engine trzszEngine) Err() error {
	err := engine.Transfer.Err()
	// The card knows one cancellation, whichever protocol ended the transfer.
	if errors.Is(err, trzsz.ErrCancelled) {
		return zmodem.ErrCancelled
	}
	return err
}

// chooseTrzszFilesToSend asks for the upload's source and starts the transfer.
// A plain trz offer takes files; a tree offer (trz -d) takes a folder, whose
// contents are listed here rather than by the remote.
func (state *State) chooseTrzszFilesToSend(session *Session) {
	m := state.messages.Tools
	transfer := &session.transfer
	directory := transfer.trigger.Directory()
	var paths []string
	if directory {
		chosen, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: m.TransferChooseSendFolderTitle})
		if errors.Is(err, app.ErrCanceled) || err == nil && chosen == "" {
			return
		}
		if err != nil {
			transfer.dialogError = err.Error()
			return
		}
		paths = []string{chosen}
	} else {
		chosen, err := state.options.OpenFiles(app.OpenFileOptions{Title: m.TransferChooseFilesTitle, AllowMultiple: true})
		if errors.Is(err, app.ErrCanceled) || err == nil && len(chosen) == 0 {
			return
		}
		if err != nil {
			transfer.dialogError = err.Error()
			return
		}
		paths = chosen
	}
	files, err := state.trzszSendFiles(paths, directory)
	if err != nil {
		transfer.dialogError = err.Error()
		return
	}
	engine, err := trzsz.NewSend(files, transfer.trigger)
	if err != nil {
		transfer.dialogError = err.Error()
		return
	}
	state.closeModal()
	state.startTransfer(session, trzszEngine{engine})
	state.showNextPrompt()
}

// trzszSendFiles describes what the user chose for the protocol. The remote
// never sees a path from this side, only names relative to the transfer.
func (state *State) trzszSendFiles(paths []string, directory bool) ([]trzsz.SendFile, error) {
	m := state.messages.Tools
	if directory {
		if len(paths) != 1 {
			return nil, errors.New("trzsz: one folder is sent at a time")
		}
		return trzsz.ListDirectory(paths[0])
	}
	if len(paths) > maxSendFiles {
		return nil, fmt.Errorf(m.TransferTooManyFmt, maxSendFiles)
	}
	files := make([]trzsz.SendFile, 0, len(paths))
	for index, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf(m.TransferNotFileFmt, filepath.Base(path))
		}
		// TRZSZ carries a 64-bit size, so unlike ZMODEM there is no 4 GiB cap.
		files = append(files, trzsz.SendFile{
			Path: path, Rel: []string{filepath.Base(path)}, Size: info.Size(),
			Mode: uint32(info.Mode().Perm()), ModTime: info.ModTime(), PathID: index,
		})
	}
	return files, nil
}
