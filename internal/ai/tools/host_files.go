package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

const (
	NameSFTPStat       = "sftp_stat"
	NameSFTPChecksum   = "sftp_checksum"
	NameSFTPUpload     = "sftp_upload"
	NameSFTPDownload   = "sftp_download"
	NameTransferStatus = "transfer_status"
	NameTransferCancel = "transfer_cancel"
	NameUploadSelected = "sftp_upload_selected"
	MaxPageEntries     = 1000
)

const hostSessionProperty = `"session":{"type":"string","maxLength":256,"description":"Authorized host id from list_sessions; optional when only one host is available."}`
const remotePathProperty = `"path":{"type":"string","minLength":1,"maxLength":4096,"description":"Remote file or directory path."}`

var hostFileSpecs = []Spec{
	{Name: NameSFTPStat, Description: "Inspect a remote path without following its final symbolic link. Returns size, type, permissions and modification time; SCP metadata is approximate.", Kind: Read,
		Schema: json.RawMessage(`{"type":"object","properties":{` + hostSessionProperty + `,` + remotePathProperty + `},"required":["path"],"additionalProperties":false}`)},
	{Name: NameSFTPChecksum, Description: "Stream a regular remote file through SHA-256 or SHA-512 without returning its contents. Symbolic links are refused. Uses the host's SFTP/SCP configuration.", Kind: Read,
		Schema: json.RawMessage(`{"type":"object","properties":{` + hostSessionProperty + `,` + remotePathProperty + `,"algorithm":{"type":"string","enum":["sha256","sha512"],"default":"sha256"}},"required":["path"],"additionalProperties":false}`)},
	{Name: NameTransferStatus, Description: "Query a transfer by transfer_id, or list this MCP process's bounded transfer history when omitted. Only state completed means the transfer succeeded.", Kind: Read,
		Schema: json.RawMessage(`{"type":"object","properties":{"transfer_id":{"type":"string","maxLength":64}},"additionalProperties":false}`)},
	{Name: NameSFTPUpload, Description: "Start a binary-safe upload from an allowed local transfer root to an authorized host, using SFTP or SCP. Returns a task id, not completion. Poll transfer_status. Existing destinations fail unless an explicit policy is supplied.", Kind: Write, Schema: transferSchema()},
	{Name: NameSFTPDownload, Description: "Start a binary-safe download from an authorized host into an allowed local transfer root, using SFTP or SCP. Returns a task id, not completion. Poll transfer_status. Existing destinations fail unless an explicit policy is supplied.", Kind: Write, Schema: transferSchema()},
	{Name: NameTransferCancel, Description: "Request cancellation of this MCP process's transfer. Poll transfer_status until it settles. SCP uploads can leave a partial destination; cancellation does not undo already committed files.", Kind: Write,
		Schema: json.RawMessage(`{"type":"object","properties":{"transfer_id":{"type":"string","minLength":1,"maxLength":64}},"required":["transfer_id"],"additionalProperties":false}`)},
}

var selectedUploadSpec = Spec{
	Name: NameUploadSelected, Kind: Write,
	Description: "Upload a file explicitly selected by the user for this request to an authorized SSH host. File contents never enter the model context. The call completes only when the transfer settles. Existing destinations fail by default; replacement requires an explicit policy and user approval.",
	Schema:      json.RawMessage(`{"type":"object","properties":{"file_id":{"type":"string","minLength":1,"maxLength":16,"description":"ID of a file the user selected for this request."},"session":{"type":"string","maxLength":256},"remote_path":{"type":"string","minLength":1,"maxLength":4096,"description":"Full destination path on the SSH host."},"existing":{"type":"string","enum":["fail","replace","resume"],"default":"fail"}},"required":["file_id","remote_path"],"additionalProperties":false}`),
}

type SelectedUploadArgs struct {
	FileID     string `json:"file_id"`
	Session    string `json:"session,omitempty"`
	RemotePath string `json:"remote_path"`
	Existing   string `json:"existing,omitempty"`
}

func DecodeSelectedUpload(raw []byte) (SelectedUploadArgs, error) {
	args := SelectedUploadArgs{Existing: "fail"}
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if len(args.FileID) == 0 || len(args.FileID) > 16 {
		return args, errors.New("file_id must identify a selected file")
	}
	if err := fileTarget(args.Session, args.RemotePath); err != nil {
		return args, err
	}
	if args.Existing != "fail" && args.Existing != "replace" && args.Existing != "resume" {
		return args, errors.New("existing must be fail, replace or resume")
	}
	return args, nil
}

func transferSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + hostSessionProperty + `,"local_path":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute path on the computer running motty mcp-serve, within a configured --transfer-root."},"remote_path":{"type":"string","minLength":1,"maxLength":4096,"description":"Full destination or source path on the remote host, not just a destination parent."},"recursive":{"type":"boolean","default":false},"existing":{"type":"string","enum":["fail","replace","resume"],"default":"fail","description":"SCP does not support resume; it is rejected rather than restarted."}},"required":["local_path","remote_path"],"additionalProperties":false}`)
}

