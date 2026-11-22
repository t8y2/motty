package sftpclient

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"motty/internal/scpclient"
)

// ErrSCPNoEdit reports that saving an edited file needs SFTP: the SCP backend
// cannot replace a file atomically.
var ErrSCPNoEdit = errors.New("scp: saving edits needs SFTP; this server only offers SCP")

// scpRemote is the file backend for a server without an SFTP subsystem. Every
// operation is one exec channel: transfers run `scp`, the rest a fixed command
// with strictly quoted paths. The listing is whatever `ls` prints, so it is
// approximate.
type scpRemote struct {
	start  scpclient.Start
	client *scpclient.Client

	mu     sync.Mutex
	active map[*trackedChannel]struct{}
	closed bool
	// links are the targets the latest listing showed, so resolving them costs
	// no further command.
	links map[string]string
}

// OpenSCP returns the SCP backend over an authenticated SSH connection. It
// starts nothing: each operation opens its own exec channel.
func OpenSCP(client *ssh.Client) (Remote, error) {
	if client == nil {
		return nil, fmt.Errorf("scp: SSH client is not connected")
	}
	return NewSCP(scpclient.SSH(client)), nil
}

// NewSCP returns the SCP backend over any command starter.
func NewSCP(start scpclient.Start) Remote {
	remote := &scpRemote{start: start, active: map[*trackedChannel]struct{}{}}
	remote.client = scpclient.New(remote.track)
	return remote
}

// IsSCP reports whether remote is the SCP backend, whose listings are approximate.
func IsSCP(remote Remote) bool {
	_, ok := remote.(*scpRemote)
	return ok
}

// IsSubsystemRefused reports an error that means the server has no usable SFTP
// subsystem, as opposed to a connection that failed.
func IsSubsystemRefused(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "subsystem request failed") ||
		strings.Contains(text, "subsystem") && (strings.Contains(text, "denied") || strings.Contains(text, "refused") || strings.Contains(text, "not allowed")) ||
		// The request was accepted but the server process died before it spoke.
		strings.Contains(text, "sftp: start subsystem") && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(text, "eof"))
}

type trackedChannel struct {
	scpclient.Channel
	remote *scpRemote
}

func (c *trackedChannel) Close() error {
	c.remote.mu.Lock()
	delete(c.remote.active, c)
	c.remote.mu.Unlock()
	return c.Channel.Close()
}

func (r *scpRemote) track(command string) (scpclient.Channel, error) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, ErrCanceled
	}
	channel, err := r.start(command)
	if err != nil {
		return nil, err
	}
	tracked := &trackedChannel{Channel: channel, remote: r}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = channel.Close()
		return nil, ErrCanceled
	}
	r.active[tracked] = struct{}{}
	r.mu.Unlock()
	return tracked, nil
}

// Close aborts whatever is still running.
func (r *scpRemote) Close() error {
	r.mu.Lock()
	r.closed = true
	running := make([]*trackedChannel, 0, len(r.active))
	for channel := range r.active {
		running = append(running, channel)
	}
	r.mu.Unlock()
	for _, channel := range running {
		_ = channel.Close()
	}
	return nil
}

// scpInfo is a listing line as an os.FileInfo.
type scpInfo struct {
	name string
	item scpclient.Item
}

func (i scpInfo) Name() string       { return i.name }
func (i scpInfo) Size() int64        { return i.item.Size }
func (i scpInfo) Mode() fs.FileMode  { return i.item.Mode }
func (i scpInfo) ModTime() time.Time { return i.item.ModTime }
func (i scpInfo) IsDir() bool        { return i.item.Mode.IsDir() }
func (i scpInfo) Sys() any {
	return &sftp.FileStat{UID: i.item.UID, GID: i.item.GID}
}

func (r *scpRemote) Getwd() (string, error) {
	out, err := r.client.Run("pwd", 4096)
	if err != nil {
		return "", err
	}
	directory := strings.TrimSpace(string(out))
	if !path.IsAbs(directory) {
		return "", fmt.Errorf("scp: the server did not report a working directory")
	}
	return directory, nil
}

