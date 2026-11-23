package sftpclient

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"motty/internal/scpclient"
)

// The SCP transfers differ from the SFTP ones in what the protocol allows: a
// file is written under its final name (there is no staged replacement and no
// resume, so ExistingResume acts as Replace), the totals of a download are not
// known beforehand, and for a folder upload the existing-file policy applies to
// the folder as a whole.

// cancelReader checks for cancellation and pause before each read.
type cancelReader struct {
	worker     *Worker
	reader     io.Reader
	generation uint64
	id         uint64
	onRead     func(int)
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if !r.worker.proceed(r.generation, r.id) {
		return 0, ErrCanceled
	}
	count, err := r.reader.Read(p)
	if count > 0 {
		r.onRead(count)
	}
	return count, err
}

// proceed waits while the transfer is paused and reports whether it goes on.
func (worker *Worker) proceed(generation, id uint64) bool {
	if id != 0 {
		return worker.holdWhilePaused(generation, id)
	}
	return !worker.canceled(generation)
}

type scpDownloader struct {
	worker     *Worker
	next       request
	generation uint64
	files      int
	bytes      int64
	root       bool // the root is a directory
}

func (worker *Worker) scpDownload(remote *scpRemote, next request, generation uint64) (files int, bytes int64, resumed int64, err error) {
	downloader := &scpDownloader{worker: worker, next: next, generation: generation}
	stats, err := remote.client.Download(next.remote, next.recursive, downloader)
	return stats.Files, stats.Bytes, 0, err
}

func (d *scpDownloader) Dir(relative string, mode fs.FileMode, _ time.Time) error {
	if !d.worker.proceed(d.generation, d.next.id) {
		return ErrCanceled
	}
	if relative == "." {
		d.root = true
		return ensureLocalRoot(d.next.local, d.next.policy)
	}
	local, err := joinLocalWithin(d.next.local, relative)
	if err != nil {
		return err
	}
	return ensureLocalDirectory(local, mode)
}

func (d *scpDownloader) File(relative string, mode fs.FileMode, modTime time.Time, size int64) (scpclient.FileWriter, error) {
	if !d.worker.proceed(d.generation, d.next.id) {
		return nil, ErrCanceled
	}
	local := d.next.local
	if relative != "." {
		var err error
		if local, err = joinLocalWithin(d.next.local, relative); err != nil {
			return nil, err
		}
	} else {
		d.worker.setTotals(d.next.id, 1, size)
	}
	finalInfo, finalExists, err := localLstat(local)
	if err != nil {
		return nil, err
	}
	skip := false
	if finalExists {
		switch {
		case finalInfo.Mode()&os.ModeSymlink != 0:
			return nil, fmt.Errorf("%w: %s", ErrSymbolicLink, local)
		case finalInfo.IsDir() || d.next.policy == ExistingFail:
			return nil, &ConflictError{Path: local}
		case d.next.policy == ExistingSkip:
			skip = true
		}
	}
	file := &scpLocalFile{d: d, final: local, mode: mode, modTime: modTime, size: size}
	if skip {
		file.skipped = true
		return file, nil
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return nil, err
	}
	partial := localPartialPath(local)
	if info, exists, err := localLstat(partial); err != nil {
		return nil, err
	} else if exists && (info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return nil, fmt.Errorf("%w: %s", ErrSymbolicLink, partial)
	}
	file.partial = partial
	file.file, err = os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return file, nil
}

type scpLocalFile struct {
	d       *scpDownloader
	final   string
	partial string
	file    *os.File
	mode    fs.FileMode
	modTime time.Time
	size    int64
	written int64
	skipped bool
}

func (f *scpLocalFile) Write(p []byte) (int, error) {
	if !f.d.worker.proceed(f.d.generation, f.d.next.id) {
		return 0, ErrCanceled
	}
	count := len(p)
	if !f.skipped {
		var err error
		if count, err = f.file.Write(p); err != nil {
			return count, err
		}
	}
	f.written += int64(count)
	f.d.worker.setProgress(f.d.next.id, f.d.files, f.d.bytes+f.written, 0)
	return count, nil
}

func (f *scpLocalFile) Commit() error {
	d := f.d
	defer func() { d.files++; d.bytes += f.size }()
	if f.skipped {
		return nil
	}
	file := f.file
	f.file = nil
	if err := file.Sync(); err != nil {
		file.Close()
		_ = os.Remove(f.partial)
		return err
	}
	if err := file.Chmod(f.mode.Perm()); err != nil {
		file.Close()
		_ = os.Remove(f.partial)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(f.partial)
		return err
	}
	if err := replaceLocalFile(f.partial, f.final); err != nil {
		_ = os.Remove(f.partial)
		return err
	}
	if d.worker.preserveTimes() && !f.modTime.IsZero() {
		_ = os.Chtimes(f.final, f.modTime, f.modTime)
	}
	return nil
}