type HostExecArgs struct {
	Session        string `json:"session,omitempty"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
}

type FileListArgs struct {
	Session string `json:"session,omitempty"`
	Path    string `json:"path"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type FileReadArgs struct {
	Session  string `json:"session,omitempty"`
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
	Tail     bool   `json:"tail,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type FileChecksumArgs struct {
	Session   string `json:"session,omitempty"`
	Path      string `json:"path"`
	Algorithm string `json:"algorithm,omitempty"`
}

type TransferArgs struct {
	Session    string `json:"session,omitempty"`
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Recursive  bool   `json:"recursive,omitempty"`
	Existing   string `json:"existing,omitempty"`
}

type TransferQuery struct {
	TransferID string `json:"transfer_id,omitempty"`
}

func DecodeHostExec(raw []byte) (HostExecArgs, error) {
	args := HostExecArgs{TimeoutSeconds: DefaultTimeoutSeconds}
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if _, err := session(&args.Session); err != nil {
		return args, err
	}
	if _, _, err := commandAndTimeout(&args.Command, &args.TimeoutSeconds); err != nil {
		return args, err
	}
	if args.Cwd != "" {
		if _, err := path(&args.Cwd); err != nil {
			return args, err
		}
		if args.Cwd[0] != '/' {
			return args, errors.New("cwd must be an absolute POSIX remote directory")
		}
	}
	return args, nil
}

func DecodeFileList(raw []byte) (FileListArgs, error) {
	args := FileListArgs{Limit: 500}
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if err := fileTarget(args.Session, args.Path); err != nil {
		return args, err
	}
	if args.Offset < 0 || args.Offset > 1_000_000 || args.Limit < 1 || args.Limit > MaxPageEntries {
		return args, fmt.Errorf("offset must be 0..1000000 and limit 1..%d", MaxPageEntries)
	}
	return args, nil
}

func DecodeFileRead(raw []byte) (FileReadArgs, error) {
	args := FileReadArgs{MaxBytes: MaxReadBytes, Encoding: "utf8"}
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if err := fileTarget(args.Session, args.Path); err != nil {
		return args, err
	}
	if args.MaxBytes < 1 || args.MaxBytes > MaxReadBytes || args.Offset < 0 || args.Offset > 1<<53-1 {
		return args, errors.New("invalid max_bytes or offset")
	}
	if args.Tail && args.Offset != 0 {
		return args, errors.New("tail and a nonzero offset are mutually exclusive")
	}
	if args.Encoding != "utf8" && args.Encoding != "base64" {
		return args, errors.New("encoding must be utf8 or base64")
	}
	return args, nil
}

func DecodeFileChecksum(raw []byte) (FileChecksumArgs, error) {
	args := FileChecksumArgs{Algorithm: "sha256"}
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if err := fileTarget(args.Session, args.Path); err != nil {
		return args, err
	}
	if args.Algorithm != "sha256" && args.Algorithm != "sha512" {
		return args, errors.New("algorithm must be sha256 or sha512")
	}
	return args, nil
}

func DecodeTransfer(raw []byte) (TransferArgs, error) {
	args := TransferArgs{Existing: "fail"}
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if err := fileTarget(args.Session, args.RemotePath); err != nil {
		return args, err
	}
	if _, err := path(&args.LocalPath); err != nil {
		return args, err
	}
	if !filepath.IsAbs(args.LocalPath) {
		return args, errors.New("local_path must be absolute on the MCP server computer")
	}
	if args.Existing != "fail" && args.Existing != "replace" && args.Existing != "resume" {
		return args, errors.New("existing must be fail, replace or resume")
	}
	return args, nil
}

func DecodeTransferQuery(raw []byte, required bool) (TransferQuery, error) {
	var args TransferQuery
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if len(args.TransferID) > 64 || required && args.TransferID == "" {
		return args, errors.New("transfer_id is required and must be at most 64 bytes")
	}
	return args, text("transfer_id", args.TransferID, false)
}

func fileTarget(host, remote string) error {
	if _, err := session(&host); err != nil {
		return err
	}
	_, err := path(&remote)
	return err
}

// hostSchema adapts a shared tool to the headless server. transfers reports
// whether the transfer tools are offered, so descriptions never point at a
// tool the client cannot call.
func hostSchema(spec Spec, transfers bool) Spec {
	switch spec.Name {
	case NameTerminalExec:
		spec.Schema = json.RawMessage(`{"type":"object","properties":{` + hostSessionProperty + `,"command":{"type":"string","minLength":1,"maxLength":8192},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":60},"cwd":{"type":"string","minLength":1,"maxLength":4096,"description":"Optional absolute POSIX working directory for this command only."}},"required":["command"],"additionalProperties":false}`)
		spec.Description = "Run a non-interactive SSH command on an authorized host. Returns structured stdout, stderr, exit_code (null if unavailable), duration_ms, truncated and timed_out, plus legacy combined text. Optional cwd requires a POSIX remote shell; state never persists between calls."
	case NameSFTPList:
		spec.Schema = json.RawMessage(`{"type":"object","properties":{` + hostSessionProperty + `,` + remotePathProperty + `,"offset":{"type":"integer","minimum":0,"maximum":1000000,"default":0},"limit":{"type":"integer","minimum":1,"maximum":1000,"default":500}},"required":["path"],"additionalProperties":false}`)
		spec.Description = "List a name-sorted page of a remote directory using SFTP or SCP. Returns entries, total, next_offset and has_more. Pages are live, not a snapshot; restart pagination if the directory changes."
	case NameSFTPRead:
		spec.Schema = json.RawMessage(`{"type":"object","properties":{` + hostSessionProperty + `,` + remotePathProperty + `,"max_bytes":{"type":"integer","minimum":1,"maximum":262144,"default":262144},"offset":{"type":"integer","minimum":0,"maximum":9007199254740991,"default":0},"tail":{"type":"boolean","default":false,"description":"Read the last max_bytes; cannot combine with nonzero offset."},"encoding":{"type":"string","enum":["utf8","base64"],"default":"utf8"}},"required":["path"],"additionalProperties":false}`)
		spec.Description = "Read a bounded range of a regular remote file, or its tail. Returns byte offsets, bytes_read, eof and truncated. Choose base64 to preserve arbitrary bytes; utf8 replaces invalid sequences."
		if transfers {
			spec.Description += " Use sftp_download for a complete local copy."
		}
	}
	return spec
}
