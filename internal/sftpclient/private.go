package sftpclient

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"motty/internal/scpclient"
)

// privateRemote is what a backend needs to keep files for their owner alone:
// SFTP and SCP both have it, though only SFTP can open a file for writing.
type privateRemote interface {
	Remote
	Lstat(path string) (os.FileInfo, error)
	Mkdir(path string) error
	Chmod(path string, mode os.FileMode) error
	Remove(path string) error
}

// EnsurePrivateDir makes sure the directory relative names, below base, exists,
// and returns its path. Base must be there already. What is missing of the
// directory is created closed to everyone but its owner; what was already there
// is left as it is.
func EnsurePrivateDir(r Remote, base, relative string) (string, error) {
	p, ok := r.(privateRemote)
	if !ok {
		return "", ErrUnsupported
	}
	if !path.IsAbs(base) {
		return "", fmt.Errorf("sftp: %s is not an absolute path", base)
	}
	current := path.Clean(base)
	for _, part := range strings.Split(relative, "/") {
		if part == "" || part == "." || part == ".." {
			continue
		}
		current = path.Join(current, part)
		info, err := p.Lstat(current)
		switch {
		case err == nil:
			// A link is left to the one who made it: this is their own tree.
			if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return "", fmt.Errorf("sftp: %s is not a directory", current)
			}
			continue
		case !errors.Is(err, os.ErrNotExist):
			return "", err
		}
		if err := p.Mkdir(current); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		// A server that keeps the umask is no worse off than before; the
		// directory is only ever a layer of defence on top of the file's mode.
		_ = p.Chmod(current, 0o700)
	}
	return current, nil
}

// WritePrivateFile creates a new file that only its owner can read. It never
// replaces a file: ErrExists says the name is taken.
func WritePrivateFile(r Remote, name string, content []byte) error {
	p, ok := r.(privateRemote)
	if !ok {
		return ErrUnsupported
	}
	if _, err := p.Lstat(name); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if scp, ok := r.(*scpRemote); ok {
		return scp.writeNew(name, content, 0o600)
	}
	if err := WriteFile(r, name, content, false); err != nil {
		return err
	}
	return p.Chmod(name, 0o600)
}

// writeNew sends one file as `scp -t` would take it.
func (r *scpRemote) writeNew(name string, content []byte, mode os.FileMode) error {
	return r.client.Upload(path.Dir(name), false, func(sender *scpclient.Sender) error {
		return sender.File(path.Base(name), mode, time.Time{}, int64(len(content)), bytes.NewReader(content))
	})
}

// PruneFiles removes the plain files of a directory that match and were last
// written before cutoff, and reports how many went. A file that cannot be removed
// is skipped, and a directory that cannot be read is an error.
func PruneFiles(r Remote, directory string, match func(name string) bool, cutoff time.Time) (int, error) {
	p, ok := r.(privateRemote)
	if !ok {
		return 0, ErrUnsupported
	}
	entries, err := p.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.Mode().IsRegular() || !match(entry.Name()) || !entry.ModTime().Before(cutoff) {
			continue
		}
		if p.Remove(path.Join(directory, entry.Name())) == nil {
			removed++
		}
	}
	return removed, nil
}
