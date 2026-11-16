package completion

import (
	"bytes"
	"testing"
)

func TestBuiltinSnapshot(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	if catalog.Source != "carapace-bin" || catalog.Release != "v1.8.1" {
		t.Fatalf("metadata = %q %q", catalog.Source, catalog.Release)
	}
	if catalog.Commit != "6bc2b8d69a2474b6eeb626a85ef83254c2ab7d82" || catalog.License != "MIT" {
		t.Fatalf("provenance = %q %q", catalog.Commit, catalog.License)
	}
	if catalog.RootCount() < 700 || catalog.NodeCount() < 6000 || catalog.ValueCount() < 50 {
		t.Fatalf("snapshot too small: roots=%d nodes=%d values=%d",
			catalog.RootCount(), catalog.NodeCount(), catalog.ValueCount())
	}

	// The embedded snapshot must survive decode/encode byte for byte; this
	// catches non-deterministic ordering in the generator or the decoder.
	if !bytes.Equal(catalog.Encode(), BuiltinBytes()) {
		t.Fatal("decode/encode is not byte-identical to the embedded snapshot")
	}

	if catalog.Lookup([]string{"git", "commit"}) == nil {
		t.Fatal("git commit is missing")
	}
	if catalog.Root("git") == nil || catalog.Root("g") != nil {
		t.Fatal("root lookup by token")
	}
}

func TestBuiltinCompletion(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}

	result := Complete(catalog, "git s")
	for _, want := range []string{"status", "stash", "submodule"} {
		if !hasCandidate(result, want) {
			t.Fatalf("git s lacks %q", want)
		}
	}

	result = Complete(catalog, "git commit --a")
	for _, want := range []string{"--all", "--amend", "--author"} {
		if !hasCandidate(result, want) {
			t.Fatalf("git commit --a lacks %q in %v", want, candidateTexts(result))
		}
	}

	result = Complete(catalog, "git commit --author ")
	if len(result.Candidates) != 0 {
		t.Fatalf("--author should have no literal values: %v", candidateTexts(result))
	}

	result = Complete(catalog, "docker cont")
	if !hasCandidate(result, "container") {
		t.Fatalf("docker cont = %v", candidateTexts(result))
	}

	// docker compose is a CLI plugin: carapace finds it at runtime, so the
	// snapshot keeps it as the separate docker-compose root. The catalog
	// bridges the two and both spellings must complete the same CLI.
	result = Complete(catalog, "docker comp")
	if !hasCandidate(result, "compose") {
		t.Fatalf("docker comp = %v", candidateTexts(result))
	}
	for _, line := range []string{"docker compose ", "docker-compose "} {
		result = Complete(catalog, line)
		for _, want := range []string{"up", "down", "ps"} {
			if !hasCandidate(result, want) {
				t.Fatalf("%s lacks %q in %v", line, want, candidateTexts(result))
			}
		}
	}
}
