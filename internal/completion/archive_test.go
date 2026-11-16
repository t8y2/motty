package completion

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

type archiveEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

func testArchive(t *testing.T, entries []archiveEntry, limit ArchiveLimits) (*SourceArchive, error) {
	t.Helper()
	return ReadSourceArchive(bytes.NewReader(testArchiveBytes(t, entries)), limit)
}

func testArchiveBytes(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: entry.typeflag}
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
		}
		if len(entry.linkname) > 0 {
			header.Linkname = entry.linkname
		}
		if header.Typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write header %q: %v", entry.name, err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write body %q: %v", entry.name, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buffer.Bytes()
}

func TestReadSourceArchive(t *testing.T) {
	archive, err := testArchive(t, []archiveEntry{
		{name: "carapace-bin-1.8.1/LICENSE", body: "MIT License\n"},
		{name: "carapace-bin-1.8.1/man/cmd/git/git.yaml", body: "name: git\n"},
		{name: "carapace-bin-1.8.1/man/cmd/git/README.md", body: "not yaml"},
		{name: "carapace-bin-1.8.1/man/cmd/../escape.yaml", body: "name: escape\n"},
		{name: "carapace-bin-1.8.1/man/cmd/git/link.yaml", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
		{name: "carapace-bin-1.8.1/pkg/command/command.go", body: "package command"},
	}, DefaultArchiveLimits())
	if err != nil {
		t.Fatalf("ReadSourceArchive: %v", err)
	}
	if len(archive.Files) != 1 || archive.Files[0].Name != "git/git.yaml" {
		t.Fatalf("files = %+v", archive.Files)
	}
	if !strings.Contains(string(archive.License), "MIT") {
		t.Fatalf("license = %q", archive.License)
	}
}

func TestReadSourceArchiveRejects(t *testing.T) {
	base := []archiveEntry{
		{name: "man/cmd/git/git.yaml", body: "name: git\n"},
		{name: "man/cmd/ls/ls.yaml", body: "name: ls\n"},
		{name: "LICENSE", body: "MIT\n"},
	}

	limit := DefaultArchiveLimits()
	limit.MaxMembers = 1
	if _, err := testArchive(t, base, limit); err == nil {
		t.Fatal("too many members was accepted")
	}

	limit = DefaultArchiveLimits()
	limit.MaxCompressed = 8
	if _, err := testArchive(t, base, limit); err == nil {
		t.Fatal("oversized archive was accepted")
	}

	limit = DefaultArchiveLimits()
	limit.MaxFileBytes = 4
	if _, err := testArchive(t, base, limit); err == nil {
		t.Fatal("oversized source file was accepted")
	}

	if _, err := testArchive(t, []archiveEntry{
		{name: "man/cmd/git/git.yaml", body: "name: git\n"},
	}, DefaultArchiveLimits()); err == nil {
		t.Fatal("archive without license was accepted")
	}
}

func TestReadSourceArchiveRejectsBadPrefix(t *testing.T) {
	if _, err := testArchive(t, []archiveEntry{
		{name: "nested/carapace-bin-1.8.1/man/cmd/git/git.yaml", body: "name: git\n"},
		{name: "nested/carapace-bin-1.8.1/LICENSE", body: "MIT\n"},
	}, DefaultArchiveLimits()); err == nil {
		t.Fatal("two-level prefix was accepted")
	}
}

func TestReadSourceArchiveRejectsOversizedChild(t *testing.T) {
	limit := DefaultArchiveLimits()
	limit.MaxFileBytes = 12
	if _, err := testArchive(t, []archiveEntry{
		{name: "LICENSE", body: "MIT"},
		{name: "man/cmd/a/a.yaml", body: "name: a\n"},
		{name: "man/cmd/a/a.child.yaml", body: "name: child\ndescription: too long\n"},
	}, limit); err == nil {
		t.Fatal("oversized child was silently dropped")
	}
}

func TestReadSourceArchiveAllowsFileAtSizeLimit(t *testing.T) {
	data := "name: a\n"
	limits := DefaultArchiveLimits()
	limits.MaxFileBytes = int64(len(data))
	archive, err := testArchive(t, []archiveEntry{{name: "LICENSE", body: "MIT"}, {name: "man/cmd/a/a.yaml", body: data}}, limits)
	if err != nil || len(archive.Files) != 1 {
		t.Fatalf("source exactly at size limit was rejected: %v", err)
	}
}

func TestReadSourceArchivePlainTar(t *testing.T) {
	compressed := testArchiveBytes(t, []archiveEntry{{name: "LICENSE", body: "MIT"}, {name: "man/cmd/a/a.yaml", body: "name: a\n"}})
	zipper, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(zipper)
	if err != nil {
		t.Fatal(err)
	}
	zipper.Close()
	archive, err := ReadSourceArchive(bytes.NewReader(plain), DefaultArchiveLimits())
	if err != nil || len(archive.Files) != 1 || archive.Files[0].Name != "a/a.yaml" {
		t.Fatalf("plain tar not read intact: archive=%+v, error=%v", archive, err)
	}
}

func TestReadSourceArchiveRejectsDamagedGzipTrailer(t *testing.T) {
	compressed := testArchiveBytes(t, []archiveEntry{{name: "LICENSE", body: "MIT"}, {name: "man/cmd/a/a.yaml", body: "name: a\n"}})
	corrupted := bytes.Clone(compressed)
	corrupted[len(corrupted)-8] ^= 0xff // Damage the gzip CRC after a complete tar payload.
	for name, data := range map[string][]byte{"checksum": corrupted, "truncated": compressed[:len(compressed)-8]} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadSourceArchive(bytes.NewReader(data), DefaultArchiveLimits()); err == nil {
				t.Fatal("damaged gzip trailer was accepted")
			}
		})
	}
}

