package sftpclient

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

const MaxEditFileBytes = 1 << 20

const MaxPreviewFileBytes = 16 << 20

var ErrPreviewFileTooLarge = errors.New("file exceeds the 16 MiB preview limit")
var ErrNotRegularFile = errors.New("sftp: preview requires a regular file")

func editSizeError(limit int) error {
	if limit == MaxEditFileBytes {
		return ErrEditFileTooLarge
	}
	return ErrPreviewFileTooLarge
}

var (
	ErrEditFileTooLarge    = errors.New("sftp: remote text file exceeds 1 MiB")
	ErrUnsupportedEncoding = errors.New("sftp: remote file is not UTF-8 text")
	ErrRemoteChanged       = errors.New("sftp: remote file changed since it was opened")
)

var utf8ByteOrderMark = []byte{0xef, 0xbb, 0xbf}

// Revision is the content identity captured when a remote text file is opened.
type Revision [sha256.Size]byte

// TextDocument is a bounded UTF-8 remote file prepared for an application-owned
// editor. UTF8BOM preserves an existing byte-order mark without exposing it as
// editable text.
type TextDocument struct {
	Text     string
	Revision Revision
	UTF8BOM  bool
}

func (worker *Worker) openText(client Remote, remotePath string, generation uint64) (TextDocument, error) {
	content, err := worker.readEditBytes(client, remotePath, generation)
	if err != nil {
		return TextDocument{}, err
	}
	return decodeTextDocument(content)
}

func (worker *Worker) saveText(client Remote, remotePath, value string, revision Revision, utf8BOM bool, generation uint64) (TextDocument, error) {
	content, err := encodeTextDocument(value, utf8BOM)
	if err != nil {
		return TextDocument{}, err
	}
	if err = worker.replaceDocumentBytes(client, remotePath, content, revision, generation); err != nil {
		return TextDocument{}, err
	}
	return decodeTextDocument(content)
}

// replaceDocumentBytes saves content over a remote file the user opened at
// revision. It replaces the file atomically through a sibling temporary file,
// which needs write access to the directory. Where the directory refuses
// that but the file itself is writable (a root-owned /etc/app with a
// group-writable config), it writes the file in place instead, as vim does
// under backupcopy=auto. In-place writing also keeps owner, ACLs and hard
// links, at the cost of atomicity.
func (worker *Worker) replaceDocumentBytes(client Remote, remotePath string, content []byte, revision Revision, generation uint64) error {
	advanced, ok := client.(transferRemote)
	if !ok {
		if IsSCP(client) {
			return ErrSCPNoEdit
		}
		return fmt.Errorf("sftp: atomic remote file replacement is not supported")
	}
	if err := worker.checkDocumentRevision(client, remotePath, revision, generation); err != nil {
		return err
	}
	info, exists, err := remoteLstat(advanced, remotePath)
	if err != nil {
		return err
	}
	if !exists || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrRemoteChanged, remotePath)
	}
	err = worker.replaceDocumentAtomically(client, advanced, remotePath, content, revision, info, generation)
	if !errors.Is(err, os.ErrPermission) {
		return err
	}
	// Nothing was changed: the temporary file could not be created, or could not
	// be moved over the original, and a leftover is removed. Try the file itself;
	// if that is refused too the permission error is the real answer.
	return worker.replaceDocumentInPlace(client, advanced, remotePath, content, revision, generation)
}

// checkDocumentRevision reports ErrRemoteChanged unless the remote file still
// has the content the editor opened.
func (worker *Worker) checkDocumentRevision(client Remote, remotePath string, revision Revision, generation uint64) error {
	current, err := worker.readFileBytes(client, remotePath, generation, MaxPreviewFileBytes)
	if err != nil {
		if editReadMeansConflict(err) {
			return fmt.Errorf("%w: %s", ErrRemoteChanged, remotePath)
		}
		return err
	}
	if Revision(sha256.Sum256(current)) != revision {
		return fmt.Errorf("%w: %s", ErrRemoteChanged, remotePath)
	}
	return nil
}

func (worker *Worker) replaceDocumentAtomically(client Remote, advanced transferRemote, remotePath string, content []byte, revision Revision, original os.FileInfo, generation uint64) (err error) {
	temporary, target, err := openEditTemporary(advanced, remotePath)
	if err != nil {
		return err
	}
	committed := false
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, temporary.Close())
		}
		if !committed {
			_ = advanced.Remove(target)
		}
	}()
	if err = temporary.Truncate(0); err != nil {
		return err
	}
	if _, err = temporary.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = worker.copy(temporary, bytes.NewReader(content), generation, 0, 0, 0, 0); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil && !isUnsupportedSFTP(err) {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	closed = true
	if privileged, ok := client.(interface {
		preserveEditOwner(string, os.FileInfo) error
	}); ok {
		if err = privileged.preserveEditOwner(target, original); err != nil {
			return err
		}
	}
	if err = advanced.Chmod(target, original.Mode().Perm()); err != nil {
		return err
	}
	if err = worker.checkDocumentRevision(client, remotePath, revision, generation); err != nil {
		return err
	}
	if worker.canceled(generation) {
		return ErrCanceled
	}
	if err = commitRemote(advanced, target, remotePath, true); err != nil {
		return err
	}
	committed = true
	return nil
}

