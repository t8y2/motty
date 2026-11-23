package completion

import (
	"strings"
	"unicode"
)

// MaxCandidates bounds one answer. The list shows fewer rows than this; the
// bound exists so a broad prefix cannot build an unbounded response.
const MaxCandidates = 100

const (
	maxLineBytes = 4096
	maxTokens    = 128
)

// Kind tells the caller what a candidate is. The list uses it for ordering and
// the ghost suggestion only accepts a unique command, option or value.
type Kind uint8

const (
	KindCommand Kind = iota
	KindOption
	KindValue
	// KindPath marks a filesystem entry the workbench read from the session's
	// own host. The catalog never produces it: it is the application's answer,
	// not a command's syntax, and only its own escaping is valid for it.
	KindPath
)

// Candidate is one possible continuation. Text is the complete token; Insert is
// the suffix that is appended to the current word, which is never rewritten.
type Candidate struct {
	Text   string
	Insert string
	Desc   string
	Kind   Kind
	// FromCatalog marks text built from the command catalog. The workbench
	// revalidates exactly those insertions before typing them; suggestions
	// learned from the user's own history are not restricted this way.
	FromCatalog bool
	// Dir marks a KindPath candidate that names a folder. It has no meaning for
	// the other kinds and is used only to choose the row's glyph.
	Dir bool
}

// Result is the answer for one line. Suppressed reports syntax this package
// does not understand: the caller must leave the input alone and close any
// list, because no suffix can be proven safe.
type Result struct {
	Prefix     string
	Word       string
	Candidates []Candidate
	Suppressed bool
}

// commandWrapper describes a root command that runs a command line of its own:
// the syntax of the wrapped command answers what follows, while the wrapper's
// own options answer while they are still being typed. A command's
// specification cannot say that its arguments are another command, so the set
// is explicit. It is the subcommand-offset idea that bash-completion and fish
// ship for the same commands.
type commandWrapper struct {
	// operands is how many of the wrapper's own positional arguments come
	// before the wrapped command (timeout's DURATION).
	operands int
	// assignments skips NAME=VALUE operands (env).
	assignments bool
	// fileFlags are options after which the wrapper takes files rather than a
	// command (sudo's -e/--edit). Nothing after them is completed from the
	// catalog.
	fileFlags []string
}

// commandWrappers are the wrappers whose own options the catalog knows. A
// wrapper that is not here keeps the plain first-word interpretation.
var commandWrappers = map[string]commandWrapper{
	"sudo":    {fileFlags: []string{"-e", "--edit"}},
	"doas":    {},
	"env":     {assignments: true},
	"nice":    {},
	"nohup":   {},
	"timeout": {operands: 1},
	"watch":   {},
	"xargs":   {},
}

// commandOffset returns the index of the command whose syntax answers tokens
// after the wrappers in front of it, and that command's root (nil when the
// catalog does not know it). pending is the wrapper whose wrapped command is
// still the word being typed. A line that ends inside a wrapper's own options
// or operands returns start, the wrapper to complete instead.
func commandOffset(catalog *Catalog, tokens []string) (start int, root, pending *Node) {
	if len(tokens) == 0 {
		return 0, nil, nil
	}
	for start = 0; ; {
		root = catalog.Root(tokens[start])
		if root == nil {
			return start, nil, nil
		}
		wrapper, ok := commandWrappers[root.Token]
		if !ok {
			return start, root, nil
		}
		next, atCommand := wrapper.offset(catalog, root, tokens[start+1:])
		if !atCommand {
			return start, root, nil
		}
		if next < 0 {
			return len(tokens), nil, root
		}
		start += next + 1
	}
}

// offset locates the wrapped command in args, the tokens after the wrapper
// itself. next is the command's index, or -1 when the wrapped command is the
// word being typed. atCommand is false while args is still inside the
// wrapper's own options or operands; an option this catalog cannot parse also
// stops the scan so the plain walk reports the suppression.
func (wrapper commandWrapper) offset(catalog *Catalog, node *Node, args []string) (next int, atCommand bool) {
	var expectValue, flagsStopped bool
	operands := 0
	for i, token := range args {
		if expectValue {
			expectValue = false
			continue
		}
		if !flagsStopped && token == "--" {
			flagsStopped = true
			continue
		}
		if !flagsStopped && strings.HasPrefix(token, "-") && len(token) > 1 {
			flag, attached, ok := wrapperFlag(catalog, node, token)
			if !ok || wrapper.fileFlag(token) {
				return 0, false
			}
			if !attached && flag.Value && !flag.Optional {
				expectValue = true
			}
			continue
		}
		if wrapper.assignments && isAssignment(token) {
			continue
		}
		if operands < wrapper.operands {
			operands++
			continue
		}
		return i, true
	}
	if expectValue || operands < wrapper.operands {
		return 0, false
	}
	return -1, true
}

