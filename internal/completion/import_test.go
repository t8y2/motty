package completion

import (
	"fmt"
	"strings"
	"testing"
)

func importFiles(t *testing.T, files []SourceFile, opts ImportOptions) (*Catalog, ImportStats) {
	t.Helper()
	meta := Metadata{Source: "test", Release: "v1", Commit: "abcdef", License: "MIT"}
	catalog, stats, err := Import(files, meta, opts)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return catalog, stats
}

func TestImportCommandTree(t *testing.T) {
	files := []SourceFile{
		{Name: "git/git.yaml", Data: []byte(`
name: git <tree-ish>
description: the stupid content tracker
aliases: [g]
flags:
  --color=: when to use colors
  -C=: run as if started in path
  --hidden&: not offered
  --broken~: unsupported modifier
  -P, --no-pager: do not pipe output
persistentflags:
  --version: print version
completion:
  flag:
    color:
      - always
      - auto	"pick" the mode
      - $Dynamic
      - "unsafe value"
`)},
		{Name: "git/git.commit.yaml", Data: []byte(`
name: commit [options]
description: record changes
commands:
  - name: amend
    description: amend the previous commit
`)},
		{Name: "git/git.bogus.yaml", Data: []byte(`
name: other
description: mismatched name
`)},
	}
	catalog, stats := importFiles(t, files, ImportOptions{})
	if catalog.RootCount() != 1 || catalog.NodeCount() != 3 {
		t.Fatalf("roots=%d nodes=%d, want 1 and 3", catalog.RootCount(), catalog.NodeCount())
	}
	if stats.SkippedFiles != 1 || stats.SkippedFlags == 0 || stats.SkippedValues == 3 || stats.HiddenFlags != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	git := catalog.Root("g")
	if git == nil || git.Token != "git" {
		t.Fatalf("root alias did not resolve: %+v", git)
	}
	color, ok := catalog.FindFlag(git, "--color")
	if !ok || !color.Value || len(color.Values) != 2 {
		t.Fatalf("--color = %+v, %v", color, ok)
	}
	if color.Values[1].Text != "auto" || color.Values[1].Desc != `"pick" the mode` {
		t.Fatalf("value description = %+v", color.Values[1])
	}
	if _, ok := catalog.FindFlag(git, "--hidden"); ok {
		t.Fatal("hidden flag was imported")
	}
	commit := catalog.Lookup([]string{"git", "commit"})
	if commit == nil || commit.Desc != "record changes" {
		t.Fatalf("git commit = %+v", commit)
	}
	amend := catalog.Lookup([]string{"git", "commit", "amend"})
	if amend == nil || amend.Desc != "amend the previous commit" {
		t.Fatalf("git commit amend = %+v", amend)
	}
	if catalog.Lookup([]string{"git", "bogus"}) != nil {
		t.Fatal("a file whose name disagrees with its path was imported")
	}
}

func TestImportSkipsRootlessDirectories(t *testing.T) {
	files := []SourceFile{
		{Name: "orphan/orphan.child.yaml", Data: []byte("name: child\ndescription: child\n")},
	}
	_, stats, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{})
	if err == nil {
		t.Fatal("a catalog without roots was accepted")
	}
	if stats.RootlessDirs != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestImportRequiresMetadata(t *testing.T) {
	files := []SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\n")}}
	_, _, err := Import(files, Metadata{Source: "s", License: "MIT"}, ImportOptions{})
	if err == nil {
		t.Fatal("missing commit was accepted")
	}
}

func TestImportBoundsFileSize(t *testing.T) {
	files := []SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\n")}}
	_, _, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{MaxFileBytes: 4})
	if err == nil {
		t.Fatal("a catalog without roots was accepted")
	}
	if !strings.Contains(err.Error(), "a/a.yaml") {
		t.Fatalf("error lost source path: %v", err)
	}
}

