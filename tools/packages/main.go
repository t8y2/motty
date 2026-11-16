// Command packages renders the Homebrew cask and the Scoop manifest for a
// published release from its SHA256SUMS file. It writes files only; packages.yml
// is what commits them to the tap and the bucket.
//
//	go run ./tools/packages -version 0.2.9 -sums /tmp/SHA256SUMS -out /tmp/packages
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// repository is where the packages the manifests name are published. A
	// release tag is v<version>, and every asset carries the version too.
	repository = "t8y2/motty"
	// homepage is what the package managers show for the project.
	homepage = "https://motty.page/"
	// name and description are what both package managers show.
	name        = "Motty"
	description = "SSH client with terminal, SFTP, port forwarding and an AI assistant"
	license     = "Apache-2.0"

	// macOSMinimum is [mac] min_system in viva.toml, which appbundle falls back
	// to when it is not set. A test keeps this and the manifest equal, because
	// the cask's depends_on must not promise more than the bundle requires.
	macOSMinimum = "12.0"
	macOSSymbol  = ":monterey"

	caskFile     = "Casks/motty.rb"
	manifestFile = "motty.json"
)

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "packages:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("packages", flag.ContinueOnError)
	version := flags.String("version", "", "release version, with or without a leading v, for example 0.2.9")
	sumsPath := flags.String("sums", "", "SHA256SUMS file of the release")
	outDir := flags.String("out", "", "directory to write Casks/motty.rb and motty.json into")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version == "" || *sumsPath == "" || *outDir == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: packages -version X.Y.Z -sums SHA256SUMS -out DIR")
	}
	number := strings.TrimPrefix(strings.TrimSpace(*version), "v")
	if !versionPattern.MatchString(number) {
		return fmt.Errorf("%q is not a version like 0.2.9", *version)
	}

	sums, err := readSums(*sumsPath)
	if err != nil {
		return err
	}
	packages, err := packagesOf(number, sums)
	if err != nil {
		return err
	}
	manifest, err := renderManifest(number, packages)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(*outDir)
	if err != nil {
		return err
	}
	rendered := []struct {
		path    string
		content []byte
	}{
		{filepath.Join(root, caskFile), []byte(renderCask(number, packages))},
		{filepath.Join(root, manifestFile), manifest},
	}
	for _, file := range rendered {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file.path, file.content, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "wrote %s and %s for %s\n", caskFile, manifestFile, number)
	return nil
}

// readSums reads a SHA256SUMS file: one "<hex>  <name>" line per file.
func readSums(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sums := make(map[string]string)
	for number, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || len(fields[0]) != 64 {
			return nil, fmt.Errorf("%s:%d: not a checksum line", path, number+1)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, number+1, err)
		}
		// A leading * marks a binary file in some tools' output.
		sums[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	return sums, nil
}

// asset is one file of the release and the digest the manifests carry for it.
type asset struct {
	name string
	sum  string
}

// downloads are the four packages the two manifests install from.
type downloads struct {
	dmgARM64 asset
	dmgAMD64 asset
	zipARM64 asset
	zipAMD64 asset
}

func (files downloads) all() []asset {
	return []asset{files.dmgARM64, files.dmgAMD64, files.zipARM64, files.zipAMD64}
}

// packagesOf names the packages of one version and requires each of them to be
// in the checksums: a manifest must never point at a file the release does not
// carry.
func packagesOf(version string, sums map[string]string) (downloads, error) {
	files := downloads{
		dmgARM64: asset{"motty-" + version + "-darwin-arm64.dmg", sums["motty-"+version+"-darwin-arm64.dmg"]},
		dmgAMD64: asset{"motty-" + version + "-darwin-amd64.dmg", sums["motty-"+version+"-darwin-amd64.dmg"]},
		zipARM64: asset{"motty-" + version + "-windows-arm64.zip", sums["motty-"+version+"-windows-arm64.zip"]},
		zipAMD64: asset{"motty-" + version + "-windows-amd64.zip", sums["motty-"+version+"-windows-amd64.zip"]},
	}
	var missing []string
	for _, file := range files.all() {
		if file.sum == "" {
			missing = append(missing, file.name)
		}
	}
	if len(missing) != 0 {
		return downloads{}, fmt.Errorf("the checksums have no %s", strings.Join(missing, ", "))
	}
	return files, nil
}
