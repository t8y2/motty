package sftpclient

import (
	"errors"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
)

// A folder transfer moves a symbolic link inside it as a link: the target text
// is read at one end and written at the other, the way cp -R, rsync -a and tar
// do. The target is never opened, so a link that points out of the folder or
// back up the tree reads nothing it should not, and a bundle such as a macOS
// .app, whose frameworks are made of links, arrives intact. A link the user
// selected themselves is still refused: it is not a part of a folder.

// ReadLocalLink reports whether info at localPath is a symbolic link or, on
// Windows, an NTFS directory junction / mount-point reparse point (which Go
// reports with ModeIrregular rather than ModeSymlink, and which Node/Nitro and
// pnpm create inside .output/server/node_modules when symbolic-link privilege
// is absent), returning its stored target text.
func ReadLocalLink(localPath string, info os.FileInfo) (target string, isLink bool, err error) {
	if info == nil {
		return "", false, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(localPath)
		return target, true, err
	}
	if runtime.GOOS == "windows" && info.Mode()&os.ModeIrregular != 0 {
		if target, err := os.Readlink(localPath); err == nil {
			return target, true, nil
		}
	}
	return "", false, nil
}

// remoteLinkTargetFromLocal formats a local link's target for the remote SFTP
// server. Real symbolic links keep their stored target text verbatim. Windows
// NTFS junctions always record an absolute Windows path on disk even when
// linking two folders inside the same build output (such as Nuxt's
// .output/server/node_modules); when that junction target lies within the
// uploaded root, rewriting it to a slash-separated path relative to the link's
// parent lets the uploaded bundle resolve on a POSIX server without reading
// outside the root. A junction whose target lies outside the uploaded root
// cannot be represented portably on the remote server and is rejected.
func remoteLinkTargetFromLocal(root, localPath, target string, info os.FileInfo) (string, error) {
	if runtime.GOOS == "windows" && info != nil && info.Mode()&os.ModeSymlink == 0 && info.Mode()&os.ModeIrregular != 0 {
		rel, ok := normalizeWindowsJunctionTarget(root, localPath, target)
		if !ok {
			return "", fmt.Errorf("%w: junction %s points outside transfer root", ErrSymbolicLink, localPath)
		}
		return rel, nil
	}
	return target, nil
}

func normalizeWindowsJunctionTarget(root, localPath, target string) (string, bool) {
	cleanedTarget := toWindowsSlashPath(stripWindowsDevicePrefix(target))
	cleanedRoot := toWindowsSlashPath(stripWindowsDevicePrefix(root))
	linkParent := path.Dir(toWindowsSlashPath(stripWindowsDevicePrefix(localPath)))
	if cleanedRoot != "" && cleanedTarget != "" {
		if relToRoot, ok := windowsRelativePath(cleanedRoot, cleanedTarget); ok &&
			relToRoot != ".." && !strings.HasPrefix(relToRoot, "../") {
			if rel, ok := windowsRelativePath(linkParent, cleanedTarget); ok {
				return rel, true
			}
		}
	}
	return "", false
}

func toWindowsSlashPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(strings.ReplaceAll(p, `\`, "/"))
}

func splitWindowsVolume(p string) (volume, rest string) {
	if len(p) >= 2 && p[1] == ':' && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z')) {
		return strings.ToUpper(p[:2]), strings.TrimPrefix(p[2:], "/")
	}
	if strings.HasPrefix(p, "//") {
		trimmed := strings.TrimPrefix(p, "//")
		parts := strings.SplitN(trimmed, "/", 3)
		if len(parts) >= 2 {
			vol := "//" + strings.ToLower(parts[0]) + "/" + strings.ToLower(parts[1])
			if len(parts) == 3 {
				return vol, parts[2]
			}
			return vol, ""
		}
	}
	return "", strings.TrimPrefix(p, "/")
}

func windowsRelativePath(base, target string) (string, bool) {
	baseVol, baseRest := splitWindowsVolume(base)
	targetVol, targetRest := splitWindowsVolume(target)
	if baseVol != targetVol {
		return "", false
	}
	if (strings.HasPrefix(base, "/") && !strings.HasPrefix(target, "/")) ||
		(!strings.HasPrefix(base, "/") && strings.HasPrefix(target, "/")) {
		if baseVol == "" {
			return "", false
		}
	}
	var baseParts, targetParts []string
	if baseRest != "" && baseRest != "." {
		baseParts = strings.Split(baseRest, "/")
	}
	if targetRest != "" && targetRest != "." {
		targetParts = strings.Split(targetRest, "/")
	}
	common := 0
	for common < len(baseParts) && common < len(targetParts) && strings.EqualFold(baseParts[common], targetParts[common]) {
		common++
	}
	up := len(baseParts) - common
	out := make([]string, 0, up+len(targetParts)-common)
	for range up {
		out = append(out, "..")
	}
	out = append(out, targetParts[common:]...)
	if len(out) == 0 {
		return ".", true
	}
	return strings.Join(out, "/"), true
}

func stripWindowsDevicePrefix(p string) string {
	switch {
	case strings.HasPrefix(p, `\??\`):
		return p[4:]
	case strings.HasPrefix(p, `\\?\`) && !strings.HasPrefix(strings.ToUpper(p), `\\?\UNC\`):
		return p[4:]
	default:
		return p
	}
}

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
	return putRemoteLinkKnown(client, remotePath, target, policy, nil, false, false)
}

// putRemoteLinkKnown is putRemoteLink when the caller may already know from its
// parent folder's listing (or from having just created the parent folder)
// whether remotePath exists, avoiding a per-link Lstat round trip.
func putRemoteLinkKnown(client Remote, remotePath, target string, policy ExistingPolicy, info os.FileInfo, exists, known bool) error {
	store, ok := client.(remoteLinkStore)
	if !ok {
		return fmt.Errorf("sftp: symbolic links are not supported by the remote: %s", remotePath)
	}
	if !known {
		var err error
		info, err = store.Lstat(remotePath)
		switch {
		case err == nil:
			exists = true
		case !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err):
			return err
		}
	}
	if exists {
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
