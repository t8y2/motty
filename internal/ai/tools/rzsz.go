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
			Description: "Upload one local file by running rz in this visible terminal and sending it with ZMODEM. " +
				"Use it when this shell was reached through a jump host and SFTP is not this shell. The remote needs rz. " +
				"The file keeps its name in the working directory, or in remote_dir. An existing file is skipped unless overwrite is true.",
			Schema: json.RawMessage(`{"type":"object","properties":{` + rzszSession +
				`,"local_path":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute path of the file on the computer running Motty."}` +
				`,"remote_dir":{"type":"string","maxLength":4096,"description":"Remote directory to enter before rz. Empty uses the shell's working directory."}` +
				`,` + rzszReplace + `,` + rzszTimeout + `},"required":["local_path"],"additionalProperties":false}`),
		},
		Spec{
			Name: NameRZSZDownload,
			Kind: Write,
			Description: "Download one remote file by running sz in this visible terminal and receiving it with ZMODEM into local_dir. " +
				"Use it when this shell was reached through a jump host and SFTP is not this shell. The remote needs sz. " +
				"The file keeps its remote name. An existing local file fails unless overwrite is true.",
			Schema: json.RawMessage(`{"type":"object","properties":{` + rzszSession +
				`,"remote_path":{"type":"string","minLength":1,"maxLength":4096,"description":"File path as the remote shell sees it."}` +
				`,"local_dir":{"type":"string","minLength":1,"maxLength":4096,"description":"Absolute directory on the computer running Motty."}` +
				`,` + rzszReplace + `,` + rzszTimeout + `},"required":["local_dir","remote_path"],"additionalProperties":false}`),
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
