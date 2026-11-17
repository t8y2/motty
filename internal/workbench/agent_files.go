package workbench

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/sftpclient"
)

var _ gate.FileReader = (*agentBackend)(nil)
var _ gate.HostCommands = (*agentBackend)(nil)

func (b *agentBackend) withFiles(ctx context.Context, hostID string, operation func(sftpclient.Remote) error) error {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errAgentClosed
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopBackend := context.AfterFunc(b.transferCtx, cancel)
	defer stopBackend()
	host, err := b.resolve(hostID)
	if err != nil {
		return err
	}
	conn, err := b.dial(ctx, host)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, conn.Close)
	defer stop()
	remote, err := openRemoteFiles(realConn{conn}, host.Transfer)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	err = operation(remote)
	closeErr := remote.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	return closeErr
}

func agentFileInfo(info os.FileInfo) gate.FileInfo {
	return gate.FileInfo{Name: info.Name(), Size: info.Size(), Mode: info.Mode().String(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano), Directory: info.IsDir(), Symlink: info.Mode()&os.ModeSymlink != 0}
}

func remoteInfo(remote sftpclient.Remote, path string, regular bool) (os.FileInfo, error) {
	stat, ok := remote.(interface {
		Lstat(string) (os.FileInfo, error)
	})
	if !ok {
		return nil, sftpclient.ErrUnsupported
	}
	info, err := stat.Lstat(path)
	if err != nil {
		return nil, err
	}
	if regular && !info.Mode().IsRegular() {
		return nil, errors.New("only regular files can be read or checksummed; symbolic links are refused")
	}
	return info, nil
}

func (b *agentBackend) ListFiles(ctx context.Context, args tools.FileListArgs) (gate.FilePage, error) {
	page := gate.FilePage{Entries: []gate.FileInfo{}}
	err := b.withFiles(ctx, args.Session, func(remote sftpclient.Remote) error {
		entries, err := remote.ReadDir(args.Path)
		if err != nil {
			return err
		}
		if len(entries) > 1_000_000 {
			return errors.New("directory exceeds the one-million-entry listing limit")
		}
		slices.SortFunc(entries, func(left, right os.FileInfo) int { return strings.Compare(left.Name(), right.Name()) })
		page.Total = len(entries)
		start := min(args.Offset, len(entries))
		end := min(start+args.Limit, len(entries))
		for _, info := range entries[start:end] {
			page.Entries = append(page.Entries, agentFileInfo(info))
		}
		page.NextOffset, page.HasMore, page.Approximate = end, end < len(entries), sftpclient.IsSCP(remote)
		return nil
	})
	return page, err
}

func (b *agentBackend) StatFile(ctx context.Context, args tools.SFTPListArgs) (gate.FileInfo, error) {
	var result gate.FileInfo
	err := b.withFiles(ctx, args.Session, func(remote sftpclient.Remote) error {
		info, err := remoteInfo(remote, args.Path, false)
		if err != nil {
			return err
		}
		result = agentFileInfo(info)
		result.Approximate = sftpclient.IsSCP(remote)
		return nil
	})
	return result, err
}

func (b *agentBackend) ReadRange(ctx context.Context, args tools.FileReadArgs) (gate.FileRange, error) {
	result := gate.FileRange{Encoding: args.Encoding}
	err := b.withFiles(ctx, args.Session, func(remote sftpclient.Remote) (operationErr error) {
		info, err := remoteInfo(remote, args.Path, true)
		if err != nil {
			return err
		}
		result.Size = info.Size()
		result.Offset = args.Offset
		if args.Tail {
			result.Offset = max(0, info.Size()-int64(args.MaxBytes))
		}
		file, err := remote.Open(args.Path)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := file.Close(); operationErr == nil {
				operationErr = closeErr
			}
		}()
		if result.Offset > 0 {
			if seeker, ok := file.(io.Seeker); ok {
				if _, err := seeker.Seek(result.Offset, io.SeekStart); err != nil {
					return err
				}
			} else {
				if _, err := io.CopyN(io.Discard, file, result.Offset); err != nil && !errors.Is(err, io.EOF) {
					return err
				}
			}
		}
		data, err := io.ReadAll(io.LimitReader(file, int64(args.MaxBytes)+1))
		if err != nil {
			return err
		}
		result.Truncated = len(data) > args.MaxBytes
		if result.Truncated {
			data = data[:args.MaxBytes]
		}
		result.BytesRead = len(data)
		result.NextOffset = result.Offset + int64(len(data))
		result.EOF = !result.Truncated
		if args.Encoding == "base64" {
			result.Data = base64.StdEncoding.EncodeToString(data)
		} else {
			result.Data = strings.ToValidUTF8(string(data), "�")
		}
		return nil
	})
	return result, err
}

func (b *agentBackend) ChecksumFile(ctx context.Context, args tools.FileChecksumArgs) (gate.FileChecksum, error) {
	result := gate.FileChecksum{Algorithm: args.Algorithm}
	err := b.withFiles(ctx, args.Session, func(remote sftpclient.Remote) (operationErr error) {
		before, err := remoteInfo(remote, args.Path, true)
		if err != nil {
			return err
		}
		file, err := remote.Open(args.Path)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := file.Close(); operationErr == nil {
				operationErr = closeErr
			}
		}()
		digest := sha256.New()
		if args.Algorithm == "sha512" {
			digest = sha512.New()
		}
		result.Bytes, err = io.Copy(digest, file)
		if err != nil {
			return err
		}
		after, err := remoteInfo(remote, args.Path, true)
		if err != nil {
			return err
		}
		if result.Bytes != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			return errors.New("file changed while computing checksum; retry on a stable file")
		}
		result.Digest = hex.EncodeToString(digest.Sum(nil))
		return nil
	})
	return result, err
}

func (b *agentBackend) ExecCommand(ctx context.Context, args tools.HostExecArgs) (gate.CommandResult, error) {
	started := time.Now()
	result := gate.CommandResult{}
	entry, err := b.acquire(ctx, args.Session)
	if err != nil {
		result.DurationMS = time.Since(started).Milliseconds()
		return result, err
	}
	defer entry.release()
	limited, cancel := context.WithTimeout(ctx, execTimeout(args.TimeoutSeconds))
	defer cancel()
	command := args.Command
	if args.Cwd != "" {
		command = agentCommandAt(args.Command, args.Cwd)
	}
	executed, err := entry.conn.Exec(limited, command, maxExecOutput)
	result.Output = strings.ToValidUTF8(string(executed.Output), "�")
	result.Stdout = strings.ToValidUTF8(string(executed.Stdout), "�")
	result.Stderr = strings.ToValidUTF8(string(executed.Stderr), "�")
	result.Truncated = executed.Truncated
	result.DurationMS = time.Since(started).Milliseconds()
	if err == nil {
		result.ExitCode = &executed.ExitCode
		return result, nil
	}
	result.TimedOut = errors.Is(err, context.DeadlineExceeded)
	return result, fmt.Errorf("SSH command failed: %w", err)
}

func agentCommandAt(command, cwd string) string {
	quoted := "'" + strings.ReplaceAll(cwd, "'", "'\\''") + "'"
	return "cd -- " + quoted + " && {\n" + command + "\n}"
}
