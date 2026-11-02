package completion

import "testing"

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	commit := &Node{
		Token: "commit",
		Desc:  "record changes",
		Flags: []Flag{
			{Long: "all", Short: "a", Desc: "stage all"},
			{Long: "amend", Desc: "amend the previous commit"},
			{Long: "author", Value: true, Desc: "override the author"},
			{Long: "format", Value: true, Desc: "output format", Values: []Value{
				{Text: "json", Desc: "machine readable"},
				{Text: "human"},
			}},
			{Long: "file", Short: "F", Value: true, Repeatable: true},
		},
	}
	git := &Node{
		Token:      "git",
		Desc:       "the stupid content tracker",
		Aliases:    []string{"g"},
		Persistent: []Flag{{Long: "version"}},
		Flags: []Flag{
			{Long: "no-pager"},
			{Long: "paginate", Repeatable: true},
			{Long: "directory", Short: "C", Value: true},
		},
		Children: []*Node{commit, {Token: "stash", Desc: "stash changes"}, {Token: "status", Desc: "show status"}},
	}
	catalog, err := NewCatalog("test", "v0", "deadbeef", "MIT", []*Node{git, {Token: "docker", Desc: "containers"}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func candidateTexts(result Result) []string {
	texts := make([]string, len(result.Candidates))
	for i, candidate := range result.Candidates {
		texts[i] = candidate.Text
	}
	return texts
}

func hasCandidate(result Result, text string) bool {
	for _, candidate := range result.Candidates {
		if candidate.Text == text {
			return true
		}
	}
	return false
}

func TestCompleteSubcommands(t *testing.T) {
	catalog := testCatalog(t)

	result := Complete(catalog, "git s")
	if result.Suppressed {
		t.Fatal("git s was suppressed")
	}
	for _, want := range []string{"stash", "status"} {
		if !hasCandidate(result, want) {
			t.Fatalf("git s lacks %q in %v", want, candidateTexts(result))
		}
	}
	if hasCandidate(result, "commit") {
		t.Fatalf("git s offered a non-matching candidate: %v", candidateTexts(result))
	}
	if got := result.Candidates[0].Insert; got != "tash" {
		t.Fatalf("first insert = %q, want tash", got)
	}
}

func TestCompleteNestedOptions(t *testing.T) {
	catalog := testCatalog(t)

	result := Complete(catalog, "git commit --a")
	if result.Suppressed {
		t.Fatal("git commit --a was suppressed")
	}
	for _, want := range []string{"--all", "--amend", "--author"} {
		if !hasCandidate(result, want) {
			t.Fatalf("git commit --a lacks %q in %v", want, candidateTexts(result))
		}
	}

	// A long option already typed must not be offered again unless repeatable.
	result = Complete(catalog, "git commit --all --a")
	if hasCandidate(result, "--all") {
		t.Fatalf("used non-repeatable option was offered again: %v", candidateTexts(result))
	}
	if !hasCandidate(result, "--amend") {
		t.Fatalf("--amend is missing: %v", candidateTexts(result))
	}
}

func TestCompleteRepeatableOption(t *testing.T) {
	catalog := testCatalog(t)
	result := Complete(catalog, "git commit -F one -F two -F ")
	// -F takes a value with no literals, so nothing is inserted.
	if len(result.Candidates) != 0 {
		t.Fatalf("value-less option offered candidates: %v", candidateTexts(result))
	}
	result = Complete(catalog, "git --paginate --pag")
	if !hasCandidate(result, "--paginate") {
		t.Fatalf("repeatable option was not offered again: %v", candidateTexts(result))
	}
}

func TestCompleteOptionValue(t *testing.T) {
	catalog := testCatalog(t)

	result := Complete(catalog, "git commit --format=j")
	if len(result.Candidates) != 1 || result.Candidates[0].Text != "json" || result.Candidates[0].Insert != "son" {
		t.Fatalf("--format=j = %+v", result.Candidates)
	}

	result = Complete(catalog, "git commit --format ")
	if len(result.Candidates) != 2 {
		t.Fatalf("--format values = %v", candidateTexts(result))
	}

	// A value-taking option without literal values completes nothing, and the
	// rest of the line stays untouched.
	result = Complete(catalog, "git commit --author ")
	if len(result.Candidates) != 0 {
		t.Fatalf("--author offered %v", candidateTexts(result))
	}
}

func TestCompleteAfterOptionValueIsSubcommand(t *testing.T) {
	catalog := testCatalog(t)
	result := Complete(catalog, "git -C /tmp s")
	if !hasCandidate(result, "status") {
		t.Fatalf("after -C value: %v", candidateTexts(result))
	}
}

func TestCompleteOtherwise(t *testing.T) {
	catalog := testCatalog(t)
	cases := []string{
		`git commit -m "msg"`,
		`git commit -am x`,
		`git --unknown `,
		`git commit -- x`,
		`git commit msg s`,
		`git commit -abc`,
		" git s",
		"git  s",
		"git commit -m\tx",
	}
	for _, line := range cases {
		if result := Complete(catalog, line); !result.Suppressed && len(result.Candidates) > 0 {
			t.Errorf("%q offered %v (suppressed=%v)", line, candidateTexts(result), result.Suppressed)
		}
	}
}

func TestCompleteStopsOptionsAfterSeparator(t *testing.T) {
	catalog := testCatalog(t)
	result := Complete(catalog, "git commit -- ")
	if len(result.Candidates) != 0 {
		t.Fatalf("after --: %v", candidateTexts(result))
	}
}

func TestCompleteRoots(t *testing.T) {
	catalog := testCatalog(t)
	if result := Complete(catalog, ""); len(result.Candidates) != 3 {
		// Both roots and the reachable alias are offered.
		t.Fatalf("empty line roots = %v", candidateTexts(result))
	}
	result := Complete(catalog, "do")
	if len(result.Candidates) != 1 || result.Candidates[0].Text != "docker" {
		t.Fatalf("do = %v", candidateTexts(result))
	}
	// An alias reaches the same node but keeps its own text.
	if result := Complete(catalog, "g "); !hasCandidate(result, "commit") {
		t.Fatalf("alias line = %v", candidateTexts(result))
	}
}

func TestCompleteShortOptions(t *testing.T) {
	catalog := testCatalog(t)
	result := Complete(catalog, "git commit -")
	if !hasCandidate(result, "-a") || !hasCandidate(result, "-F") {
		t.Fatalf("short options = %v", candidateTexts(result))
	}
	result = Complete(catalog, "git commit message ")
	if len(result.Candidates) != 0 {
		t.Fatalf("after positional: %v", candidateTexts(result))
	}
}

func TestCompleteSeparatorSuppressesOptionWords(t *testing.T) {
	catalog := testCatalog(t)
	for _, line := range []string{"git commit -- --a", "git commit -- -", "git commit -- --format=j", "git commit -- path --a"} {
		if result := Complete(catalog, line); len(result.Candidates) != 0 {
			t.Errorf("%q offered %v", line, candidateTexts(result))
		}
	}
}

func TestCompleteOptionalValueDoesNotConsumeNextOption(t *testing.T) {
	catalog, _ := importFiles(t, []SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\nflags:\n  -c, --color?: colors\n  --verbose: verbose\ncompletion:\n  flag:\n    color: [always, auto]\n")}}, ImportOptions{})
	for _, line := range []string{"a --color --v", "a -c --v"} {
		if result := Complete(catalog, line); !hasCandidate(result, "--verbose") {
			t.Fatalf("%q optional value consumed next option: %v", line, candidateTexts(result))
		}
	}
	if result := Complete(catalog, "a --color=a"); !hasCandidate(result, "always") {
		t.Fatalf("attached optional value missing: %v", candidateTexts(result))
	}
}

func TestCandidatesAreMarkedAsCatalogText(t *testing.T) {
	result := Complete(testCatalog(t), "git ")
	if len(result.Candidates) == 0 {
		t.Fatal("no candidates")
	}
	for _, candidate := range result.Candidates {
		if !candidate.FromCatalog {
			t.Errorf("candidate %q is not marked as catalog text", candidate.Text)
		}
	}
}
