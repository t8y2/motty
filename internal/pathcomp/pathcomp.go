// Package pathcomp answers what a command line's current word is as a file
// path and turns one directory listing into completion rows. It is plain
// string work: it never reads a directory, knows no session and does no I/O.
//
// The workbench owns where the listing comes from (SFTP for a server, the
// local filesystem for this computer) and the shell's current directory. This
// package owns the grammar shared by both: which lines may be completed, the
// table of commands whose arguments are paths, and the escaping that makes a
// remote file name safe to type into a POSIX shell.
package pathcomp

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxLineBytes bounds one line this package will reason about.
	MaxLineBytes = 4096
	// MaxNameBytes bounds one file name offered as a candidate.
	MaxNameBytes = 1024
	// MaxCandidates bounds one answer.
	MaxCandidates = 100
)

// Context is where the line's current word points.
type Context struct {
	// Command is the first word with its directory removed ("/bin/ls" is "ls").
	Command string
	// Word is the current word with backslash escapes removed.
	Word string
	// Dir is the typed directory part, escapes removed, ending in '/'. It is
	// empty for a word that names an entry in the shell's own directory.
	Dir string
	// Base is the final component, escapes removed.
	Base string
	// Query is the directory to read: an absolute path, a path relative to the
	// current directory, or, when Home is set, a path relative to home.
	Query string
	// Home reports that the word's directory starts at the shell's home.
	Home bool
	// Path reports that the current word is a filesystem path to complete.
	Path bool
	// Separate reports that the current word is the command itself: the path is
	// its next argument, so an insertion starts with a space.
	Separate bool
	// Argument reports that the current word is a later word of the line, not
	// the command itself.
	Argument bool
	// ArgumentIndex is how many complete words precede the current one. It is
	// what a dismissal is scoped to within one command line.
	ArgumentIndex int
}

// Entry is one item of a directory listing.
type Entry struct {
	Name string
	Dir  bool
	Link bool
}

// Candidate is one row built from an entry. Text is what the row shows; Insert
// is the escaped suffix appended to the word the user typed, and never rewrites
// what is already on the line.
type Candidate struct {
	Text   string
	Insert string
	Dir    bool
}

// pathCommand is how a command's positionals are read. skip is how many leading
// positionals are not paths (chmod's mode, grep's pattern).
type pathCommand struct{ skip int }

// pathCommands are the commands whose positional arguments are filesystem
// paths. This is product data, not catalog data: the public catalog is command
// syntax imported from Carapace and deliberately carries no dynamic
// completions. A command that is not listed still gets path completion when the
// word itself looks like a path ("." or "/" or "~"), which is how any script
// can be helped.
var pathCommands = map[string]pathCommand{
	"cd": {}, "pushd": {},
	"ls": {}, "cat": {}, "less": {}, "more": {}, "head": {}, "tail": {},
	"mkdir": {}, "rmdir": {}, "touch": {}, "cp": {}, "mv": {}, "rm": {},
	"ln": {}, "stat": {}, "file": {}, "du": {}, "df": {},
	"tar": {}, "unzip": {}, "zip": {}, "gzip": {}, "gunzip": {},
	"vim": {}, "vi": {}, "nvim": {}, "nano": {}, "emacs": {}, "code": {},
	"open":  {},
	"chmod": {skip: 1}, "chown": {skip: 1}, "chgrp": {skip: 1},
	"grep": {skip: 1}, "rg": {skip: 1}, "sed": {skip: 1}, "awk": {skip: 1},
}

// Parse reads the command line and reports where its current word points. ok
// is false for anything whose meaning a bare-word suffix could change: quotes,
// expansions, globs, redirections, operators, control characters. Those lines
// are left alone rather than guessed at.
func Parse(line string) (Context, bool) {
	tokens, word, ok := splitWords(line)
	if !ok {
		return Context{}, false
	}
	if len(tokens) == 0 {
		// The command word is complete but has no space after it yet. A path
		// command is already asking for its argument, so its folder can open
		// here; the insertion supplies the space.
		ctx := Context{Command: commandName(word), Word: word}
		if command, known := pathCommands[ctx.Command]; known && command.skip == 0 {
			ctx.Path, ctx.Separate = true, true
		}
		return ctx, true
	}
	if strings.HasPrefix(word, "~") && word != "~" && !strings.HasPrefix(word, "~/") {
		// "~name" is another account's home, which the session cannot expand.
		return Context{}, false
	}
	ctx := Context{Command: commandName(tokens[0]), Word: word, Argument: true, ArgumentIndex: len(tokens)}
	if word == "~" {
		ctx.Dir, ctx.Base, ctx.Home = "~/", "", true
	} else {
		ctx.Dir, ctx.Base = splitWord(word)
		ctx.Home = strings.HasPrefix(word, "~")
	}
	if ctx.Home {
		ctx.Query = strings.TrimPrefix(ctx.Dir, "~/")
	} else {
		ctx.Query = ctx.Dir
	}
	if strings.HasPrefix(word, "-") {
		return ctx, true // an option, never a path
	}
	pathWord := strings.Contains(word, "/") || strings.HasPrefix(word, "~")
	if !pathWord {
		command, known := pathCommands[ctx.Command]
		if !known || positionalIndex(tokens) < command.skip {
			return ctx, true
		}
	}
	ctx.Path = true
	return ctx, true
}

