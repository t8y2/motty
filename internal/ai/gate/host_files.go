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

type FileInfo struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Mode        string `json:"mode"`
	ModifiedAt  string `json:"modified_at"`
	Directory   bool   `json:"directory"`
	Symlink     bool   `json:"symlink"`
	Approximate bool   `json:"approximate,omitempty"`
}

type FilePage struct {
	Entries     []FileInfo `json:"entries"`
	Total       int        `json:"total"`
	NextOffset  int        `json:"next_offset"`
	HasMore     bool       `json:"has_more"`
	Approximate bool       `json:"approximate"`
}

type FileRange struct {
	Data       string `json:"data"`
	Encoding   string `json:"encoding"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Size       int64  `json:"size"`
	BytesRead  int    `json:"bytes_read"`
	EOF        bool   `json:"eof"`
	Truncated  bool   `json:"truncated"`
}

type FileChecksum struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
	Bytes     int64  `json:"bytes"`
}

type FileReader interface {
	ListFiles(context.Context, tools.FileListArgs) (FilePage, error)
	ReadRange(context.Context, tools.FileReadArgs) (FileRange, error)
	StatFile(context.Context, tools.SFTPListArgs) (FileInfo, error)
	ChecksumFile(context.Context, tools.FileChecksumArgs) (FileChecksum, error)
}

type Transfer struct {
	ID              string `json:"transfer_id"`
	Session         string `json:"session"`
	Direction       string `json:"direction"`
	LocalPath       string `json:"local_path"`
	RemotePath      string `json:"remote_path"`
	State           string `json:"state"`
	Protocol        string `json:"protocol,omitempty"`
	BytesDone       int64  `json:"bytes_done"`
	BytesTotal      int64  `json:"bytes_total"`
	FilesDone       int    `json:"files_done"`
	FilesTotal      int    `json:"files_total"`
	BytesPerSecond  int64  `json:"bytes_per_second"`
	Error           string `json:"error,omitempty"`
	CancelRequested bool   `json:"cancel_requested"`
}

type FileTransfers interface {
	StartTransfer(context.Context, tools.TransferArgs, bool) (Transfer, error)
	Transfers(context.Context, string) ([]Transfer, error)
	CancelTransfer(context.Context, string) (Transfer, error)
}

type SelectedUploader interface {
	UploadSelected(context.Context, tools.SelectedUploadArgs) (Transfer, error)
	SelectedFileName(string) (string, bool)
}

type CommandResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   *int   `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
	TimedOut   bool   `json:"timed_out"`
	Error      string `json:"error,omitempty"`
	Output     string `json:"-"`
}

type HostCommands interface {
	ExecCommand(context.Context, tools.HostExecArgs) (CommandResult, error)
}

func structured(value any, text string) Result {
	data, err := json.Marshal(value)
	if err != nil {
		return failure(fmt.Errorf("encode tool result: %w", err))
	}
	if text == "" {
		text = string(data)
	}
	return Result{Text: text, Structured: data}
}

