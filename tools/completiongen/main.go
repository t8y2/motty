// Command completiongen builds the built-in command-completion snapshot from a
// pinned Carapace source archive.
//
// The snapshot is generated offline, committed as an asset and embedded in the
// binary. Running this tool again with the same archive must produce the same
// bytes, which -check verifies in CI-friendly form.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"motty/internal/completion"
)

const defaultCommit = "6bc2b8d69a2474b6eeb626a85ef83254c2ab7d82"

func main() {
	archivePath := flag.String("archive", "", "path to the pinned carapace-bin source archive (.tar.gz)")
	wantSHA := flag.String("sha256", "", "expected SHA-256 of the archive")
	outDir := flag.String("out", "internal/completion/data", "output directory")
	release := flag.String("release", "v1.8.1", "upstream release")
	commit := flag.String("commit", defaultCommit, "immutable upstream commit")
	source := flag.String("source", "carapace-bin", "upstream source name")
	licenseName := flag.String("license", "MIT", "upstream license identifier")
	check := flag.Bool("check", false, "verify the existing snapshot instead of writing it")
	flag.Parse()

	if *archivePath == "" {
		fatal("-archive is required")
	}
	archive, err := os.ReadFile(*archivePath)
	if err != nil {
		fatal("%v", err)
	}
	sum := sha256.Sum256(archive)
	archiveSHA := hex.EncodeToString(sum[:])
	if *wantSHA != "" && *wantSHA != archiveSHA {
		fatal("archive SHA-256 is %s, expected %s", archiveSHA, *wantSHA)
	}

	sources, err := completion.ReadSourceArchive(bytes.NewReader(archive), completion.DefaultArchiveLimits())
	if err != nil {
		fatal("%v", err)
	}
	catalog, stats, err := completion.Import(sources.Files, completion.Metadata{
		Source:  *source,
		Release: *release,
		Commit:  *commit,
		License: *licenseName,
	}, completion.DefaultImportOptions())
	if err != nil {
		fatal("%v", err)
	}
	snapshot := catalog.Encode()
	snapshotSum := sha256.Sum256(snapshot)

	manifest := fmt.Sprintf(`source: %s
release: %s
commit: %s
license: %s
format: %d
archive: %s
archive-bytes: %d
archive-sha256: %s
snapshot-bytes: %d
snapshot-sha256: %s
roots: %d
nodes: %d
flags: %d
values: %d
imported-files: %d
skipped-files: %d
skipped-commands: %d
skipped-flags: %d
skipped-values: %d
hidden-flags: %d
rootless-dirs: %d
repaired-files: %d
skipped-sources: %d
audit-warnings: %d
generated-by: tools/completiongen
`, *source, *release, *commit, *licenseName, completion.FormatVersion,
		filepath.Base(*archivePath), len(archive), archiveSHA,
		len(snapshot), hex.EncodeToString(snapshotSum[:]),
		stats.Roots, stats.Nodes, stats.Flags, stats.Values,
		len(sources.Files), stats.SkippedFiles, stats.SkippedCommands,
		stats.SkippedFlags, stats.SkippedValues, stats.HiddenFlags, stats.RootlessDirs,
		stats.RepairedFiles, stats.SkippedSources, stats.AuditWarnings)

	outputs := []struct {
		name string
		data []byte
	}{
		{"catalog.bin", snapshot},
		{"LICENSE.carapace", sources.License},
		{"SOURCE.txt", []byte(manifest)},
	}
	if *check {
		for _, output := range outputs {
			path := filepath.Join(*outDir, output.name)
			existing, err := os.ReadFile(path)
			if err != nil {
				fatal("%v", err)
			}
			if !bytes.Equal(existing, output.data) {
				fatal("%s does not match the pinned archive", path)
			}
		}
		fmt.Printf("snapshot up to date: %d roots, %d nodes, %d flags, %d values, %d bytes\n",
			stats.Roots, stats.Nodes, stats.Flags, stats.Values, len(snapshot))
		return
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal("%v", err)
	}
	for _, output := range outputs {
		if err := writeAtomic(filepath.Join(*outDir, output.name), output.data); err != nil {
			fatal("%v", err)
		}
	}
	fmt.Printf("wrote %s: %d roots, %d nodes, %d flags, %d values, %d bytes\n",
		*outDir, stats.Roots, stats.Nodes, stats.Flags, stats.Values, len(snapshot))
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".completiongen-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "completiongen: "+format+"\n", args...)
	os.Exit(1)
}
