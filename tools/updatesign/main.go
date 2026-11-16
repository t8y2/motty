// Command updatesign signs the packages of a release with the key the updater
// checks, and writes the SIGNATURES manifest that tools/mirror-release.sh copies
// into latest.json.
//
//	updatesign generate -out ~/.motty/updater-signing.key
//	updatesign sign -key KEY -version 0.1.2 -dir build/release-0.1.2
//	updatesign verify -version 0.1.2 -dir build/release-0.1.2
//
// The private key never belongs in the repository: the release workflow reads it
// from MOTTY_UPDATE_SIGNING_KEY and the owner keeps an offline copy.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"aead.dev/minisign"
	"motty/internal/updatesign"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "generate":
		err = generate(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  updatesign generate -out KEY
  updatesign sign -key KEY -version VERSION -dir DIR [-out SIGNATURES]
  updatesign verify -version VERSION -dir DIR [-signatures SIGNATURES] [-pubkey KEY]
`)
	os.Exit(2)
}

func parseKey(path string) (minisign.PrivateKey, error) {
	var key minisign.PrivateKey
	text, err := os.ReadFile(path)
	if err != nil {
		return key, err
	}
	if minisign.IsEncrypted(text) {
		return key, errors.New("the key is encrypted; this tool takes the plain release key")
	}
	if err := key.UnmarshalText(text); err != nil {
		return key, fmt.Errorf("reading %s: %w", path, err)
	}
	return key, nil
}

func packageNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "motty") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("%s holds no packages to sign", dir)
	}
	return names, nil
}

func generate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ExitOnError)
	out := flags.String("out", "", "file to write the private key to")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("generate needs -out")
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a signing key", *out)
	}
	publicKey, privateKey, err := minisign.GenerateKey(nil)
	if err != nil {
		return err
	}
	privateText, err := privateKey.MarshalText()
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, privateText, 0o600); err != nil {
		return err
	}
	publicText, err := publicKey.MarshalText()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "private key written to %s (keep it offline and out of the repository)\n", *out)
	fmt.Printf("%s\n", publicText)
	return nil
}

func sign(args []string) error {
	flags := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := flags.String("key", "", "the private key file")
	version := flags.String("version", "", "the release version, without the v")
	dir := flags.String("dir", "", "the directory holding the release packages")
	out := flags.String("out", "", "where to write the signatures (default DIR/SIGNATURES)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" || *version == "" || *dir == "" {
		return errors.New("sign needs -key, -version and -dir")
	}
	if *out == "" {
		*out = filepath.Join(*dir, "SIGNATURES")
	}
	privateKey, err := parseKey(*keyPath)
	if err != nil {
		return err
	}
	names, err := packageNames(*dir)
	if err != nil {
		return err
	}
	var manifest strings.Builder
	for _, name := range names {
		if strings.ContainsAny(name, "\t\n") {
			return fmt.Errorf("a package name cannot hold a tab or newline: %q", name)
		}
		packageFile, err := os.Open(filepath.Join(*dir, name))
		if err != nil {
			return err
		}
		signature, err := updatesign.Sign(privateKey, packageFile, *version, name, time.Now())
		packageFile.Close()
		if err != nil {
			return fmt.Errorf("signing %s: %w", name, err)
		}
		fmt.Fprintf(&manifest, "%s\t%s\n", name, signature)
		fmt.Printf("  signed %s\n", name)
	}
	if err := os.WriteFile(*out, []byte(manifest.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d packages)\n", *out, len(names))
	return nil
}

func verify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	version := flags.String("version", "", "the release version, without the v")
	dir := flags.String("dir", "", "the directory holding the release packages")
	manifestPath := flags.String("signatures", "", "the signatures file (default DIR/SIGNATURES)")
	publicPath := flags.String("pubkey", "", "a public key file, instead of the embedded release key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version == "" || *dir == "" {
		return errors.New("verify needs -version and -dir")
	}
	if *manifestPath == "" {
		*manifestPath = filepath.Join(*dir, "SIGNATURES")
	}
	publicKey := updatesign.ReleaseKey()
	if *publicPath != "" {
		text, err := os.ReadFile(*publicPath)
		if err != nil {
			return err
		}
		if err := publicKey.UnmarshalText(text); err != nil {
			return fmt.Errorf("reading %s: %w", *publicPath, err)
		}
	}
	file, err := os.Open(*manifestPath)
	if err != nil {
		return err
	}
	defer file.Close()
	verified := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		name, signature, ok := strings.Cut(line, "\t")
		if !ok || name == "" || signature == "" {
			return fmt.Errorf("%s: a line does not name a file and a signature", *manifestPath)
		}
		if err := updatesign.VerifyFile(publicKey, filepath.Join(*dir, name), signature, *version, name); err != nil {
			fmt.Printf("  FAILED %s: %v\n", name, err)
			return err
		}
		fmt.Printf("  verified %s\n", name)
		verified++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if verified == 0 {
		return fmt.Errorf("%s names no packages", *manifestPath)
	}
	fmt.Printf("verified %d packages\n", verified)
	return nil
}
