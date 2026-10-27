package workbench

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

const (
	maxListEntries  = 500
	maxReadChars    = 64 << 10
	maxScreenLines  = 500
	errSFTPNeedsSSH = "SFTP needs an SSH session; this one is local or not connected"
)

// aiBackend carries tool calls out on this window's sessions. Its methods run on
// any goroutine: they hop to the window thread for anything in the State, and
// talk to the server from the calling goroutine.
type aiBackend struct {
	state *State
	// A request owns its backend and connection lifetime. Reconnecting the same
	// tab must not resume an old request or reuse its SFTP client.
	boundID       string
	boundTerminal *terminal
	boundDone     <-chan struct{}

	mu   sync.Mutex
	sftp map[uint32]sftpclient.Remote // one per session, opened when first needed
}

var _ gate.Backend = (*aiBackend)(nil)

func (b *aiBackend) Sessions(ctx context.Context) ([]gate.SessionInfo, error) {
	return aiCall(b.state, ctx, func() ([]gate.SessionInfo, error) {
		var out []gate.SessionInfo
		for index := range b.state.sessions {
			session := &b.state.sessions[index]
			if session.term == nil {
				continue
			}
			out = append(out, gate.SessionInfo{
				ID: sessionHandle(session.ID), Title: session.Title, Address: session.Address,
				Active: session.ID == b.state.activeSessionID,
				Ready:  session.term.shell != nil && session.term.live,
			})
		}
		return out, nil
	})
}

func (b *aiBackend) ReadTerminal(ctx context.Context, args tools.TerminalReadArgs) (string, error) {
	return aiCall(b.state, ctx, func() (string, error) {
		session, err := b.session(args.Session)
		if err != nil {
			return "", err
		}
		if session.term == nil {
			return "", errNoShell
		}
		var text string
		if args.Scope == tools.ScopeScrollback {
			text = string(session.term.screen.AppendRetainedText(nil))
		} else {
			text = session.term.screen.Text()
		}
		return lastLines(text, min(args.Lines, maxScreenLines)), nil
	})
}

// lastLines keeps the final n lines of text.
func lastLines(text string, n int) string {
	text = strings.TrimRight(text, "\n ")
	if n <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (b *aiBackend) Exec(ctx context.Context, args tools.TerminalExecArgs) (gate.ExecResult, error) {
	exec, err := aiCall(b.state, ctx, func() (*aiExec, error) {
		if _, err := b.session(args.Session); err != nil {
			return nil, err
		}
		return b.state.beginExec(args)
	})
	if err != nil {
		return gate.ExecResult{}, err
	}
	return b.state.aiExecWait(exec, execTimeout(args.TimeoutSeconds), ctx.Done()), nil
}

// remote returns the session's SFTP client, opening it on first use.
func (b *aiBackend) remote(ctx context.Context, handle string) (sftpclient.Remote, uint32, error) {
	type target struct {
		id       uint32
		conn     Connection
		transfer transferMode
	}
	t, err := aiCall(b.state, ctx, func() (target, error) {
		session, err := b.session(handle)
		if err != nil {
			return target{}, err
		}
		if !session.hasServer() || session.conn == nil || session.phase != sshconn.PhaseConnected {
			return target{}, errors.New(errSFTPNeedsSSH)
		}
		return target{session.ID, session.conn, session.host.Transfer}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	b.mu.Lock()
	remote := b.sftp[t.id]
	b.mu.Unlock()
	if remote != nil {
		return remote, t.id, nil
	}
	opened, err := openRemoteFiles(t.conn, t.transfer)
	if err != nil {
		return nil, 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing := b.sftp[t.id]; existing != nil {
		_ = opened.Close()
		return existing, t.id, nil
	}
	if b.sftp == nil {
		b.sftp = map[uint32]sftpclient.Remote{}
	}
	b.sftp[t.id] = opened
	return opened, t.id, nil
}

// drop forgets a session's client after it failed, so the next call reconnects.
func (b *aiBackend) drop(id uint32) {
	b.mu.Lock()
	remote := b.sftp[id]
	delete(b.sftp, id)
	b.mu.Unlock()
	if remote != nil {
		_ = remote.Close()
	}
}

func (b *aiBackend) closeAll() {
	b.mu.Lock()
	remotes := b.sftp
	b.sftp = nil
	b.mu.Unlock()
	for _, remote := range remotes {
		_ = remote.Close()
	}
}

func (b *aiBackend) ListDir(ctx context.Context, args tools.SFTPListArgs) ([]gate.FileEntry, error) {
	remote, id, err := b.remote(ctx, args.Session)
	if err != nil {
		return nil, err
	}
	entries, err := listRemoteDir(remote, args.Path)
	if err != nil {
		b.drop(id)
		return nil, err
	}
	return entries, nil
}

func (b *aiBackend) ReadFile(ctx context.Context, args tools.SFTPReadArgs) (string, bool, error) {
	remote, id, err := b.remote(ctx, args.Session)
	if err != nil {
		return "", false, err
	}
	text, truncated, err := readRemoteFile(remote, args.Path, args.MaxBytes)
	if err != nil {
		b.drop(id)
		return "", false, err
	}
	return text, truncated, nil
}

func (b *aiBackend) WriteFile(ctx context.Context, args tools.SFTPWriteArgs) error {
	remote, id, err := b.remote(ctx, args.Session)
	if err != nil {
		return err
	}
	if err := sftpclient.WriteFile(remote, args.Path, []byte(args.Content), args.Overwrite); err != nil {
		if !errors.Is(err, sftpclient.ErrExists) {
			b.drop(id)
		}
		return err
	}
	return nil
}

func (b *aiBackend) Remove(ctx context.Context, args tools.SFTPRemoveArgs) error {
	remote, id, err := b.remote(ctx, args.Session)
	if err != nil {
		return err
	}
	if err := sftpclient.Delete(remote, args.Path); err != nil {
		b.drop(id)
		return err
	}
	return nil
}

// listRemoteDir lists a directory for a model: sorted by name, and cut at
// maxListEntries.
func listRemoteDir(remote sftpclient.Remote, path string) ([]gate.FileEntry, error) {
	infos, err := remote.ReadDir(path)
	if err != nil {
		return nil, err
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name() < infos[j].Name() })
	if len(infos) > maxListEntries {
		infos = infos[:maxListEntries]
	}
	entries := make([]gate.FileEntry, len(infos))
	for index, info := range infos {
		entries[index] = gate.FileEntry{Name: info.Name(), Size: info.Size(), Dir: info.IsDir()}
	}
	return entries, nil
}

// readRemoteFile reads the start of a file as text for a model, replacing bytes
// that are not UTF-8, and says whether the file was longer than what it read.
func readRemoteFile(remote sftpclient.Remote, path string, maxBytes int) (string, bool, error) {
	data, truncated, err := sftpclient.ReadAll(remote, path, min(maxBytes, maxReadChars))
	if err != nil {
		return "", false, err
	}
	return strings.ToValidUTF8(string(data), "�"), truncated, nil
}
