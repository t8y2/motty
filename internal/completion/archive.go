package completion

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ArchiveLimits bounds one archive read. They are intentionally independent of
// the YAML import bounds: a member can be small and still be one of too many.
type ArchiveLimits struct {
	MaxCompressed int64
	MaxExpanded   int64
	MaxMembers    int
	MaxFileBytes  int64
	// MaxSourceBytes bounds the total of the source and license bytes kept in
	// memory. It is independent of MaxExpanded, which only bounds the work.
	MaxSourceBytes int64
}

// DefaultArchiveLimits returns the limits used for catalog updates.
func DefaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{
		MaxCompressed:  maxArchiveBytes,
		MaxExpanded:    maxExpandedBytes,
		MaxMembers:     maxArchiveMembers,
		MaxFileBytes:   maxSourceFileBytes,
		MaxSourceBytes: maxSourceBytes,
	}
}

// SourceArchive is the subset of a Carapace release archive the catalog needs.
type SourceArchive struct {
	Files   []SourceFile
	License []byte
}

var errArchiveTooLarge = errors.New("completion: archive exceeds the size limit")

// cappedReader fails once more than the allowed number of bytes has been read,
// so a hostile or endless download stops at the limit instead of filling memory.
type cappedReader struct {
	reader    io.Reader
	remaining int64
}

func (capped *cappedReader) Read(p []byte) (int, error) {
	n, err := capped.reader.Read(p)
	capped.remaining -= int64(n)
	if capped.remaining < 0 {
		return n, errArchiveTooLarge
	}
	return n, err
}

// ReadSourceArchive reads man/cmd/*.yaml and LICENSE from a gzipped or plain
// tar stream. Only regular files directly below man/cmd are read; symlinks,
// absolute paths and path traversal are refused. Nothing is extracted to disk.
func ReadSourceArchive(reader io.Reader, limits ArchiveLimits) (*SourceArchive, error) {
	if limits.MaxCompressed <= 0 {
		limits = DefaultArchiveLimits()
	}
	if limits.MaxSourceBytes <= 0 {
		limits.MaxSourceBytes = maxSourceBytes
	}
	// The stream is read once, as it arrives: the compressed archive is never
	// held in memory beside the sources taken from it.
	buffered := bufio.NewReader(&cappedReader{reader: reader, remaining: limits.MaxCompressed})
	var stream io.Reader = buffered
	if magic, err := buffered.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gzipReader, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, fmt.Errorf("completion: open gzip stream: %w", err)
		}
		defer gzipReader.Close()
		stream = gzipReader
	}

	archive := &SourceArchive{}
	kept := int64(0)
	expandedStream := &io.LimitedReader{R: stream, N: limits.MaxExpanded + 1}
	tarReader := tar.NewReader(expandedStream)
	var expanded int64
	members := 0
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("completion: read archive member: %w", err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		expanded += header.Size
		if expanded > limits.MaxExpanded {
			return nil, errors.New("completion: archive expands beyond the size limit")
		}
		name, ok := cleanArchiveName(header.Name)
		if !ok {
			continue
		}
		relative, source := sourceRelative(name)
		license := !source && isTopLevelLicense(name)
		if !source && !license {
			// Source code, documentation and tests in the release archive do
			// not contribute and do not count against the member limit.
			continue
		}
		members++
		if members > limits.MaxMembers {
			return nil, errors.New("completion: archive has too many source files")
		}
		if header.Size > limits.MaxFileBytes {
			return nil, fmt.Errorf("completion: archive member %s exceeds the file size limit", name)
		}
		if source {
			if archive.Files == nil {
				archive.Files = make([]SourceFile, 0, 8000)
			}
			if kept += header.Size; kept > limits.MaxSourceBytes {
				return nil, errors.New("completion: archive sources exceed the memory limit")
			}
			content := make([]byte, header.Size)
			if _, err := io.ReadFull(tarReader, content); err != nil {
				return nil, fmt.Errorf("completion: read %s: %w", name, err)
			}
			archive.Files = append(archive.Files, SourceFile{Name: relative, Data: content})
			continue
		}
		if archive.License == nil {
			if kept += header.Size; kept > limits.MaxSourceBytes {
				return nil, errors.New("completion: archive sources exceed the memory limit")
			}
			content := make([]byte, header.Size)
			if _, err := io.ReadFull(tarReader, content); err != nil {
				return nil, fmt.Errorf("completion: read %s: %w", name, err)
			}
			archive.License = content
		}
	}
	// tar stops at its end markers before gzip necessarily verifies the trailer.
	// Drain within the expansion bound so checksum/truncation errors reject the
	// complete download instead of installing an apparently usable prefix.
	if _, err := io.Copy(io.Discard, expandedStream); err != nil {
		return nil, fmt.Errorf("completion: finish archive stream: %w", err)
	}
	if expandedStream.N == 0 {
		return nil, errors.New("completion: archive expands beyond the size limit")
	}
	if archive.License == nil {
		return nil, errors.New("completion: archive has no LICENSE")
	}
	if len(archive.Files) == 0 {
		return nil, errors.New("completion: archive has no man/cmd sources")
	}
	return archive, nil
}

func cleanArchiveName(name string) (string, bool) {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsRune(name, '\\') {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false
		}
	}
	return strings.TrimPrefix(name, "./"), true
}

// sourceRelative accepts exactly the commit archive's man/cmd prefix, with an
// optional single top-level directory, and returns "<dir>/<file>.yaml".
func sourceRelative(name string) (string, bool) {
	const prefix = "man/cmd/"
	var rest string
	switch {
	case strings.HasPrefix(name, prefix):
		rest = name[len(prefix):]
	default:
		index := strings.IndexByte(name, '/')
		if index <= 0 || !strings.HasPrefix(name[index+1:], prefix) {
			return "", false
		}
		rest = name[index+1+len(prefix):]
	}
	if strings.Count(rest, "/") != 1 {
		return "", false
	}
	if !strings.HasSuffix(rest, ".yaml") {
		return "", false
	}
	return rest, true
}

func isTopLevelLicense(name string) bool {
	if name == "LICENSE" {
		return true
	}
	return strings.Count(name, "/") == 1 && strings.HasSuffix(name, "/LICENSE")
}
