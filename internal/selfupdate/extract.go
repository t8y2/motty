package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// What a package of this size may hold. The packages are tens of megabytes and a
// few files; these only stop a bad archive from filling the disk.
const (
	maxExtractFiles = 10000
	maxExtractBytes = 1 << 30
)

// extractor tracks the budget of one archive.
type extractor struct {
	dest  string
	files int
	bytes int64
}

// target is where an archive entry goes, or an error if it would land outside
// dest. Names are slash separated and relative.
func (x *extractor) target(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("selfupdate: the package holds a bad path %q", name)
	}
	target := filepath.Join(x.dest, filepath.FromSlash(name))
	relative, err := filepath.Rel(x.dest, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("selfupdate: the package holds a path outside itself: %q", name)
	}
	return target, nil
}

// write makes one regular file from reader. mode is the archive's; only the
// permission bits of an ordinary program or document survive.
func (x *extractor) write(target string, mode fs.FileMode, size int64, reader io.Reader) error {
	x.files++
	x.bytes += size
	if x.files > maxExtractFiles || x.bytes > maxExtractBytes || size < 0 {
		return errors.New("selfupdate: the package is larger than a release can be")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	// The header's size is a claim; the copy is cut at it so a lying header
	// cannot write more.
	written, err := io.Copy(file, io.LimitReader(reader, size+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if written != size {
		return errors.New("selfupdate: a file in the package is not the size it says")
	}
	return os.Chmod(target, perm)
}

// ExtractZip unpacks a zip into dest, a directory that does not exist yet. Only
// directories and ordinary files are allowed; a link or a path that climbs out
// ends the unpacking.
func ExtractZip(archive, dest string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.Mkdir(dest, 0o700); err != nil {
		return err
	}
	x := &extractor{dest: dest}
	for _, entry := range reader.File {
		target, err := x.target(strings.TrimSuffix(entry.Name, "/"))
		if err != nil {
			return err
		}
		switch {
		case entry.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case entry.Mode().IsRegular():
			source, err := entry.Open()
			if err != nil {
				return err
			}
			err = x.write(target, entry.Mode(), int64(entry.UncompressedSize64), source)
			source.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("selfupdate: the package holds something that is not a file: %q", entry.Name)
		}
	}
	return nil
}

// ExtractTarGz unpacks a gzip-compressed tar into dest, which does not exist yet,
// under the same rules as ExtractZip.
func ExtractTarGz(archive, dest string) error {
	source, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer source.Close()
	zipped, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	defer zipped.Close()
	if err := os.Mkdir(dest, 0o700); err != nil {
		return err
	}
	x := &extractor{dest: dest}
	reader := tar.NewReader(zipped)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(strings.TrimPrefix(header.Name, "./"), "/")
		if name == "" || name == "." {
			continue // the archive's own root
		}
		target, err := x.target(name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := x.write(target, fs.FileMode(header.Mode), header.Size, reader); err != nil {
				return err
			}
		default:
			return fmt.Errorf("selfupdate: the package holds something that is not a file: %q", header.Name)
		}
	}
}