func TestParseFlagSpec(t *testing.T) {
	cases := []struct {
		spec   string
		flag   Flag
		hidden bool
		ok     bool
	}{
		{"--all", Flag{Long: "all"}, false, true},
		{"-a, --all", Flag{Long: "all", Short: "a"}, false, true},
		{"-a", Flag{Short: "a"}, false, true},
		{"--color=", Flag{Long: "color", Value: true}, false, true},
		{"--color?", Flag{Long: "color", Value: true, Optional: true}, false, true},
		{"--tag=*", Flag{Long: "tag", Value: true, Repeatable: true}, false, true},
		{"--file=!", Flag{Long: "file", Value: true, Required: true}, false, true},
		{"--secret&", Flag{Long: "secret"}, true, true},
		{"--bad~", Flag{}, false, false},
		{"--", Flag{}, false, false},
		{"-a, -b", Flag{}, false, false},
	}
	for _, c := range cases {
		flag, hidden, ok := parseFlagSpec(c.spec)
		if ok != c.ok {
			t.Errorf("%q ok=%v, want %v", c.spec, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if flag.Long != c.flag.Long || flag.Short != c.flag.Short || flag.Value != c.flag.Value ||
			flag.Optional != c.flag.Optional || flag.Repeatable != c.flag.Repeatable || flag.Required != c.flag.Required {
			t.Errorf("%q = %+v, want %+v", c.spec, flag, c.flag)
		}
		if hidden != c.hidden {
			t.Errorf("%q hidden=%v, want %v", c.spec, hidden, c.hidden)
		}
	}
}

func TestImportValueBounds(t *testing.T) {
	values := make([]string, 0, maxValuesPerFlag+5)
	for i := 0; i < maxValuesPerFlag+5; i++ {
		values = append(values, fmt.Sprintf("value%d", i))
	}
	data := "name: a\nflags:\n  --mode=: mode\ncompletion:\n  flag:\n    mode:\n"
	for _, value := range values {
		data += "      - " + value + "\n"
	}
	catalog, stats := importFiles(t, []SourceFile{{Name: "a/a.yaml", Data: []byte(data)}}, ImportOptions{})
	flag, _ := catalog.FindFlag(catalog.Root("a"), "--mode")
	if len(flag.Values) != maxValuesPerFlag {
		t.Fatalf("values = %d, want %d", len(flag.Values), maxValuesPerFlag)
	}
	if stats.SkippedValues == 0 {
		t.Fatal("overflowing values were not counted")
	}
}

func TestImportSkipsStructuralErrors(t *testing.T) {
	for name, data := range map[string]string{
		"invalid flag list":    "name: child\nflags: [not-a-map]\n",
		"invalid scalar flags": "name: child\nflags: unexpected\n",
		"invalid description":  "name: child\nflags:\n  --all: [bad]\n",
		"invalid command":      "name: child\ncommands: [bad]\n",
		"unrepairable yaml":    "name: child\nflags:\n\t--tab: value\n",
		"empty yaml":           "",
		"null root":            "null",
		"second document":      "name: child\n---\nflags: [bad]\n",
	} {
		t.Run(name, func(t *testing.T) {
			files := []SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\n")}, {Name: "a/a.child.yaml", Data: []byte(data)}}
			catalog, stats, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{})
			if err != nil {
				t.Fatalf("one unparsable file failed the whole import: %v", err)
			}
			if stats.SkippedSources != 1 || stats.RepairedFiles != 0 {
				t.Fatalf("stats = %+v", stats)
			}
			if catalog.RootCount() != 1 || catalog.Lookup([]string{"a", "child"}) != nil {
				t.Fatalf("roots = %d, child imported", catalog.RootCount())
			}
		})
	}
}

func TestImportRepairsAPlainScalarWithMappingColons(t *testing.T) {
	files := []SourceFile{{Name: "mpv/mpv.yaml", Data: []byte(`
name: mpv
description: a media player
flags:
    --osd-align-x?: Control to which corner of the screen OSD should be aligned to
    --osd-align-y?: Vertical position (default: top). Details see --osd-align-x
`)}}
	catalog, stats := importFiles(t, files, ImportOptions{})
	if stats.RepairedFiles != 1 || stats.SkippedSources != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	flag, ok := catalog.FindFlag(catalog.Root("mpv"), "--osd-align-y")
	if !ok || flag.Desc != "Vertical position (default: top). Details see --osd-align-x" {
		t.Fatalf("repaired flag = %+v, found=%v", flag, ok)
	}
}

func TestImportDoesNotRepairNonPlainScalars(t *testing.T) {
	for _, value := range []string{`[safe]: text`, `"safe": text`, `'safe': text`, `{description: safe}: text`} {
		t.Run(value, func(t *testing.T) {
			files := []SourceFile{
				{Name: "a/a.yaml", Data: []byte("name: a\n")},
				{Name: "tool/tool.yaml", Data: []byte("name: tool\nflags:\n  --mode: " + value + "\n")},
			}
			catalog, stats := importFiles(t, files, ImportOptions{})
			if stats.RepairedFiles != 0 || stats.SkippedSources != 1 || catalog.Root("tool") != nil {
				t.Fatalf("non-plain scalar was guessed into a description: stats=%+v root=%+v", stats, catalog.Root("tool"))
			}
		})
	}
}

func TestImportDoesNotRepairNonDescriptionFields(t *testing.T) {
	for _, data := range []string{
		"name: tool\nrun: echo: hello\n",
		"name: tool\nrun:\n  --mode: echo: hello\n",
		"name: tool\ndocumentation:\n  description: example: text\n",
	} {
		files := []SourceFile{
			{Name: "a/a.yaml", Data: []byte("name: a\n")},
			{Name: "tool/tool.yaml", Data: []byte(data)},
		}
		catalog, stats := importFiles(t, files, ImportOptions{})
		if stats.RepairedFiles != 0 || stats.SkippedSources != 1 || catalog.Root("tool") != nil {
			t.Fatalf("non-description field was repaired: source=%q stats=%+v", data, stats)
		}
	}
}

func TestImportRepairsSupportedDescriptionPaths(t *testing.T) {
	for name, source := range map[string]string{
		"command":         "name: tool\ndescription: example: text\n",
		"persistent flag": "name: tool\npersistentflags:\n  --mode: example: text\n",
		"flag list":       "name: tool\nflags:\n  - --mode: example: text\n",
		"structured flag": "name: tool\nflags:\n  --mode:\n    description: example: text\n",
		"nested command":  "name: tool\ncommands:\n  - name: child\n    description: example: text\n",
	} {
		t.Run(name, func(t *testing.T) {
			catalog, stats := importFiles(t, []SourceFile{{Name: "tool/tool.yaml", Data: []byte(source)}}, ImportOptions{})
			if stats.RepairedFiles != 1 || stats.SkippedSources != 0 {
				t.Fatalf("description was not repaired: %+v", stats)
			}
			root := catalog.Root("tool")
			desc := root.Desc
			if name == "nested command" {
				desc = catalog.Lookup([]string{"tool", "child"}).Desc
			} else if name != "command" {
				flag, ok := catalog.FindFlag(root, "--mode")
				if !ok {
					t.Fatal("repaired option is missing")
				}
				desc = flag.Desc
			}
			if desc != "example: text" {
				t.Fatalf("description = %q", desc)
			}
		})
	}
}

func TestImportRepairsEveryDefectLineAndKeepsItsText(t *testing.T) {
	files := []SourceFile{{Name: "zig/zig.yaml", Data: []byte("name: zig\n")}, {Name: "zig/zig.run.yaml", Data: []byte(`
name: run [options] [files] [-- args]
description: Create executable and run immediately
flags:
    --growable-table: WebAssembly: remove maximum size from function table, allowing table to grow
    --seed=: Override the random seed (default: 0)
    --note: It's a 'quoted': value
`)}}
	catalog, stats := importFiles(t, files, ImportOptions{})
	if stats.RepairedFiles != 1 || stats.SkippedSources != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	run := catalog.Lookup([]string{"zig", "run"})
	for _, test := range []struct{ name, desc string }{
		{"--growable-table", "WebAssembly: remove maximum size from function table, allowing table to grow"},
		{"--seed", "Override the random seed (default: 0)"},
		{"--note", "It's a 'quoted': value"},
	} {
		flag, ok := catalog.FindFlag(run, test.name)
		if !ok || flag.Desc != test.desc {
			t.Errorf("%s = %+v, found=%v; want %q", test.name, flag, ok, test.desc)
		}
	}
}

func TestImportSkipsASourceWhenRepairCannotMakeItParse(t *testing.T) {
	files := []SourceFile{
		{Name: "a/a.yaml", Data: []byte("name: a\n")},
		{Name: "a/a.broken.yaml", Data: []byte("name: broken\nflags:\n  --all: first: second\n  --bad: [not, a, description]\n")},
	}
	catalog, stats, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{})
	if err != nil {
		t.Fatalf("a file that repair cannot fix failed the import: %v", err)
	}
	if stats.SkippedSources != 1 || stats.RepairedFiles != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if catalog.RootCount() != 1 || catalog.Lookup([]string{"a", "broken"}) != nil {
		t.Fatalf("roots = %d", catalog.RootCount())
	}
}