// wrapperFlag resolves one of a wrapper's own option tokens. attached reports
// a value spelled with '='; ok is false for syntax the matcher refuses
// everywhere: an unknown option or a short-option cluster.
func wrapperFlag(catalog *Catalog, node *Node, token string) (flag Flag, attached, ok bool) {
	if strings.HasPrefix(token, "--") {
		name, _, hasValue := strings.Cut(token, "=")
		flag, ok = catalog.FindFlag(node, name)
		return flag, hasValue, ok
	}
	if len(token) != 2 {
		return Flag{}, false, false
	}
	flag, ok = catalog.FindFlag(node, token)
	return flag, false, ok
}

// fileFlag reports an option that switches the wrapper to file arguments.
func (wrapper commandWrapper) fileFlag(token string) bool {
	name, _, _ := strings.Cut(token, "=")
	for _, flag := range wrapper.fileFlags {
		if name == flag {
			return true
		}
	}
	return false
}

// isAssignment reports a NAME=VALUE environment operand.
func isAssignment(token string) bool {
	name, _, ok := strings.Cut(token, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if !(unicode.IsLetter(r) || r == '_' || i > 0 && unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// addWrappedCommand answers the position of the command a wrapper runs. An
// option word still belongs to the wrapper; anything else names a command.
func addWrappedCommand(catalog *Catalog, wrapper *Node, word string, result *Result) {
	switch {
	case strings.HasPrefix(word, "--"):
		addLongFlags(catalog, wrapper, word, nil, result)
	case strings.HasPrefix(word, "-"):
		addShortFlags(catalog, wrapper, word, nil, result)
	default:
		addRoots(catalog, word, result)
	}
}

// Complete answers what the end of line could continue into. The line is
// expected to be a single command line with the cursor at its end; anything
// else is treated as unsupported rather than guessed at.
func Complete(catalog *Catalog, line string) Result {
	if catalog == nil {
		return Result{Suppressed: true}
	}
	if len(line) > maxLineBytes || strings.ContainsAny(line, "\t\r\n") {
		return Result{Suppressed: true}
	}
	for _, r := range line {
		if !lineRune(r) {
			return Result{Suppressed: true}
		}
	}
	if strings.HasPrefix(line, " ") || strings.Contains(line, "  ") {
		// A leading space hides the command from shell history, and an empty
		// token makes the word boundaries ambiguous.
		return Result{Suppressed: true}
	}

	var prefix, word string
	if strings.HasSuffix(line, " ") {
		prefix, word = line, ""
	} else if index := strings.LastIndexByte(line, ' '); index >= 0 {
		prefix, word = line[:index+1], line[index+1:]
	} else {
		word = line
	}

	result := Result{Prefix: prefix, Word: word}
	var tokens []string
	if trimmed := strings.TrimSuffix(prefix, " "); trimmed != "" {
		tokens = strings.Split(trimmed, " ")
		if len(tokens) > maxTokens {
			return Result{Suppressed: true}
		}
	}
	if len(tokens) == 0 {
		if word == "" {
			addRoots(catalog, "", &result)
		} else {
			addRoots(catalog, word, &result)
		}
		return result
	}

	start, root, pending := commandOffset(catalog, tokens)
	if pending != nil {
		// A wrapper is waiting for the command it runs: the next word is a
		// command, unless it is spelled as one of the wrapper's own options.
		addWrappedCommand(catalog, pending, word, &result)
		return result
	}
	tokens = tokens[start:]
	if root == nil {
		// An unknown command may still be a local script; the static catalog
		// has nothing to say, and history suggestions are handled by the caller.
		return result
	}

	node := root
	var expect *Flag
	positional := false
	flagsStopped := false
	used := make(map[string]bool)
	for _, token := range tokens[1:] {
		if expect != nil {
			expect = nil
			continue
		}
		if !flagsStopped {
			if token == "--" {
				flagsStopped = true
				continue
			}
			if strings.HasPrefix(token, "--") {
				name, _, hasValue := strings.Cut(token, "=")
				flag, ok := catalog.FindFlag(node, name)
				if !ok {
					return Result{Suppressed: true}
				}
				used[flag.Token()] = true
				if !hasValue && flag.Value && !flag.Optional {
					expect = &flag
				}
				continue
			}
			if strings.HasPrefix(token, "-") && len(token) > 1 {
				if len(token) != 2 {
					// Short-option clusters and long options spelled with one
					// dash are not understood.
					return Result{Suppressed: true}
				}
				flag, ok := catalog.FindFlag(node, token)
				if !ok {
					return Result{Suppressed: true}
				}
				used[flag.Token()] = true
				if flag.Value && !flag.Optional {
					expect = &flag
				}
				continue
			}
		}
		if flagsStopped || positional || token == "-" {
			positional = true
			continue
		}
		if child := childByToken(node, token); child != nil {
			node = child
			continue
		}
		// A host's subcommands are not all in the snapshot: carapace finds
		// docker/git/gh/cargo plugins by running the host at completion time.
		// The offline catalog keeps them as "<host>-<name>" roots, so the
		// second token falls back to that root. node == root keeps this to the
		// position directly below the host.
		if node == root {
			if plugin := pluginCommand(catalog, root.Token, token); plugin != nil {
				node = plugin
				continue
			}
		}
		positional = true
	}
	if expect != nil {
		addValues(expect, word, &result)
		return result
	}
	if flagsStopped {
		return result
	}
	if word == "" {
		if positional {
			return result
		}
		addChildren(catalog, node, root, "", &result)
		addLongFlags(catalog, node, "", used, &result)
		return result
	}
	if strings.HasPrefix(word, "--") {
		if name, value, hasValue := strings.Cut(word, "="); hasValue {
			flag, ok := catalog.FindFlag(node, name)
			if !ok || !flag.Value {
				return result
			}
			addValues(&flag, value, &result)
			return result
		}
		addLongFlags(catalog, node, word, used, &result)
		return result
	}
	if strings.HasPrefix(word, "-") {
		if len(word) > 2 {
			// A short-option cluster is complete syntax of its own; do not guess.
			return result
		}
		addShortFlags(catalog, node, word, used, &result)
		return result
	}
	if positional {
		return result
	}
	addChildren(catalog, node, root, word, &result)
	return result
}

// lineRune accepts only characters that cannot change the meaning of the line
// for a shell or for this tokenizer. Quotes, expansions, redirects, globs and
// command separators are all refused.
func lineRune(r rune) bool {
	switch {
	case r >= 0x80:
		return true
	case unicode.IsLetter(r) || unicode.IsDigit(r):
		return true
	}
	return r == ' ' || strings.ContainsRune("-_.:@%+,=/", r)
}

func childByToken(node *Node, token string) *Node {
	node.load()
	for _, child := range node.Children {
		if child.Token == token {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == token {
				return child
			}
		}
	}
	return nil
}

func addRoots(catalog *Catalog, prefix string, result *Result) {
	for _, root := range catalog.Roots() {
		addCandidate(result, root.Token, prefix, root.Desc, KindCommand)
		for _, alias := range root.Aliases {
			addCandidate(result, alias, prefix, root.Desc, KindCommand)
		}
	}
}

// addChildren lists a node's subcommands and, directly below a plugin host,
// the subcommands that host resolves from PATH. Both sets are in name order and
// merged so the list stays alphabetical; a real subcommand wins over a plugin
// of the same name because that is what the token resolves to.
func addChildren(catalog *Catalog, node, root *Node, prefix string, result *Result) {
	node.load()
	var plugins []PluginCommand
	if node == root {
		plugins = catalog.Plugins(root.Token)
	}
	children := node.Children
	child, plugin := 0, 0
	for {
		for child < len(children) && children[child].Hidden {
			child++
		}
		switch {
		case child >= len(children) && plugin >= len(plugins):
			return
		case plugin >= len(plugins) || (child < len(children) && children[child].Token < plugins[plugin].Name):
			current := children[child]
			addCandidate(result, current.Token, prefix, current.Desc, KindCommand)
			for _, alias := range current.Aliases {
				addCandidate(result, alias, prefix, current.Desc, KindCommand)
			}
			child++
		default:
			current := plugins[plugin]
			plugin++
			if childByToken(node, current.Name) != nil {
				// A real subcommand, visible or not, or one of its aliases
				// claims the token; the plugin of that name is unreachable.
				continue
			}
			addCandidate(result, current.Name, prefix, current.Node.Desc, KindCommand)
		}
	}
}

func addLongFlags(catalog *Catalog, node *Node, prefix string, used map[string]bool, result *Result) {
	for _, flag := range catalog.VisibleFlags(node) {
		if flag.Long == "" {
			continue
		}
		if used[flag.Token()] && !flag.Repeatable {
			continue
		}
		addCandidate(result, flag.Token(), prefix, flag.Desc, KindOption)
	}
}

func addShortFlags(catalog *Catalog, node *Node, prefix string, used map[string]bool, result *Result) {
	for _, flag := range catalog.VisibleFlags(node) {
		if flag.Short == "" {
			continue
		}
		if used[flag.Token()] && !flag.Repeatable {
			continue
		}
		addCandidate(result, flag.ShortToken(), prefix, flag.Desc, KindOption)
	}
}

func addValues(flag *Flag, prefix string, result *Result) {
	for _, value := range flag.Values {
		if !strings.HasPrefix(value.Text, prefix) {
			continue
		}
		addCandidate(result, value.Text, prefix, value.Desc, KindValue)
	}
}

func addCandidate(result *Result, text, prefix, desc string, kind Kind) {
	if len(result.Candidates) >= MaxCandidates {
		return
	}
	if !strings.HasPrefix(text, prefix) || text == prefix {
		return
	}
	duplicate := false
	for _, existing := range result.Candidates {
		if existing.Text == text && existing.Kind == kind {
			duplicate = true
			break
		}
	}
	if duplicate {
		return
	}
	result.Candidates = append(result.Candidates, Candidate{
		Text:        text,
		Insert:      text[len(prefix):],
		Desc:        desc,
		Kind:        kind,
		FromCatalog: true,
	})
}
