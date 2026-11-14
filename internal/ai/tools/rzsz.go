package tools

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RZSZ tools drive rz and sz on the visible terminal. They are not in the
// headless catalog: that server has no terminal, and its SFTP connection
// already follows a configured jump to the final host.

const (
	rzszSession = `"session":{"type":"string","maxLength":256,"description":"Session id from list_sessions. Defaults to the active session."}`
	rzszTimeout = `"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":60,"description":"Give up after this many seconds."}`
	rzszReplace = `"overwrite":{"type":"boolean","default":false,"description":"Replace an existing file. Otherwise the transfer fails, or the remote skips it."}`
)

func init() {
	specs = append(specs,
		Spec{
			Name: NameRZSZUpload,
			Kind: Write,
			Description: "Upload one local file with rz and ZMODEM when SFTP is not this shell. The remote needs rz. " +
				"The file keeps its name; an existing file is skipped unless overwrite is true. " +
				"Reports how many bytes have moved. A client timeout is not a failure: call rzsz_status. " +
				"The screen stays at rz waiting to receive until the end either way.",
			Schema: json.RawMessage(`{"type":"object","properties":{` + rzszSession +
				`,"local_path":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute path of the file on the computer running Motty."}` +
				`,"remote_dir":{"type":"string","maxLength":4096,"description":"Remote directory to enter before rz. Empty uses the shell's working directory."}` +
				`,` + rzszReplace + `,` + rzszTimeout + `},"required":["local_path"],"additionalProperties":false}`),
		},
		Spec{
			Name: NameRZSZDownload,
			Kind: Write,
			Description: "Download one remote file with sz and ZMODEM into local_dir when SFTP is not this shell. The remote needs sz. " +
				"The file keeps its name; an existing local file fails unless overwrite is true. " +
				"Reports how many bytes have moved. A client timeout is not a failure: call rzsz_status. " +
				"The screen stays at rz waiting to receive until the end either way.",
			Schema: json.RawMessage(`{"type":"object","properties":{` + rzszSession +
				`,"remote_path":{"type":"string","minLength":1,"maxLength":4096,"description":"File path as the remote shell sees it."}` +
				`,"local_dir":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute directory on the computer running Motty."}` +
				`,` + rzszReplace + `,` + rzszTimeout + `},"required":["local_dir","remote_path"],"additionalProperties":false}`),
		},
		Spec{
			Name: NameRZSZStatus,
			Kind: Read,
			Description: "Read the rz/sz transfer on this terminal. phase waiting and bytes_done 0 means the remote has not accepted the file. " +
				"phase transferring means bytes are moving, even while the screen still says rz waiting to receive. " +
				"Do not start another transfer. active is false when none is running. bytes_total is -1 until the size is known.",
			Schema: json.RawMessage(`{"type":"object","properties":{` + rzszSession + `},"additionalProperties":false}`),
		},
	)
}

