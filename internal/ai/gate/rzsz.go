package gate

import (
	"context"
	"fmt"
	"strings"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// Phases of an rz/sz transfer that has not finished.
const (
	// RZSZPhaseWaiting means rz or sz has been typed and no file bytes have moved.
	RZSZPhaseWaiting = "waiting"
	// RZSZPhaseTransferring means the two ends are in ZMODEM and bytes_done is
	// how much of the file has moved. The terminal keeps showing the remote's
	// "rz waiting to receive" line until the transfer ends, so this phase is
	// the way to tell a live transfer from one the sender never joined.
	RZSZPhaseTransferring = "transferring"
)

// RZSZSnapshot is an rz/sz transfer that is still running.
type RZSZSnapshot struct {
	Active     bool   `json:"active"`
	Direction  string `json:"direction,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Name       string `json:"name,omitempty"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal int64  `json:"bytes_total"`
}

// FormatRZSZSnapshot is the one line a progress notification and rzsz_status share.
func FormatRZSZSnapshot(snap RZSZSnapshot) string {
	name := oneLine(snap.Name)
	if name == "" {
		name = "the file"
	}
	switch snap.Phase {
	case RZSZPhaseWaiting:
		return "waiting for the remote to accept " + name + "; 0 bytes have moved. The screen stays at rz waiting to receive until the transfer ends"
	case RZSZPhaseTransferring:
		if snap.BytesTotal >= 0 {
			return fmt.Sprintf("transferring %s; %d/%d bytes have moved. The screen stays at rz waiting to receive until the transfer ends", name, snap.BytesDone, snap.BytesTotal)
		}
		return fmt.Sprintf("transferring %s; %d bytes have moved. The screen stays at rz waiting to receive until the transfer ends", name, snap.BytesDone)
	default:
		return "rz/sz transfer in progress"
	}
}

func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

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

// RZSZObserver reads an rz/sz transfer while it runs. active is false when
// that session has none.
type RZSZObserver interface {
	RZSZStatus(ctx context.Context, session string) (RZSZSnapshot, bool, error)
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

func (g Gate) runRZSZStatus(ctx context.Context, raw []byte) Result {
	observer, ok := g.Backend.(RZSZObserver)
	if !ok {
		return Result{Text: "rz/sz needs an open terminal in the desktop app. This server has none.", IsError: true}
	}
	args, err := tools.DecodeRZSZStatus(raw)
	if err != nil {
		return failure(err)
	}
	if args.Session, err = g.targetSession(args.Session); err != nil {
		return failure(err)
	}
	snap, active, err := observer.RZSZStatus(ctx, args.Session)
	if err != nil {
		return failure(err)
	}
	if !active {
		return structured(struct {
			Active bool `json:"active"`
		}{false}, "no rz/sz transfer is running")
	}
	snap.Active = true
	return structured(snap, FormatRZSZSnapshot(snap))
}
