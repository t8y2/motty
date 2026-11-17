// Command checkbuild checks the provenance of a release executable before Viva
// packages it. Reading Go build information works for foreign target binaries.
package main

import (
	"debug/buildinfo"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"
)

type expectedBuild struct {
	revision, when, target, arch string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "checkbuild:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("checkbuild", flag.ContinueOnError)
	var program string
	var expected expectedBuild
	flags.StringVar(&program, "program", "", "release executable to inspect")
	flags.StringVar(&expected.revision, "revision", "", "expected full Git commit")
	flags.StringVar(&expected.when, "time", "", "expected Git commit time (RFC3339)")
	flags.StringVar(&expected.target, "target", "", "expected GOOS")
	flags.StringVar(&expected.arch, "arch", "", "expected GOARCH")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if program == "" || expected.revision == "" || expected.when == "" || expected.target == "" || expected.arch == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: checkbuild -program FILE -revision SHA -time RFC3339 -target GOOS -arch GOARCH")
	}
	info, err := buildinfo.ReadFile(program)
	if err != nil {
		return fmt.Errorf("read %s: %w", program, err)
	}
	if err := expected.check(info); err != nil {
		return fmt.Errorf("%s: %w", program, err)
	}
	fmt.Fprintf(out, "verified build %s %s %s/%s\n", expected.revision, expected.when, expected.target, expected.arch)
	return nil
}

func (expected expectedBuild) check(info *debug.BuildInfo) error {
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	fields := []struct{ key, want string }{
		{"vcs", "git"}, {"vcs.revision", expected.revision},
		{"vcs.modified", "false"}, {"GOOS", expected.target}, {"GOARCH", expected.arch},
	}
	// A cgo program links the C library of the machine that built it, so a
	// Linux program built on a newer distribution refuses to start on an older
	// one. Neither platform uses cgo (Viva reaches them through purego and
	// system calls); the macOS program does, through the Objective-C bridge.
	if expected.target == "linux" || expected.target == "windows" {
		fields = append(fields, struct{ key, want string }{"CGO_ENABLED", "0"})
	}
	for _, field := range fields {
		if got := settings[field.key]; got != field.want {
			return fmt.Errorf("%s=%q, want %q", field.key, got, field.want)
		}
	}
	want, err := time.Parse(time.RFC3339, expected.when)
	if err != nil {
		return fmt.Errorf("invalid expected commit time: %w", err)
	}
	got, err := time.Parse(time.RFC3339, settings["vcs.time"])
	if err != nil || !got.Equal(want) {
		return fmt.Errorf("vcs.time=%q, want %q", settings["vcs.time"], expected.when)
	}
	return nil
}