func TestImportBoundsRepairsPerSource(t *testing.T) {
	var data strings.Builder
	data.WriteString("name: many\nflags:\n")
	for index := 0; index <= maxRepairsPerSource; index++ {
		fmt.Fprintf(&data, "  --flag%d: first: second\n", index)
	}
	files := []SourceFile{
		{Name: "a/a.yaml", Data: []byte("name: a\n")},
		{Name: "a/a.many.yaml", Data: []byte(data.String())},
	}
	catalog, stats, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{})
	if err != nil {
		t.Fatalf("a file over the repair bound failed the import: %v", err)
	}
	if stats.SkippedSources != 1 || stats.RepairedFiles != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if catalog.RootCount() != 1 {
		t.Fatalf("roots = %d", catalog.RootCount())
	}
}

// A release may carry a handful of broken files, but a source that lost most
// of its documents must fail instead of installing a partial catalog.
func TestImportBoundsToleratedUnparsableSources(t *testing.T) {
	root := SourceFile{Name: "a/a.yaml", Data: []byte("name: a\n")}
	broken := func(index int) SourceFile {
		return SourceFile{Name: fmt.Sprintf("a/a.child%02d.yaml", index), Data: []byte("name: child\nflags: [not-a-map]\n")}
	}
	tolerated := []SourceFile{root}
	for index := 0; index < 16; index++ {
		tolerated = append(tolerated, broken(index))
	}
	catalog, stats, err := Import(tolerated, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{})
	if err != nil {
		t.Fatalf("16 skipped sources were rejected: %v", err)
	}
	if stats.SkippedSources != 16 || catalog.RootCount() != 1 {
		t.Fatalf("stats = %+v, roots = %d", stats, catalog.RootCount())
	}

	tooMany := append(append([]SourceFile(nil), tolerated...), broken(16))
	if _, stats, err := Import(tooMany, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{}); err == nil || !strings.Contains(err.Error(), "could not be parsed") {
		t.Fatalf("17 skipped sources = %v (stats %+v)", err, stats)
	}
}

