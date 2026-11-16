package main

import (
	"regexp"
	"strconv"
	"testing"
)

// The shards must together carry every test exactly once: dropping one would let
// CI pass without running it, and duplicating one would waste a runner.
func TestShardsCoverEveryNameOnce(t *testing.T) {
	names := make([]string, 0, 500)
	for i := 0; i < 500; i++ {
		names = append(names, "Test"+string(rune('A'+i%26))+"Case"+strconv.Itoa(i))
	}

	const shards = 4
	seen := make(map[string]int, len(names))
	for index := 0; index < shards; index++ {
		for _, name := range shard(names, shards, index) {
			seen[name]++
		}
	}
	if len(seen) != len(names) {
		t.Fatalf("shards carry %d names, want %d", len(seen), len(names))
	}
	for name, count := range seen {
		if count != 1 {
			t.Errorf("%s is in %d shards, want 1", name, count)
		}
	}
}

// The split has to be stable: a name that moves between runs would make the CI
// caches useless.
func TestShardIsStableAndBalanced(t *testing.T) {
	names := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		names = append(names, "Test"+strconv.Itoa(i))
	}

	first := shard(names, 2, 0)
	if second := shard(names, 2, 0); len(second) != len(first) {
		t.Fatalf("shard size changed between runs: %d then %d", len(first), len(second))
	}
	if len(first) < 400 || len(first) > 600 {
		t.Fatalf("shard of 2 holds %d of 1000 names, want roughly half", len(first))
	}
}

// The pattern is handed to go test -run: it must select the shard's names and
// nothing else.
func TestRunPatternSelectsOnlyItsShard(t *testing.T) {
	names := []string{"TestAlpha", "TestBravo", "TestCharlie", "TestDelta", "TestEcho"}
	inShard := shard(names, 2, 0)
	pattern := regexp.MustCompile(runPattern(inShard))

	for _, name := range names {
		matched := pattern.MatchString(name)
		want := false
		for _, want2 := range inShard {
			if want2 == name {
				want = true
			}
		}
		if matched != want {
			t.Errorf("pattern %q matches %s = %v, want %v", pattern, name, matched, want)
		}
	}

	if got := runPattern(nil); got != "^$" {
		t.Fatalf("empty shard pattern = %q, want ^$", got)
	}
}

// Chunking keeps each go test command line short without dropping or
// duplicating a name; a shard must still cover its tests exactly once.
func TestRunPatternsChunksWithoutChangingTheSelection(t *testing.T) {
	names := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		names = append(names, "TestSomeLongName"+strconv.Itoa(i))
	}
	const max = 600
	patterns := runPatterns(names, max)
	if len(patterns) < 2 {
		t.Fatalf("max pattern %d produced %d pattern(s): %v", max, len(patterns), patterns)
	}
	counts := make(map[string]int, len(names))
	for _, pattern := range patterns {
		if len(pattern) > max {
			t.Errorf("pattern of %d bytes exceeds max %d: %q", len(pattern), max, pattern)
		}
		re := regexp.MustCompile(pattern)
		for _, name := range names {
			if re.MatchString(name) {
				counts[name]++
			}
		}
	}
	for _, name := range names {
		if counts[name] != 1 {
			t.Fatalf("%s is matched by %d patterns, want 1", name, counts[name])
		}
	}
	if got := runPatterns(nil, max); len(got) != 1 || got[0] != "^$" {
		t.Fatalf("empty shard patterns = %v, want [^$]", got)
	}
	if got := runPatterns(names[:3], 0); len(got) != 1 {
		t.Fatalf("unlimited patterns = %v, want one", got)
	}
}

// Package status lines sit between the names in go test -list output.
func TestParseNamesKeepsOnlyNames(t *testing.T) {
	out := []byte("TestAlpha\nTestBravo\nok  \tmotty/internal/x\t0.021s\nFuzzCharlie\n?   \tmotty/internal/y\t[no test files]\nTestAlpha\n")
	got := parseNames(out)
	want := []string{"FuzzCharlie", "TestAlpha", "TestBravo"}
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}
}
