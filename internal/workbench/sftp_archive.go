package workbench

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

const (
	archiveMissingTool = "MOTTY_ARCHIVE_MISSING_TOOL:"
	archiveExists      = "MOTTY_ARCHIVE_DESTINATION_EXISTS"
)

type archiveResult struct {
	sequence                  uint64
	directory, source, target string
	compress                  bool
	output                    []byte
	err                       error
}

func (state *State) pumpSFTPArchive(session *Session, view *sftpView) {
	if view.archiveResults == nil {
		return
	}
	select {
	case result := <-view.archiveResults:
		view.archiveResults = nil
		state.finishSFTPArchive(session.ID, view, result.sequence, result.directory, result.source, result.target, result.compress, result.output, result.err)
	default:
	}
}

func archiveToolCheck(tool string) string {
	return "if ! command -v " + shellQuote(tool) + " >/dev/null 2>&1; then printf '%s\\n' " +
		shellQuote(archiveMissingTool+tool) + "; exit 127; fi; "
}

func archiveDestinationCheck(destination string) string {
	quoted := shellQuote(destination)
	return "if [ -e " + quoted + " ] || [ -L " + quoted + " ]; then printf '%s\\n' " +
		shellQuote(archiveExists) + "; exit 17; fi; "
}

// Non-interactive SSH sessions often start in the C locale. Info-ZIP may
// otherwise discard Unicode characters while converting archive member names.
func archiveUTF8Locale() string {
	return "if command -v locale >/dev/null 2>&1; then " +
		"for archive_locale in C.UTF-8 C.utf8 en_US.UTF-8 en_US.utf8; do " +
		"case \"$(LC_ALL=\"$archive_locale\" locale charmap 2>/dev/null)\" in " +
		"UTF-8|UTF8) export LC_ALL=\"$archive_locale\"; break;; esac; done; fi; "
}

// compressCommand runs entirely on the server. Its private staging directory
// lives beside the final archive; linking the finished file publishes it
// without ever replacing an existing file, directory or dangling symlink.
func compressCommand(source, destination string) string {
	lower := strings.ToLower(destination)
	var tools, pack, payload string
	switch {
	case strings.HasSuffix(lower, ".zip"):
		tools, payload = archiveToolCheck("zip")+archiveUTF8Locale(), "archive.zip"
		pack = "zip -qr \"$stage/" + payload + "\" -- " + shellQuote(path.Base(source))
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		tools, payload = archiveToolCheck("tar")+archiveToolCheck("gzip"), "archive.tar.gz"
		pack = "tar -czf \"$stage/" + payload + "\" -- " + shellQuote(path.Base(source))
	default:
		return ""
	}
	parent := path.Dir(destination)
	return "( " + tools + archiveToolCheck("mktemp") + archiveToolCheck("ln") +
		archiveDestinationCheck(destination) + "stage=$(mktemp -d -- " +
		shellQuote(path.Join(parent, ".motty-compress.XXXXXXXX")) + ") || exit; " +
		"trap 'rm -rf -- \"$stage\"' 0; " +
		"cd -- " + shellQuote(path.Dir(source)) + " || exit; " + pack + " || exit; " +
		"if ! ln -- \"$stage/" + payload + "\" " + shellQuote(destination) + "; then " +
		archiveDestinationCheck(destination) + "exit 1; fi ) 2>&1"
}

func (state *State) canCompressSFTP(session *Session, view *sftpView) bool {
	entry, selected := state.selectedSFTPEntry(view)
	return !view.local && selected && entry.IsDir() && !entry.IsLink() && session.host.System != sshconn.SystemWindows
}

func (state *State) openSFTPCompressDialog(view *sftpView) {
	session, ok := state.sftpSession(view)
	if !ok || !state.canCompressSFTP(session, view) {
		return
	}
	entry, _ := state.selectedSFTPEntry(view)
	view.dialogEditor, view.dialogEditor2 = text.Editor{}, text.Editor{}
	view.dialogEntry, view.archiveDirectory = entry, view.path
	view.dialogError = ""
	view.dialogEditor.Insert(entry.Name + ".tar.gz")
	view.dialog = sftpDialogCompress
}

