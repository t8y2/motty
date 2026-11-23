package sftpclient

import (
	"errors"
	"fmt"
	"os"
)

// A folder transfer moves a symbolic link inside it as a link: the target text
// is read at one end and written at the other, the way cp -R, rsync -a and tar
// do. The target is never opened, so a link that points out of the folder or
// back up the tree reads nothing it should not, and a bundle such as a macOS
// .app, whose frameworks are made of links, arrives intact. A link the user
// selected themselves is still refused: it is not a part of a folder.

// remoteLinkStore is what writing a link at a server needs.
type remoteLinkStore interface {
	Lstat(path string) (os.FileInfo, error)
	Remove(path string) error
	ReadLink(path string) (string, error)
	Symlink(target, link string) error
}

// putRemoteLink makes a link at remotePath that points at target. A link
// already there is kept when it points at the same place, and otherwise
// replaced unless the policy refuses or skips it; anything that is not a link
// is a conflict, so a file or a folder is never removed to make room.
func putRemoteLink(client Remote, remotePath, target string, policy ExistingPolicy) error {
	store, ok := client.(remoteLinkStore)
	if !ok {
		return fmt.Errorf("sftp: symbolic links are not supported by the remote: %s", remotePath)
	}
	info, err := store.Lstat(remotePath)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink == 0 || policy == ExistingFail {
			return &ConflictError{Path: remotePath}
		}
		if policy == ExistingSkip {
			return nil
		}
		if current, err := store.ReadLink(remotePath); err == nil && current == target {
			return nil
		}
		if err := store.Remove(remotePath); err != nil {
			return err
		}
	case !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err):
		return err
	}
	return store.Symlink(target, remotePath)
}

// putLocalLink is putRemoteLink for this computer.
func putLocalLink(localPath, target string, policy ExistingPolicy) error {
	info, exists, err := localLstat(localPath)
	if err != nil {
		return err
	}
	if exists {
		if info.Mode()&os.ModeSymlink == 0 || policy == ExistingFail {
			return &ConflictError{Path: localPath}
		}
		if policy == ExistingSkip {
			return nil
		}
		if current, err := os.Readlink(localPath); err == nil && current == target {
			return nil
		}
		if err := os.Remove(localPath); err != nil {
			return err
		}
	}
	return os.Symlink(target, localPath)
}