func (r *scpRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	items, err := r.client.List(directory)
	if err != nil {
		return nil, err
	}
	infos := make([]os.FileInfo, len(items))
	links := map[string]string{}
	for index, item := range items {
		infos[index] = scpInfo{name: item.Name, item: item}
		if item.LinkTarget != "" {
			links[path.Join(directory, item.Name)] = item.LinkTarget
		}
	}
	r.mu.Lock()
	r.links = links
	r.mu.Unlock()
	return infos, nil
}

func (r *scpRemote) Lstat(name string) (os.FileInfo, error) {
	item, err := r.client.Stat(name, false)
	if err != nil {
		return nil, err
	}
	return scpInfo{name: item.Name, item: item}, nil
}

func (r *scpRemote) Stat(name string) (os.FileInfo, error) {
	item, err := r.client.Stat(name, true)
	if err != nil {
		return nil, err
	}
	return scpInfo{name: item.Name, item: item}, nil
}

func (r *scpRemote) ReadLink(name string) (string, error) {
	r.mu.Lock()
	target, known := r.links[name]
	r.mu.Unlock()
	if known {
		return target, nil
	}
	item, err := r.client.Stat(name, false)
	if err != nil {
		return "", err
	}
	if item.Mode&os.ModeSymlink == 0 {
		return "", fmt.Errorf("scp: %s is not a symbolic link", name)
	}
	return item.LinkTarget, nil
}

func (r *scpRemote) run(format string, args ...string) error {
	command, err := scpclient.Command(format, args...)
	if err != nil {
		return err
	}
	_, err = r.client.Run(command, 64<<10)
	return err
}

func (r *scpRemote) Mkdir(name string) error { return r.run("mkdir -p -- {}", name) }

// Remove and RemoveDirectory run under the C locale like the listing commands:
// their errors are matched by message, and a localized server would not say the
// English words scpclient maps to os.ErrNotExist (and IsPathNotFound looks for).
func (r *scpRemote) Remove(name string) error {
	return r.run("LC_ALL=C rm -- {}", name)
}

func (r *scpRemote) RemoveDirectory(name string) error {
	return r.run("LC_ALL=C rmdir -- {}", name)
}
func (r *scpRemote) Symlink(target, link string) error { return r.run("ln -s -- {} {}", target, link) }

// Rename never replaces: like SFTP's rename it fails when the destination is there.
func (r *scpRemote) Rename(oldPath, newPath string) error {
	return r.run(`if [ -e {} ] || [ -L {} ]; then echo "rename: destination exists" >&2; exit 1; fi; mv -- {} {}`,
		newPath, newPath, oldPath, newPath)
}

func (r *scpRemote) Chmod(name string, mode os.FileMode) error {
	command, err := scpclient.Command(fmt.Sprintf("chmod %04o -- {}", mode.Perm()), name)
	if err != nil {
		return err
	}
	_, err = r.client.Run(command, 64<<10)
	return err
}

// Open streams a remote file. Closing the reader early aborts the transfer.
func (r *scpRemote) Open(name string) (io.ReadCloser, error) {
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := r.client.Download(name, false, pipeHandler{writer})
		writer.CloseWithError(err) // nil reads as io.EOF
	}()
	return &pipeReader{PipeReader: reader, done: done}, nil
}

type pipeReader struct {
	*io.PipeReader
	done chan struct{}
}

func (p *pipeReader) Close() error {
	err := p.PipeReader.CloseWithError(ErrCanceled)
	<-p.done
	return err
}

type pipeHandler struct{ writer *io.PipeWriter }

func (pipeHandler) Dir(string, fs.FileMode, time.Time) error {
	return errors.New("scp: that is a directory")
}

func (h pipeHandler) File(string, fs.FileMode, time.Time, int64) (scpclient.FileWriter, error) {
	return pipeFile{h.writer}, nil
}

type pipeFile struct{ writer *io.PipeWriter }

func (f pipeFile) Write(p []byte) (int, error) { return f.writer.Write(p) }
func (pipeFile) Commit() error                 { return nil }
func (pipeFile) Abort()                        {}
