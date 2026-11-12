package pathcomp

import "testing"

func TestParseFindsThePathPosition(t *testing.T) {
	tests := []struct {
		line                           string
		command, dir, base             string
		query                          string
		home, path, separate, supports bool
	}{
		{line: "cd ", command: "cd", path: true, supports: true},
		{line: "cd src/", command: "cd", dir: "src/", query: "src/", path: true, supports: true},
		{line: "cd src/al", command: "cd", dir: "src/", base: "al", query: "src/", path: true, supports: true},
		{line: "cd /usr/lo", command: "cd", dir: "/usr/", base: "lo", query: "/usr/", path: true, supports: true},
		{line: "cd ~", command: "cd", dir: "~/", home: true, path: true, supports: true},
		{line: "cd ~/", command: "cd", dir: "~/", home: true, path: true, supports: true},
		{line: "cd ~/pro", command: "cd", dir: "~/", base: "pro", home: true, path: true, supports: true},
		{line: "cat file", command: "cat", base: "file", path: true, supports: true},
		{line: "ls -l ", command: "ls", path: true, supports: true},
		{line: "chmod 755 fi", command: "chmod", base: "fi", path: true, supports: true},
		{line: "grep pattern fi", command: "grep", base: "fi", path: true, supports: true},
		{line: "git a/b", command: "git", dir: "a/", base: "b", query: "a/", path: true, supports: true},
		{line: "echo anything/here", command: "echo", dir: "anything/", base: "here", query: "anything/", path: true, supports: true},

		// A complete path command is asking for its argument before the space:
		// the menu opens there and the insertion supplies the separator.
		{line: "cd", command: "cd", path: true, separate: true, supports: true},
		{line: "cat", command: "cat", path: true, separate: true, supports: true},
		{line: "./cat", command: "cat", path: true, separate: true, supports: true},
		{line: "chmod", command: "chmod", supports: true},
		{line: "grep", command: "grep", supports: true},
		{line: "lsof", command: "lsof", supports: true},
		{line: "cdx", command: "cdx", supports: true},

		// Not a path position: the word is an option, or a plain argument of an
		// unknown command.
		{line: "git checkout", command: "git", base: "checkout", supports: true},
		{line: "git ", command: "git", supports: true},
		{line: "cat -", command: "cat", base: "-", supports: true},
		{line: "chmod 7", command: "chmod", base: "7", supports: true},
		{line: "grep ", command: "grep", supports: true},
		{line: "echo foo", command: "echo", base: "foo", supports: true},

		// Lines whose meaning a bare-word suffix could change are refused.
		{line: `cat "x`},
		{line: "cat 'x"},
		{line: "cat $(x)"},
		{line: "cat *.txt"},
		{line: "cat a;b"},
		{line: "cat a|b"},
		{line: "cat a>b"},
		{line: "cat a#b"},
		{line: "cat a!b"},
		{line: " cat"},
		{line: "cat  x"},
		{line: "cd ~bob/"},
		{line: "cat a\\"},
		{line: "cat\ta"},
	}
	for _, test := range tests {
		t.Run(test.line, func(t *testing.T) {
			got, ok := Parse(test.line)
			if ok != test.supports {
				t.Fatalf("Parse(%q) ok=%v, want %v", test.line, ok, test.supports)
			}
			if !ok {
				return
			}
			if got.Command != test.command || got.Dir != test.dir || got.Base != test.base ||
				got.Query != test.query || got.Home != test.home || got.Path != test.path || got.Separate != test.separate {
				t.Fatalf("Parse(%q) = %+v, want command=%q dir=%q base=%q query=%q home=%v path=%v separate=%v",
					test.line, got, test.command, test.dir, test.base, test.query, test.home, test.path, test.separate)
			}
		})
	}
}

func TestParseKeepsEscapedSpacesInTheWord(t *testing.T) {
	ctx, ok := Parse(`cd ~/a\ b`)
	if !ok || !ctx.Path || !ctx.Home || ctx.Dir != "~/" || ctx.Base != "a b" || ctx.Query != "" {
		t.Fatalf("escaped space parsed as %+v ok=%v", ctx, ok)
	}
}

func TestEscapeMakesABareWord(t *testing.T) {
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{name: "alpha.txt", want: "alpha.txt", ok: true},
		{name: "a b", want: `a\ b`, ok: true},
		{name: "a'b", want: `a\'b`, ok: true},
		{name: `a"b`, want: `a\"b`, ok: true},
		{name: "a$b", want: `a\$b`, ok: true},
		{name: "a`b", want: "a\\`b", ok: true},
		{name: "a;b", want: `a\;b`, ok: true},
		{name: "a&b", want: `a\&b`, ok: true},
		{name: "a(b)", want: `a\(b\)`, ok: true},
		{name: "a[b]", want: `a\[b\]`, ok: true},
		{name: "a{b}", want: `a\{b\}`, ok: true},
		{name: "a*b", want: `a\*b`, ok: true},
		{name: "a?b", want: `a\?b`, ok: true},
		{name: "a!b", want: `a\!b`, ok: true},
		{name: "a#b", want: `a\#b`, ok: true},
		{name: "a~b", want: `a\~b`, ok: true},
		{name: "a=b", want: `a\=b`, ok: true},
		{name: "a:b", want: `a\:b`, ok: true},
		{name: `a\b`, want: `a\\b`, ok: true},
		{name: "中文.txt", want: "中文.txt", ok: true},
		{name: "a\tb"},
		{name: "a\nb"},
		{name: "a/b"},
		{name: "\xff"},
		{name: "‮"},
		{name: ""},
	}
	for _, test := range tests {
		got, ok := Escape(test.name)
		if ok != test.ok || got != test.want {
			t.Errorf("Escape(%q) = %q, %v; want %q, %v", test.name, got, ok, test.want, test.ok)
		}
	}
}