func TestImportHandlesAV182LikeSourceSet(t *testing.T) {
	files := []SourceFile{
		{Name: "mpv/mpv.yaml", Data: []byte("name: mpv\nflags:\n    --osd-align-y?: Vertical position (default: top). Details see --osd-align-x\n")},
		{Name: "zig/zig.yaml", Data: []byte("name: zig\n")},
		{Name: "zig/zig.run.yaml", Data: []byte("name: run [options]\nflags:\n    --growable-table: WebAssembly: remove maximum size from function table, allowing table to grow\n")},
		{Name: "zig/zig.build-exe.yaml", Data: []byte("name: build-exe\nflags:\n    --growable-table: WebAssembly: remove maximum size from function table, allowing table to grow\n")},
		{Name: "plain/plain.yaml", Data: []byte("name: plain\n")},
	}
	catalog, stats := importFiles(t, files, ImportOptions{})
	if stats.RepairedFiles != 3 || stats.SkippedSources != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if catalog.RootCount() != 3 || catalog.Lookup([]string{"zig", "run"}) == nil || catalog.Lookup([]string{"zig", "build-exe"}) == nil {
		t.Fatalf("roots = %d", catalog.RootCount())
	}
}

func TestImportStructuredFlagDescription(t *testing.T) {
	catalog, _ := importFiles(t, []SourceFile{{Name: "wt/wt.yaml", Data: []byte("name: wt\nflags:\n  --format=:\n    description: Output format\n    default: text\ncompletion:\n  flag:\n    format: [text, json]\n")}}, ImportOptions{})
	flag, ok := catalog.FindFlag(catalog.Root("wt"), "--format")
	if !ok || flag.Desc != "Output format" || len(flag.Values) != 2 {
		t.Fatalf("structured flag = %+v, found=%v", flag, ok)
	}
}