func (g Gate) runHostTool(ctx context.Context, name string, raw []byte) (Result, bool) {
	if name == tools.NameUploadSelected {
		backend, ok := g.Backend.(SelectedUploader)
		if !ok || g.Target == nil {
			return failure(errors.New("selected-file uploads are unavailable")), true
		}
		args, err := tools.DecodeSelectedUpload(raw)
		if err != nil {
			return failure(err), true
		}
		filename, ok := backend.SelectedFileName(args.FileID)
		if !ok {
			return failure(errors.New("file was not selected for this request")), true
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err), true
		}
		assessment := risk.Assessment{Level: risk.High, Reasons: []string{"uploads a local file to an SSH host"}}
		if g.Approver == nil {
			return failure(errors.New("an explicit upload approval is required")), true
		}
		approved, _, err := g.Approver(ctx, Request{Tool: name, Kind: tools.Write, Session: args.Session, SessionLabel: g.sessionLabel(args.Session), Summary: "upload " + filename + " -> " + args.RemotePath, Assessment: assessment})
		if err != nil {
			return failure(err), true
		}
		if !approved {
			return Result{Text: "the user declined this upload", IsError: true, Denied: true}, true
		}
		result, err := backend.UploadSelected(ctx, args)
		if err != nil {
			return failure(err), true
		}
		return structured(result, ""), true
	}
	if name == tools.NameTerminalExec {
		backend, ok := g.Backend.(HostCommands)
		if !ok {
			return Result{}, false
		}
		args, err := tools.DecodeHostExec(raw)
		if err != nil {
			return failure(err), true
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err), true
		}
		summary := args.Command
		if args.Cwd != "" {
			summary = "cd -- " + shellQuote(args.Cwd) + " && " + args.Command
		}
		if denied := g.ask(ctx, name, tools.Exec, args.Session, summary, risk.Classify(summary)); denied != nil {
			return *denied, true
		}
		result, runErr := backend.ExecCommand(ctx, args)
		exitCode := 0
		if result.ExitCode != nil {
			exitCode = *result.ExitCode
		}
		text := formatExec(ExecResult{Output: result.Output, ExitCode: exitCode, Truncated: result.Truncated, TimedOut: result.TimedOut})
		if runErr != nil {
			result.Error = runErr.Error()
			text = result.Output + "\n[error: " + runErr.Error() + "]"
		}
		reply := structured(result, text)
		reply.IsError = runErr != nil || result.TimedOut
		return reply, true
	}
	reader, hasReader := g.Backend.(FileReader)
	switch name {
	case tools.NameSFTPList:
		if !hasReader {
			return Result{}, false
		}
		args, err := tools.DecodeFileList(raw)
		if err != nil {
			return failure(err), true
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err), true
		}
		page, err := reader.ListFiles(ctx, args)
		if err != nil {
			return failure(err), true
		}
		entries := make([]FileEntry, len(page.Entries))
		for index, entry := range page.Entries {
			entries[index] = FileEntry{Name: entry.Name, Size: entry.Size, Dir: entry.Directory}
		}
		text := formatEntries(entries)
		if page.HasMore {
			text += fmt.Sprintf("\n[more entries: next_offset=%d]", page.NextOffset)
		}
		return structured(page, text), true
	case tools.NameSFTPRead:
		if !hasReader {
			return Result{}, false
		}
		args, err := tools.DecodeFileRead(raw)
		if err != nil {
			return failure(err), true
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err), true
		}
		if denied := g.approveFileRead(ctx, name, args.Session, args.Path); denied != nil {
			return *denied, true
		}
		result, err := reader.ReadRange(ctx, args)
		if err != nil {
			return failure(err), true
		}
		text := result.Data
		if result.Truncated {
			text += "\n[truncated]"
		}
		reply := structured(result, text)
		reply.Text = text
		return reply, true
	case tools.NameSFTPStat, tools.NameSFTPChecksum:
		if !hasReader {
			return failure(errors.New("file metadata is not available on this backend")), true
		}
		if name == tools.NameSFTPStat {
			args, err := tools.DecodeSFTPList(raw)
			if err != nil {
				return failure(err), true
			}
			if args.Session, err = g.targetSession(args.Session); err != nil {
				return failure(err), true
			}
			result, err := reader.StatFile(ctx, args)
			if err != nil {
				return failure(err), true
			}
			return structured(result, ""), true
		}
		args, err := tools.DecodeFileChecksum(raw)
		if err != nil {
			return failure(err), true
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err), true
		}
		if denied := g.approveFileRead(ctx, name, args.Session, args.Path); denied != nil {
			return *denied, true
		}
		result, err := reader.ChecksumFile(ctx, args)
		if err != nil {
			return failure(err), true
		}
		return structured(result, ""), true
	case tools.NameSFTPUpload, tools.NameSFTPDownload, tools.NameTransferStatus, tools.NameTransferCancel:
		backend, ok := g.Backend.(FileTransfers)
		if !ok {
			return failure(errors.New("file transfers are not available on this backend")), true
		}
		if name == tools.NameSFTPUpload || name == tools.NameSFTPDownload {
			args, err := tools.DecodeTransfer(raw)
			if err != nil {
				return failure(err), true
			}
			if args.Session, err = g.targetSession(args.Session); err != nil {
				return failure(err), true
			}
			assessment := risk.Assessment{Level: risk.High, Reasons: []string{"transfers files between this computer and a remote host"}}
			if denied := g.ask(ctx, name, tools.Write, args.Session, name+" "+args.LocalPath+" <-> "+args.RemotePath, assessment); denied != nil {
				return *denied, true
			}
			result, err := backend.StartTransfer(ctx, args, name == tools.NameSFTPUpload)
			if err != nil {
				return failure(err), true
			}
			return structured(result, ""), true
		}
		query, err := tools.DecodeTransferQuery(raw, name == tools.NameTransferCancel)
		if err != nil {
			return failure(err), true
		}
		if name == tools.NameTransferCancel {
			if denied := g.ask(ctx, name, tools.Write, "", "cancel transfer "+query.TransferID, risk.Assessment{Level: risk.Medium}); denied != nil {
				return *denied, true
			}
			result, err := backend.CancelTransfer(ctx, query.TransferID)
			if err != nil {
				return failure(err), true
			}
			return structured(result, ""), true
		}
		result, err := backend.Transfers(ctx, query.TransferID)
		if err != nil {
			return failure(err), true
		}
		return structured(map[string]any{"transfers": result}, ""), true
	}
	return Result{}, false
}

func (g Gate) approveFileRead(ctx context.Context, tool, host, path string) *Result {
	assessment := risk.Classify("cat " + shellQuote(path))
	if assessment.Level <= risk.Low {
		return nil
	}
	return g.ask(ctx, tool, tools.Read, host, strings.TrimSpace(tool+" "+path), assessment)
}
