package gate

import (
	"context"
	"fmt"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// RZSZStatus is one finished rz/sz transfer.
type RZSZStatus struct {
	Direction  string `json:"direction"`
	Files      int    `json:"files"`
	Bytes      int64  `json:"bytes"`
	LocalPath  string `json:"local_path"`
	RemoteName string `json:"remote_name"`
}

// RZSZMover runs rz or sz on a visible terminal. A backend without one, such
// as the headless server, does not implement it.
type RZSZMover interface {
	TransferRZSZ(ctx context.Context, req tools.RZSZRequest) (RZSZStatus, error)
}

func (g Gate) runRZSZ(ctx context.Context, name string, raw []byte) Result {
	mover, ok := g.Backend.(RZSZMover)
	if !ok {
		return Result{Text: "rz/sz needs an open terminal in the desktop app. This server has none; sftp_upload already follows a jump configured on the host.", IsError: true}
	}
	var req tools.RZSZRequest
	var summary string
	if name == tools.NameRZSZUpload {
		args, err := tools.DecodeRZSZUpload(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		req = args.Request()
		where := "the working directory"
		if req.Remote != "" {
			where = req.Remote
		}
		summary = fmt.Sprintf("rzsz upload %s -> %s", req.LocalPath, where)
	} else {
		args, err := tools.DecodeRZSZDownload(raw)
		if err != nil {
			return failure(err)
		}
		if args.Session, err = g.targetSession(args.Session); err != nil {
			return failure(err)
		}
		req = args.Request()
		summary = fmt.Sprintf("rzsz download %s -> %s", req.Remote, req.LocalPath)
	}
	assessment := risk.Assessment{Level: risk.High, Reasons: []string{"transfers a file through the terminal with rz/sz"}}
	if denied := g.ask(ctx, name, tools.Write, req.Session, summary, assessment); denied != nil {
		return *denied
	}
	status, err := mover.TransferRZSZ(ctx, req)
	if err != nil {
		return failure(err)
	}
	return structured(status, fmt.Sprintf("transferred %d bytes as %s (%s)", status.Bytes, status.RemoteName, status.LocalPath))
}