func TestImportRejectsOversizedChild(t *testing.T) {
	files := []SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\n")}, {Name: "a/a.child.yaml", Data: []byte("name: child\ndescription: too long\n")}}
	if _, _, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{MaxFileBytes: 12}); err == nil {
		t.Fatal("oversized child was silently dropped")
	}
}

func TestImportRejectsChildDecodeLimits(t *testing.T) {
	files := []SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\n")}, {Name: "a/a.child.yaml", Data: []byte("name: child\nflags:\n  --format=:\n    description: Output\n")}}
	for _, opts := range []ImportOptions{{MaxYAMLNodes: 8}, {MaxYAMLDepth: 3}} {
		if _, _, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, opts); err == nil || !strings.Contains(err.Error(), "a/a.child.yaml") {
			t.Fatalf("decode limit did not reject child with its path: %v", err)
		}
	}
}

func TestImportRejectsDuplicateSource(t *testing.T) {
	malformed := "name: child\nflags:\n  --all: [bad]\n"
	valid := "name: child\n"
	cases := map[string][]SourceFile{
		"two valid files": {
			{Name: "a/a.yaml", Data: []byte("name: a\n")},
			{Name: "a/a.yaml", Data: []byte("name: a\ndescription: overwritten\n")},
		},
		"malformed then valid": {
			{Name: "a/a.yaml", Data: []byte("name: a\n")},
			{Name: "a/a.child.yaml", Data: []byte(malformed)},
			{Name: "a/a.child.yaml", Data: []byte(valid)},
		},
		"valid then malformed": {
			{Name: "a/a.yaml", Data: []byte("name: a\n")},
			{Name: "a/a.child.yaml", Data: []byte(valid)},
			{Name: "a/a.child.yaml", Data: []byte(malformed)},
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{}); err == nil || !strings.Contains(err.Error(), "duplicate source") {
				t.Fatalf("duplicate source = %v", err)
			}
		})
	}
}

func TestImportRejectsCommandDepthLimit(t *testing.T) {
	for name, file := range map[string]SourceFile{
		"file path":      {Name: "a/a." + strings.Repeat("child.", maxDepth) + "last.yaml", Data: []byte("name: last\n")},
		"nested command": {Name: "a/a." + strings.Repeat("child.", maxDepth-2) + "last.yaml", Data: []byte("name: last\ncommands:\n  - name: nested\n")},
	} {
		t.Run(name, func(t *testing.T) {
			files := []SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\n")}, file}
			if _, _, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, ImportOptions{}); err == nil {
				t.Fatal("deep command was silently skipped")
			}
		})
	}
}