func TestSafeInsertAcceptsOnlyTheEscapedAlphabet(t *testing.T) {
	tests := []struct {
		suffix string
		ok     bool
	}{
		{suffix: "alpha", ok: true},
		{suffix: "a b"},
		{suffix: `a\ b`, ok: true},
		{suffix: "done/", ok: true},
		{suffix: "; rm -rf /"},
		{suffix: `a\;b`, ok: true},
		{suffix: "a\nb"},
		{suffix: `a\`},
		{suffix: "\xff"},
		{suffix: ""},
		{suffix: "a~b"},
		{suffix: `a\~b`, ok: true},
		{suffix: "中文", ok: true},
	}
	for _, test := range tests {
		if got := SafeInsert(test.suffix); got != test.ok {
			t.Errorf("SafeInsert(%q) = %v, want %v", test.suffix, got, test.ok)
		}
	}
}

func TestCandidatesReadTheListingOrder(t *testing.T) {
	ctx, ok := Parse("cd src/")
	if !ok || !ctx.Path {
		t.Fatalf("line parsed as %+v ok=%v", ctx, ok)
	}
	entries := []Entry{
		{Name: "docs", Dir: true},
		{Name: "docs2", Dir: true},
		{Name: "alpha.txt"},
		{Name: ".hidden", Dir: true},
		{Name: "-rf"},
	}
	got := Candidates(ctx, entries)
	want := []Candidate{
		{Text: "src/docs/", Insert: "docs/", Dir: true},
		{Text: "src/docs2/", Insert: "docs2/", Dir: true},
		{Text: "src/alpha.txt", Insert: "alpha.txt"},
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %+v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("candidate %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func TestCandidatesFilterAndCompleteExactNames(t *testing.T) {
	ctx, _ := Parse("cat al")
	got := Candidates(ctx, []Entry{{Name: "alpha"}, {Name: "beta"}})
	if len(got) != 1 || got[0].Text != "alpha" || got[0].Insert != "pha" {
		t.Fatalf("candidates = %+v", got)
	}

	// An exact file match adds nothing, so it is not a row. An exact directory
	// still adds the slash that opens it.
	ctx, _ = Parse("cat alpha")
	if got := Candidates(ctx, []Entry{{Name: "alpha"}}); len(got) != 0 {
		t.Fatalf("exact file match produced %+v", got)
	}
	ctx, _ = Parse("cd alpha")
	got = Candidates(ctx, []Entry{{Name: "alpha", Dir: true}})
	if len(got) != 1 || got[0].Insert != "/" || got[0].Text != "alpha/" {
		t.Fatalf("exact directory match produced %+v", got)
	}
}

func TestCandidatesShowHiddenNamesOnlyAfterADot(t *testing.T) {
	entries := []Entry{{Name: ".config", Dir: true}, {Name: "visible"}}
	ctx, _ := Parse("cd ")
	if got := Candidates(ctx, entries); len(got) != 1 || got[0].Text != "visible" {
		t.Fatalf("hidden names without a dot: %+v", got)
	}
	ctx, _ = Parse("cd .")
	if got := Candidates(ctx, entries); len(got) != 1 || got[0].Insert != "config/" {
		t.Fatalf("hidden names after a dot: %+v", got)
	}
}

func TestCandidatesSeparateTheCommandFromItsArgument(t *testing.T) {
	ctx, ok := Parse("cd")
	if !ok || !ctx.Path || !ctx.Separate {
		t.Fatalf("bare command parsed as %+v ok=%v", ctx, ok)
	}
	got := Candidates(ctx, []Entry{{Name: "docs", Dir: true}, {Name: "notes.txt"}})
	if len(got) != 2 {
		t.Fatalf("candidates = %+v", got)
	}
	if got[0].Text != "docs/" || got[0].Insert != " docs/" || got[1].Insert != " notes.txt" {
		t.Fatalf("candidates = %+v", got)
	}
}

func TestCandidatesEscapeNamesAndBoundTheAnswer(t *testing.T) {
	ctx, _ := Parse("cat ")
	got := Candidates(ctx, []Entry{{Name: "my file"}})
	if len(got) != 1 || got[0].Text != "my file" || got[0].Insert != `my\ file` {
		t.Fatalf("escaped candidate = %+v", got)
	}
	entries := make([]Entry, MaxCandidates+10)
	for index := range entries {
		entries[index] = Entry{Name: string(rune('a'+index%26)) + "x"}
	}
	if got := Candidates(ctx, entries); len(got) > MaxCandidates {
		t.Fatalf("candidates = %d", len(got))
	}
}