// replaceDocumentInPlace truncates and rewrites the file itself. The revision is
// checked once more right before the truncation, the last point at which a
// concurrent change can still be told apart from our own write.
func (worker *Worker) replaceDocumentInPlace(client Remote, advanced transferRemote, remotePath string, content []byte, revision Revision, generation uint64) (err error) {
	if err = worker.checkDocumentRevision(client, remotePath, revision, generation); err != nil {
		return err
	}
	if worker.canceled(generation) {
		return ErrCanceled
	}
	file, err := advanced.OpenTransferFile(remotePath, os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err = worker.copy(file, bytes.NewReader(content), generation, 0, 0, 0, 0); err != nil {
		return err
	}
	if err = file.Sync(); err != nil && !isUnsupportedSFTP(err) {
		return err
	}
	return nil
}

func editReadMeansConflict(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrEditFileTooLarge) || errors.Is(err, ErrPreviewFileTooLarge) || errors.Is(err, ErrUnsupportedEncoding) || errors.Is(err, ErrNotRegularFile)
}

func (worker *Worker) readEditBytes(client Remote, remotePath string, generation uint64) ([]byte, error) {
	return worker.readFileBytes(client, remotePath, generation, MaxEditFileBytes)
}

func (worker *Worker) readFileBytes(client Remote, remotePath string, generation uint64, limit int) ([]byte, error) {
	if remotePath == "" {
		return nil, fmt.Errorf("sftp: remote text path is empty")
	}
	if advanced, ok := client.(transferRemote); ok {
		info, exists, err := remoteLstat(advanced, remotePath)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, os.ErrNotExist
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			if limit != MaxEditFileBytes {
				return nil, fmt.Errorf("%w: %s", ErrNotRegularFile, remotePath)
			}
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedEncoding, remotePath)
		}
		if info.Size() > int64(limit) {
			return nil, editSizeError(limit)
		}
	}
	source, err := client.Open(remotePath)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	pair := bufferPool.Get().(*[2][32 * 1024]byte)
	defer bufferPool.Put(pair)
	buffer := pair[0][:]
	content := make([]byte, 0, min(limit, 32*1024))
	for {
		if worker.canceled(generation) {
			return nil, ErrCanceled
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			if len(content)+count > limit {
				return nil, editSizeError(limit)
			}
			content = append(content, buffer[:count]...)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return content, nil
			}
			return nil, readErr
		}
		if count == 0 {
			return nil, io.ErrNoProgress
		}
	}
}

func decodeTextDocument(content []byte) (TextDocument, error) {
	document := TextDocument{Revision: Revision(sha256.Sum256(content))}
	if bytes.HasPrefix(content, utf8ByteOrderMark) {
		document.UTF8BOM = true
		content = content[len(utf8ByteOrderMark):]
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return TextDocument{}, ErrUnsupportedEncoding
	}
	document.Text = string(content)
	return document, nil
}

func encodeTextDocument(value string, utf8BOM bool) ([]byte, error) {
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return nil, ErrUnsupportedEncoding
	}
	size := len(value)
	if utf8BOM {
		size += len(utf8ByteOrderMark)
	}
	if size > MaxEditFileBytes {
		return nil, ErrEditFileTooLarge
	}
	content := make([]byte, 0, size)
	if utf8BOM {
		content = append(content, utf8ByteOrderMark...)
	}
	content = append(content, value...)
	return content, nil
}

func openEditTemporary(client transferRemote, remotePath string) (TransferFile, string, error) {
	// Do not append to the source basename: it may already use NAME_MAX bytes.
	// Keep the temporary sibling short while separating simultaneous targets.
	identity := sha256.Sum256([]byte(remotePath))
	directory := path.Dir(remotePath)
	for attempt := 0; attempt < 16; attempt++ {
		target := path.Join(directory, fmt.Sprintf(".motty.edit-%x-%02x.part", identity[:8], attempt))
		file, err := client.OpenTransferFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err == nil {
			return file, target, nil
		}
		if !errors.Is(err, os.ErrExist) && !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("sftp: cannot reserve temporary file for %s", remotePath)
}