func TestImportAllowsFileAtSizeLimit(t *testing.T) {
	data := []byte("name: a\n")
	catalog, _ := importFiles(t, []SourceFile{{Name: "a/a.yaml", Data: data}}, ImportOptions{MaxFileBytes: len(data)})
	if catalog.Root("a") == nil {
		t.Fatal("source exactly at size limit was rejected")
	}
}

// Repair only corrects quoting: everything the importer refuses must still be
// refused after a repair, and a repaired description is sanitized like any
// other.
func TestRepairDoesNotBypassValuePolicy(t *testing.T) {
	files := []SourceFile{{Name: "tool/tool.yaml", Data: []byte(
		"name: tool\n" +
			"flags:\n" +
			"  --run=: Execute $(id): now\n" +
			"completion:\n" +
			"  flag:\n" +
			"    run:\n" +
			"      - $(id)\n" +
			"      - safe\n",
	)}}
	catalog, stats := importFiles(t, files, ImportOptions{})
	if stats.RepairedFiles != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	flag, ok := catalog.FindFlag(catalog.Root("tool"), "--run")
	if !ok || flag.Desc != "Execute $(id): now" {
		t.Fatalf("repaired option = %+v, found=%v", flag, ok)
	}
	if len(flag.Values) != 1 || flag.Values[0].Text != "safe" {
		t.Fatalf("values = %+v", flag.Values)
	}
	if stats.SkippedValues == 0 {
		t.Fatal("the repaired file's dynamic value was not counted")
	}
}

func TestRepairStillSanitizesDescriptions(t *testing.T) {
	files := []SourceFile{{Name: "tool/tool.yaml", Data: []byte("name: tool\nflags:\n  --align?: a\u202eb (default: c)\n")}}
	catalog, stats := importFiles(t, files, ImportOptions{})
	if stats.RepairedFiles != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	flag, ok := catalog.FindFlag(catalog.Root("tool"), "--align")
	if !ok || flag.Desc != "ab (default: c)" {
		t.Fatalf("repaired description = %+v, found=%v", flag, ok)
	}
}

func TestRepairDoesNotBypassYAMLLimits(t *testing.T) {
	files := []SourceFile{{Name: "tool/tool.yaml", Data: []byte("name: tool\nflags:\n  --align?: Vertical (default: top): now\n")}}
	for name, opts := range map[string]ImportOptions{
		"node limit":  {MaxYAMLNodes: 5},
		"depth limit": {MaxYAMLDepth: 2},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Import(files, Metadata{Source: "s", Commit: "c", License: "MIT"}, opts); err == nil || !strings.Contains(err.Error(), "tool/tool.yaml") {
				t.Fatalf("repair bypassed the limit: %v", err)
			}
		})
	}
}

func TestImportStripsInvisibleCharactersFromDescriptions(t *testing.T) {
	catalog, _ := importFiles(t, []SourceFile{{Name: "spoof/spoof.yaml", Data: []byte(
		"name: spoof\n" +
			"description: run\u202Eevil\n" +
			"flags:\n" +
			"  --all=: show\u200Fall\n" +
			"completion:\n" +
			"  flag:\n" +
			"    all:\n" +
			"      - plain\tvalue\u200Fdesc\n",
	)}}, ImportOptions{})
	root := catalog.Root("spoof")
	if root == nil || root.Desc != "runevil" {
		t.Fatalf("root description = %+v", root)
	}
	flag, ok := catalog.FindFlag(root, "--all")
	if !ok || flag.Desc != "showall" {
		t.Fatalf("flag = %+v, found=%v", flag, ok)
	}
	if len(flag.Values) != 1 || flag.Values[0].Desc != "valuedesc" {
		t.Fatalf("value descriptions = %+v", flag.Values)
	}
}
