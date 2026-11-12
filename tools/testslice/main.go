// Command testslice prints the -run pattern for one shard of a Go package's
// tests, so one long package can be spread over several CI jobs:
//
//	pattern=$(go run ./tools/testslice -race -n 2 -i 0 ./internal/workbench)
//	go test -race ./internal/workbench -run "$pattern"
//
// A shard whose pattern would not fit a shell's command line is printed as
// several patterns, one per line, with -max-pattern; run go test once for each.
// The Windows workbench job uses that: its shard pattern had grown to the
// 32 KiB command line limit.
//
// The shard is chosen by hashing the test name, so it is stable across machines
// and runs: a test that is added lands in exactly one shard without a list to
// maintain, and no test is left out as the suite grows.
//
// The names come from `go test -list` with the same build flags as the run that
// follows, so the test binary go test builds while listing is the one it runs
// next, and build-tagged tests are sliced as the platform sees them.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// listPattern selects the kinds of top-level names a shard can carry: tests,
// examples and fuzz targets. Benchmarks are left out; use -bench separately.
const listPattern = `^(Test|Example|Fuzz)`

func main() {
	var (
		shards     = flag.Int("n", 1, "number of shards")
		index      = flag.Int("i", 0, "shard to print, from 0")
		race       = flag.Bool("race", false, "ask go test for the names with -race")
		tags       = flag.String("tags", "", "build tags to list with, comma separated")
		maxPattern = flag.Int("max-pattern", 0, "split the shard into patterns no longer than this many bytes (0: one pattern)")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: testslice [-n shards] [-i shard] [-race] [-tags tags] [-max-pattern bytes] packages...\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *shards < 1 || *index < 0 || *index >= *shards {
		fmt.Fprintf(os.Stderr, "testslice: shard %d of %d is out of range\n", *index, *shards)
		os.Exit(2)
	}

	names, err := listNames(flag.Args(), *race, *tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testslice: %v\n", err)
		os.Exit(1)
	}
	for _, pattern := range runPatterns(shard(names, *shards, *index), *maxPattern) {
		fmt.Println(pattern)
	}
}

// listNames asks go test which tests, examples and fuzz targets the packages
// carry, with the caller's build flags.
func listNames(pkgs []string, race bool, tags string) ([]string, error) {
	args := []string{"test"}
	if race {
		args = append(args, "-race")
	}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "-list", listPattern)
	args = append(args, pkgs...)

	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return parseNames(out), nil
}

// parseNames keeps the listed names and drops the package status lines that go
// test -list interleaves ("ok  pkg  0.01s", "?  pkg  [no test files]").
func parseNames(out []byte) []string {
	seen := make(map[string]bool)
	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(name, "Test") && !strings.HasPrefix(name, "Example") && !strings.HasPrefix(name, "Fuzz") {
			continue
		}
		if strings.ContainsAny(name, " \t") {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// shard returns the names whose hash places them in shard index. Hashing spreads
// the suite evenly despite tests being grouped by file, and keeps a name in the
// same shard from one run to the next.
func shard(names []string, shards, index int) []string {
	var out []string
	for _, name := range names {
		if hash(name)%uint64(shards) == uint64(index) {
			out = append(out, name)
		}
	}
	return out
}

func hash(name string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(name))
	return h.Sum64()
}

// runPattern turns a shard into a -run regular expression. A shard with no names
// matches nothing, so an idle shard still runs and passes.
func runPattern(names []string) string {
	if len(names) == 0 {
		return "^$"
	}
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = regexp.QuoteMeta(name)
	}
	return "^(?:" + strings.Join(quoted, "|") + ")$"
}

// runPatterns turns a shard into one or more -run regular expressions, each no
// longer than max bytes; max <= 0 keeps one expression. Chunking changes only
// how many go test invocations the caller starts, never which tests run.
func runPatterns(names []string, max int) []string {
	if len(names) == 0 {
		return []string{"^$"}
	}
	if max <= 0 {
		return []string{runPattern(names)}
	}
	const wrapper = len("^(?:)$")
	var (
		patterns []string
		chunk    []string
		length   = wrapper
	)
	for _, name := range names {
		quoted := len(regexp.QuoteMeta(name))
		add := quoted
		if len(chunk) > 0 {
			add++ // the "|" before it
		}
		if len(chunk) > 0 && length+add > max {
			patterns = append(patterns, runPattern(chunk))
			chunk, length = chunk[:0], wrapper
			add = quoted
		}
		chunk = append(chunk, name)
		length += add
	}
	return append(patterns, runPattern(chunk))
}