func (state *State) submitSFTPCompress(view *sftpView) bool {
	session, ok := state.sftpSession(view)
	if !ok || session.conn == nil {
		return false
	}
	name := strings.TrimSpace(view.dialogEditor.String())
	directory := view.archiveDirectory
	if directory == "" {
		directory = view.path
	}
	command := compressCommand(path.Join(directory, view.dialogEntry.Name), path.Join(directory, name))
	if name == "" || strings.ContainsAny(name, "/\\\x00\r\n") || command == "" {
		view.dialogError = state.messages.Tools.CompressErrorName
		return false
	}
	state.startSFTPArchive(session, view, directory, view.dialogEntry.Name, name, command, true)
	return true
}

func (state *State) startSFTPArchive(session *Session, view *sftpView, directory, source, target, command string, compress bool) {
	state.closeSFTPDialog(view)
	view.busy, view.extracting = true, true
	view.operation = sftpclient.OperationList
	view.errorText, view.refreshStatus = "", ""
	view.status = fmt.Sprintf(state.messages.Tools.ExtractRunningFmt, source)
	if compress {
		view.status = fmt.Sprintf(state.messages.Tools.CompressRunningFmt, source)
	}
	view.archiveSequence++
	sequence, connection := view.archiveSequence, session.conn
	results, notify := make(chan archiveResult, 1), state.notifyForView(session, view)
	view.archiveResults = results
	ctx, cancel := context.WithTimeout(context.Background(), extractTimeout)
	view.cancelExtract = cancel
	go func() {
		defer cancel()
		output, err := connection.RunCommand(ctx, command, maxExtractOutput)
		results <- archiveResult{sequence: sequence, directory: directory, source: source, target: target, compress: compress, output: output, err: err}
		notify()
	}()
}

func (state *State) finishSFTPArchive(sessionID uint32, view *sftpView, sequence uint64, directory, source, target string, compress bool, output []byte, err error) {
	session, ok := state.sftpSession(view)
	if !ok || session.ID != sessionID || session.files != view || !view.extracting || view.archiveSequence != sequence {
		return
	}
	m := state.messages.Tools
	view.extracting, view.busy, view.cancelExtract = false, false, nil
	view.errorText = ""
	if err != nil {
		view.setItemCountStatus(state.messages.SFTPItemsFmt, len(view.active().rows))
		title, format := m.ExtractFailedTitle, m.ExtractFailedFmt
		if compress {
			title, format = m.CompressFailedTitle, m.CompressFailedFmt
		}
		if errors.Is(err, context.Canceled) {
			state.fileOpenNotice(view.detached, state.messages.SFTPCanceled, fmt.Sprintf(m.ArchiveCanceledFmt, path.Join(directory, source)), components.ToastInfo)
			return
		}
		reason := strings.TrimSpace(string(output))
		switch {
		case strings.Contains(reason, archiveMissingTool):
			_, tool, _ := strings.Cut(reason, archiveMissingTool)
			tool, _, _ = strings.Cut(tool, "\n")
			reason = fmt.Sprintf(m.ArchiveToolMissingFmt, strings.TrimSpace(tool))
		case strings.Contains(reason, archiveExists):
			reason = fmt.Sprintf(m.ArchiveDestinationExistsFmt, path.Join(directory, target))
		case reason == "":
			reason = err.Error()
		}
		state.fileOpenPathReason(view.detached, title, format, path.Join(directory, source), firstLines(reason, 3), components.ToastError)
		return
	}
	view.forgetListings()
	view.refreshStatus = fmt.Sprintf(m.ExtractDoneFmt, source, target)
	if compress {
		view.refreshStatus = fmt.Sprintf(m.CompressDoneFmt, source, target)
	}
	if view.path == directory {
		state.listSFTP(view, directory)
	} else {
		view.status = view.refreshStatus
		view.refreshStatus = ""
	}
}