func TestReadSourceArchiveBoundsKeptSourceBytes(t *testing.T) {
	data := "name: a\n"
	limits := DefaultArchiveLimits()
	// Room for the license and the first source, but not the second one.
	limits.MaxSourceBytes = int64(len("MIT") + len(data))
	if _, err := testArchive(t, []archiveEntry{
		{name: "LICENSE", body: "MIT"},
		{name: "man/cmd/a/a.yaml", body: data},
		{name: "man/cmd/b/b.yaml", body: data},
	}, limits); err == nil {
		t.Fatal("archive sources above the memory bound were accepted")
	}
}

func TestDefaultArchiveLimitsKeepWorkAndMemoryBoundsSeparate(t *testing.T) {
	limits := DefaultArchiveLimits()
	// Carapace 1.8.x expands to about 108 MB; the work bound must leave at least
	// double that instead of failing on normal growth.
	if limits.MaxExpanded < 200<<20 {
		t.Fatalf("MaxExpanded = %d, want at least 200 MiB", limits.MaxExpanded)
	}
	// The work bound alone must not decide how much may be kept in memory.
	if limits.MaxSourceBytes > 128<<20 {
		t.Fatalf("MaxSourceBytes = %d, want at most 128 MiB", limits.MaxSourceBytes)
	}
	if limits.MaxSourceBytes > limits.MaxExpanded/2 {
		t.Fatalf("MaxSourceBytes (%d) is not clearly below MaxExpanded (%d)", limits.MaxSourceBytes, limits.MaxExpanded)
	}
}

func TestReadSourceArchiveBoundsBytesAfterTarEnd(t *testing.T) {
	compressed := testArchiveBytes(t, []archiveEntry{{name: "LICENSE", body: "MIT"}, {name: "man/cmd/a/a.yaml", body: "name: a\n"}})
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	var padded bytes.Buffer
	writer := gzip.NewWriter(&padded)
	if _, err := writer.Write(append(plain, make([]byte, 4096)...)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	limits := DefaultArchiveLimits()
	limits.MaxExpanded = int64(len(plain))
	if _, err := ReadSourceArchive(bytes.NewReader(padded.Bytes()), limits); err == nil {
		t.Fatal("trailing gzip expansion bypassed the tar size limit")
	}
}