func (f *scpLocalFile) Abort() {
	if f.file != nil {
		f.file.Close()
		f.file = nil
		_ = os.Remove(f.partial)
	}
}

func (worker *Worker) scpUpload(remote *scpRemote, next request, generation uint64) (files int, bytes int64, resumed int64, err error) {
	if next.local == "" || next.remote == "" {
		return 0, 0, 0, fmt.Errorf("scp: upload requires local and remote paths")
	}
	info, totalFiles, totalBytes, err := scanLocal(next.local, next.recursive)
	if err != nil {
		return 0, 0, 0, err
	}
	worker.setTotals(next.id, totalFiles, totalBytes)
	target := path.Clean(next.remote)
	parent, name := path.Dir(target), path.Base(target)
	if name == "/" || name == "." {
		return 0, 0, 0, fmt.Errorf("scp: cannot upload to %s", next.remote)
	}
	if existing, statErr := remote.Lstat(target); statErr == nil {
		switch {
		case existing.Mode()&os.ModeSymlink != 0:
			return 0, 0, 0, fmt.Errorf("%w: %s", ErrSymbolicLink, target)
		case existing.IsDir() != info.IsDir():
			return 0, 0, 0, &ConflictError{Path: target}
		case next.policy == ExistingFail:
			return 0, 0, 0, &ConflictError{Path: target}
		case next.policy == ExistingSkip:
			worker.setProgress(next.id, totalFiles, totalBytes, totalBytes)
			return totalFiles, totalBytes, totalBytes, nil
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return 0, 0, 0, statErr
	}
	reader := func(file io.Reader) io.Reader {
		return &cancelReader{worker: worker, reader: file, generation: generation, id: next.id, onRead: func(count int) {
			bytes += int64(count)
			worker.setProgress(next.id, files, bytes, 0)
		}}
	}
	// The scp protocol has no link record, so the links of a folder are made
	// with ln once the folder is there.
	type scpLink struct{ remote, target string }
	var links []scpLink
	err = remote.client.Upload(parent, info.IsDir(), func(sender *scpclient.Sender) error {
		var sendFile func(local, name string, info os.FileInfo) error
		sendFile = func(local, name string, info os.FileInfo) error {
			if !worker.proceed(generation, next.id) {
				return ErrCanceled
			}
			modTime := time.Time{}
			if worker.preserveTimes() {
				modTime = info.ModTime()
			}
			if info.Mode()&os.ModeSymlink != 0 {
				relative, err := filepath.Rel(next.local, local)
				if err != nil {
					return err
				}
				destination, err := joinRemoteWithin(target, filepath.ToSlash(relative))
				if err != nil {
					return err
				}
				text, err := os.Readlink(local)
				if err != nil {
					return err
				}
				links = append(links, scpLink{remote: destination, target: text})
				return nil
			}
			if !info.IsDir() {
				if !info.Mode().IsRegular() {
					return fmt.Errorf("scp: unsupported local file type: %s", local)
				}
				source, err := os.Open(local)
				if err != nil {
					return err
				}
				defer source.Close()
				if err := sender.File(name, info.Mode(), modTime, info.Size(), reader(source)); err != nil {
					return err
				}
				files++
				worker.setProgress(next.id, files, bytes, 0)
				return nil
			}
			if err := sender.EnterDir(name, info.Mode(), modTime); err != nil {
				return err
			}
			entries, err := os.ReadDir(local)
			if err != nil {
				return err
			}
			slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
			for _, entry := range entries {
				childInfo, err := entry.Info()
				if err != nil {
					return err
				}
				if err := sendFile(filepath.Join(local, entry.Name()), entry.Name(), childInfo); err != nil {
					return err
				}
			}
			return sender.LeaveDir()
		}
		return sendFile(next.local, name, info)
	})
	if err != nil {
		return files, bytes, 0, err
	}
	for _, link := range links {
		if !worker.proceed(generation, next.id) {
			return files, bytes, 0, ErrCanceled
		}
		if err := putRemoteLink(remote, link.remote, link.target, next.policy); err != nil {
			return files, bytes, 0, err
		}
	}
	return files, bytes, 0, nil
}