// RZSZUploadArgs is a local file sent to the remote shell's rz.
type RZSZUploadArgs struct {
	Session        string `json:"session,omitempty"`
	LocalPath      string `json:"local_path"`
	RemoteDir      string `json:"remote_dir,omitempty"`
	Overwrite      bool   `json:"overwrite,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// RZSZDownloadArgs is a remote file sz sends into a local directory.
type RZSZDownloadArgs struct {
	Session        string `json:"session,omitempty"`
	RemotePath     string `json:"remote_path"`
	LocalDir       string `json:"local_dir"`
	Overwrite      bool   `json:"overwrite,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// RZSZRequest is the decoded call the backend runs. Remote is the directory to
// enter before rz, or the file sz sends.
type RZSZRequest struct {
	Session        string
	Upload         bool
	LocalPath      string
	Remote         string
	Overwrite      bool
	TimeoutSeconds int
}

// DecodeRZSZUpload validates an upload call.
func DecodeRZSZUpload(raw []byte) (RZSZUploadArgs, error) {
	var w struct {
		Session        *string `json:"session"`
		LocalPath      *string `json:"local_path"`
		RemoteDir      *string `json:"remote_dir"`
		Overwrite      *bool   `json:"overwrite"`
		TimeoutSeconds *int    `json:"timeout_seconds"`
	}
	if err := decode(raw, &w); err != nil {
		return RZSZUploadArgs{}, err
	}
	var args RZSZUploadArgs
	var err error
	if args.Session, err = session(w.Session); err != nil {
		return RZSZUploadArgs{}, err
	}
	if w.LocalPath == nil || *w.LocalPath == "" {
		return RZSZUploadArgs{}, errors.New("local_path is required")
	}
	if len(*w.LocalPath) > MaxPathLen {
		return RZSZUploadArgs{}, fmt.Errorf("local_path is longer than %d bytes", MaxPathLen)
	}
	if err = text("local_path", *w.LocalPath, false); err != nil {
		return RZSZUploadArgs{}, err
	}
	args.LocalPath = *w.LocalPath
	if w.RemoteDir != nil && *w.RemoteDir != "" {
		if len(*w.RemoteDir) > MaxPathLen {
			return RZSZUploadArgs{}, fmt.Errorf("remote_dir is longer than %d bytes", MaxPathLen)
		}
		if err = text("remote_dir", *w.RemoteDir, false); err != nil {
			return RZSZUploadArgs{}, err
		}
		args.RemoteDir = *w.RemoteDir
	}
	args.Overwrite = w.Overwrite != nil && *w.Overwrite
	args.TimeoutSeconds, err = rzszTimeoutSeconds(w.TimeoutSeconds)
	return args, err
}

// DecodeRZSZDownload validates a download call.
func DecodeRZSZDownload(raw []byte) (RZSZDownloadArgs, error) {
	var w struct {
		Session        *string `json:"session"`
		RemotePath     *string `json:"remote_path"`
		LocalDir       *string `json:"local_dir"`
		Overwrite      *bool   `json:"overwrite"`
		TimeoutSeconds *int    `json:"timeout_seconds"`
	}
	if err := decode(raw, &w); err != nil {
		return RZSZDownloadArgs{}, err
	}
	var args RZSZDownloadArgs
	var err error
	if args.Session, err = session(w.Session); err != nil {
		return RZSZDownloadArgs{}, err
	}
	if w.RemotePath == nil || *w.RemotePath == "" {
		return RZSZDownloadArgs{}, errors.New("remote_path is required")
	}
	if len(*w.RemotePath) > MaxPathLen {
		return RZSZDownloadArgs{}, fmt.Errorf("remote_path is longer than %d bytes", MaxPathLen)
	}
	if err = text("remote_path", *w.RemotePath, false); err != nil {
		return RZSZDownloadArgs{}, err
	}
	args.RemotePath = *w.RemotePath
	if w.LocalDir == nil || *w.LocalDir == "" {
		return RZSZDownloadArgs{}, errors.New("local_dir is required")
	}
	if len(*w.LocalDir) > MaxPathLen {
		return RZSZDownloadArgs{}, fmt.Errorf("local_dir is longer than %d bytes", MaxPathLen)
	}
	if err = text("local_dir", *w.LocalDir, false); err != nil {
		return RZSZDownloadArgs{}, err
	}
	args.LocalDir = *w.LocalDir
	args.Overwrite = w.Overwrite != nil && *w.Overwrite
	args.TimeoutSeconds, err = rzszTimeoutSeconds(w.TimeoutSeconds)
	return args, err
}

func rzszTimeoutSeconds(timeout *int) (int, error) {
	if timeout == nil {
		return DefaultTimeoutSeconds, nil
	}
	if *timeout < 1 || *timeout > MaxTimeoutSeconds {
		return 0, fmt.Errorf("timeout_seconds must be between 1 and %d", MaxTimeoutSeconds)
	}
	return *timeout, nil
}

// Request is the backend form of an upload.
func (args RZSZUploadArgs) Request() RZSZRequest {
	return RZSZRequest{Session: args.Session, Upload: true, LocalPath: args.LocalPath, Remote: args.RemoteDir, Overwrite: args.Overwrite, TimeoutSeconds: args.TimeoutSeconds}
}

// Request is the backend form of a download. LocalPath is the directory.
func (args RZSZDownloadArgs) Request() RZSZRequest {
	return RZSZRequest{Session: args.Session, LocalPath: args.LocalDir, Remote: args.RemotePath, Overwrite: args.Overwrite, TimeoutSeconds: args.TimeoutSeconds}
}

// RZSZStatusArgs selects the terminal whose rz/sz transfer to read.
type RZSZStatusArgs struct {
	Session string `json:"session,omitempty"`
}

// DecodeRZSZStatus validates a status call. An empty object reads the active session.
func DecodeRZSZStatus(raw []byte) (RZSZStatusArgs, error) {
	var w struct {
		Session *string `json:"session"`
	}
	if err := decode(raw, &w); err != nil {
		return RZSZStatusArgs{}, err
	}
	session, err := session(w.Session)
	return RZSZStatusArgs{Session: session}, err
}