// splitWords splits the line into the words before the current one, and the
// current word. An unescaped trailing space leaves the current word empty. The
// line is refused when it contains anything a simple bare-word tokenizer
// cannot vouch for.
func splitWords(line string) ([]string, string, bool) {
	if line == "" || len(line) > MaxLineBytes || line[0] == ' ' || strings.Contains(line, "  ") {
		return nil, "", false
	}
	var (
		tokens  []string
		word    strings.Builder
		escaped bool
	)
	for _, r := range line {
		if unrepresentable(r) {
			return nil, "", false
		}
		if escaped {
			word.WriteRune(r)
			escaped = false
			continue
		}
		switch {
		case r == '\\':
			escaped = true
		case r == ' ':
			tokens = append(tokens, word.String())
			word.Reset()
		case wordRune(r):
			word.WriteRune(r)
		default:
			return nil, "", false
		}
	}
	if escaped {
		return nil, "", false // a trailing backslash would escape the suffix
	}
	if word.Len() > 0 {
		return tokens, word.String(), true
	}
	if len(tokens) == 0 {
		return nil, "", false
	}
	return tokens, "", true
}

// wordRune is the bare alphabet a path word may be typed with. Backslash
// escapes everything else a file name could contain.
func wordRune(r rune) bool {
	if r >= utf8.RuneSelf {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.:@%+,=/~", r)
}

// unrepresentable reports a rune that can neither be shown nor typed: controls,
// invisible format characters, separators and variation selectors.
func unrepresentable(r rune) bool {
	if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || unicode.Is(unicode.Cf, r) {
		return true
	}
	return r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0100 && r <= 0xe01ef
}

// commandName is the command a first word invokes, without its directory.
func commandName(word string) string {
	if index := strings.LastIndexByte(word, '/'); index >= 0 {
		return word[index+1:]
	}
	return word
}

// splitWord splits a word into its typed directory (with the trailing slash)
// and its final component.
func splitWord(word string) (dir, base string) {
	if index := strings.LastIndexByte(word, '/'); index >= 0 {
		return word[:index+1], word[index+1:]
	}
	return "", word
}

// positionalIndex counts the non-option words after the command.
func positionalIndex(tokens []string) int {
	index := 0
	for _, token := range tokens[1:] {
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		index++
	}
	return index
}

// Escape returns name as a bare shell word, backslash-escaping everything that
// the shell would otherwise read as syntax. It reports false for a name that
// cannot be typed safely (control characters, invalid UTF-8, a slash).
func Escape(name string) (string, bool) {
	if name == "" || len(name) > MaxNameBytes || !utf8.ValidString(name) ||
		strings.ContainsAny(name, "/\x00") {
		return "", false
	}
	var escaped strings.Builder
	escaped.Grow(len(name))
	for _, r := range name {
		if unrepresentable(r) {
			return "", false
		}
		if safeRune(r) {
			escaped.WriteRune(r)
			continue
		}
		escaped.WriteByte('\\')
		escaped.WriteRune(r)
	}
	return escaped.String(), true
}

// safeRune is a character that needs no escape inside a bare word. Everything
// else a file name can contain is escaped, including '~', '#', '!' and '='.
func safeRune(r rune) bool {
	if r >= utf8.RuneSelf {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.+@%,", r)
}

// SafeInsert reports whether suffix is something Escape could have produced for
// a name. Acceptance checks it immediately before typing: a listing is untrusted
// input, and this is the one alphabet the completion may add to a shell line.
func SafeInsert(suffix string) bool {
	if suffix == "" || len(suffix) > MaxLineBytes || !utf8.ValidString(suffix) {
		return false
	}
	var escaped bool
	for _, r := range suffix {
		if escaped {
			if unrepresentable(r) {
				return false
			}
			escaped = false
			continue
		}
		switch {
		case r == '\\':
			escaped = true
		case r == '/' || safeRune(r):
		default:
			return false
		}
	}
	return !escaped
}

// Candidates turns one directory listing into rows for ctx. Entries are offered
// in the order they arrive, which the lister sorts directories first. Hidden
// names are offered only when the user has started typing a dot, and a name
// that would read as an option is left out: it could never be typed as a
// positional argument the shell would take literally.
func Candidates(ctx Context, entries []Entry) []Candidate {
	if !ctx.Path {
		return nil
	}
	hidden := strings.HasPrefix(ctx.Base, ".")
	out := make([]Candidate, 0, min(len(entries), MaxCandidates))
	for _, entry := range entries {
		if len(out) >= MaxCandidates {
			break
		}
		if entry.Name == "" || entry.Name == "." || entry.Name == ".." {
			continue
		}
		if !hidden && strings.HasPrefix(entry.Name, ".") {
			continue
		}
		if strings.HasPrefix(entry.Name, "-") || !strings.HasPrefix(entry.Name, ctx.Base) {
			continue
		}
		if entry.Name == ctx.Base && !entry.Dir {
			continue // nothing would be added
		}
		suffix := ""
		if rest := entry.Name[len(ctx.Base):]; rest != "" {
			escaped, ok := Escape(rest)
			if !ok {
				continue
			}
			suffix = escaped
		}
		text := ctx.Dir + entry.Name
		if entry.Dir {
			suffix += "/"
			text += "/"
		}
		if suffix == "" {
			continue
		}
		insert := suffix
		if ctx.Separate {
			// The user has typed the command, not an argument: joining the path
			// needs the word separator the completion supplies.
			insert = " " + suffix
		}
		out = append(out, Candidate{Text: text, Insert: insert, Dir: entry.Dir})
	}
	return out
}
