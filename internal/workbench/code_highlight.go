package workbench

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
)

// A small lexer for the code an assistant puts in its answers: enough to tell
// comments, strings, numbers, keywords, calls, shell flags and variables, and the
// keys of JSON and YAML, apart. It reads one line at a time and keeps no state
// between lines, so a block comment or string that runs over several lines is
// colored only where the line shows it. Languages it does not know are left plain.

type codeLanguage struct {
	keywords  map[string]bool
	comment   string // starts a comment that runs to the end of the line
	block     bool   // /* ... */ within a line
	quotes    string // characters that open and close a string
	shell     bool   // $variables, -flags, and the command at the start of a command
	keys      bool   // name: and "name": are keys
	calls     bool   // name( is a call or a definition
	foldCase  bool   // keywords in any case
	diff      bool   // lines are added or removed by their first character
	sections  bool   // [section] lines
	wordExtra string // characters, besides letters, digits and _, in a word
}

func words(list string) map[string]bool {
	set := make(map[string]bool)
	for _, word := range strings.Fields(list) {
		set[word] = true
	}
	return set
}

var codeLanguages = map[string]*codeLanguage{}

func init() {
	register := func(language *codeLanguage, names ...string) {
		for _, name := range names {
			codeLanguages[name] = language
		}
	}
	register(&codeLanguage{
		keywords: words("if then else elif fi for while until do done case esac in function select time"),
		comment:  "#", quotes: "\"'`", shell: true, wordExtra: "-./~:@%+=,",
	}, "sh", "bash", "zsh", "shell", "console", "terminal", "fish")
	register(&codeLanguage{keywords: words("true false null"), quotes: "\"", keys: true}, "json", "jsonc", "json5")
	register(&codeLanguage{
		keywords: words("true false null yes no on off"), comment: "#", quotes: "\"'", keys: true, sections: true, wordExtra: "-./",
	}, "yaml", "yml", "toml", "ini", "properties", "conf", "env")
	register(&codeLanguage{
		keywords: words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var true false nil"),
		comment:  "//", block: true, quotes: "\"'`", calls: true,
	}, "go", "golang")
	register(&codeLanguage{
		keywords: words("and as assert async await break class continue def del elif else except finally for from global if import in is lambda None nonlocal not or pass raise return True False try while with yield"),
		comment:  "#", quotes: "\"'", calls: true,
	}, "py", "python")
	register(&codeLanguage{
		keywords: words("break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new null return super switch this throw try typeof undefined var void while with yield async await of true false interface type enum implements"),
		comment:  "//", block: true, quotes: "\"'`", calls: true,
	}, "js", "javascript", "jsx", "ts", "typescript", "tsx", "node", "mjs")
	register(&codeLanguage{
		keywords: words("if else for while do switch case break continue return struct class enum static const void int char long short unsigned signed float double bool true false null nullptr new delete public private protected fn let mut pub impl use mod match loop trait self Self async await final abstract extends implements import package namespace using template typename include define"),
		comment:  "//", block: true, quotes: "\"'", calls: true,
	}, "c", "h", "cpp", "c++", "cc", "hpp", "java", "rust", "rs", "cs", "csharp", "kotlin", "swift")
	register(&codeLanguage{
		keywords: words("select from where insert into values update set delete create table drop alter join left right inner outer on group by order having limit as and or not null is in like distinct union all primary key foreign references index"),
		comment:  "--", block: true, quotes: "'\"", calls: true, foldCase: true,
	}, "sql", "mysql", "postgres", "postgresql")
	register(&codeLanguage{diff: true}, "diff", "patch")
}

// highlightCode appends to dst the colored stretches of one line of code in the
// language the fence named.
func highlightCode(language, line string, dst []components.MarkdownCodeRun) []components.MarkdownCodeRun {
	if language == "xml" {
		return highlightXML(line, dst)
	}
	lang := codeLanguages[strings.ToLower(language)]
	if lang == nil || line == "" {
		return dst
	}
	if lang.diff {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			return append(dst, components.MarkdownCodeRun{End: len(line), Class: components.CodeKeyword})
		case line[0] == '+':
			return append(dst, components.MarkdownCodeRun{End: len(line), Class: components.CodeAdded})
		case line[0] == '-':
			return append(dst, components.MarkdownCodeRun{End: len(line), Class: components.CodeRemoved})
		case strings.HasPrefix(line, "@@"):
			return append(dst, components.MarkdownCodeRun{End: len(line), Class: components.CodeFunction})
		}
		return dst
	}
	mark := func(start, end int, class components.MarkdownCodeClass) {
		dst = append(dst, components.MarkdownCodeRun{Start: start, End: end, Class: class})
	}
	if lang.sections && strings.HasPrefix(strings.TrimSpace(line), "[") {
		mark(0, len(line), components.CodeKey)
		return dst
	}
	command := lang.shell
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case lang.comment != "" && strings.HasPrefix(line[i:], lang.comment) && (!lang.shell || i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			mark(i, len(line), components.CodeComment)
			return dst
		case lang.block && strings.HasPrefix(line[i:], "/*"):
			end := len(line)
			if at := strings.Index(line[i+2:], "*/"); at >= 0 {
				end = i + 2 + at + 2
			}
			mark(i, end, components.CodeComment)
			i = end
		case strings.IndexByte(lang.quotes, c) >= 0:
			end := stringEnd(line, i)
			class := components.CodeString
			if rest := strings.TrimLeft(line[end:], " \t"); lang.keys && (strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "=")) {
				class = components.CodeKey
			}
			mark(i, end, class)
			i, command = end, false
		case lang.shell && c == '$':
			end := variableEnd(line, i)
			if end == i+1 && i+1 < len(line) && line[i+1] == '(' {
				command = true
			} else {
				mark(i, end, components.CodeVariable)
			}
			i = end
		case c >= '0' && c <= '9' && (i == 0 || !isCodeWord(line[i-1], lang.wordExtra)):
			end := i
			for end < len(line) && (isCodeWord(line[end], "") || line[end] == '.') {
				end++
			}
			mark(i, end, components.CodeNumber)
			i, command = end, false
		case lang.shell && c == '-' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') && i+1 < len(line) && (line[i+1] == '-' || isCodeLetter(line[i+1])):
			end := i
			for end < len(line) && line[end] != ' ' && line[end] != '\t' && line[end] != '=' {
				end++
			}
			mark(i, end, components.CodeFlag)
			i, command = end, false
		case isCodeLetter(c) || c == '_':
			end := i
			for end < len(line) && isCodeWord(line[end], lang.wordExtra) {
				end++
			}
			word := line[i:end]
			key := word
			if lang.foldCase {
				key = strings.ToLower(word)
			}
			rest := strings.TrimLeft(line[end:], " \t")
			switch {
			case lang.keys && (strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":") && (len(rest) == 1 || rest[1] == ' ')):
				mark(i, end, components.CodeKey)
			case lang.keywords[key]:
				mark(i, end, components.CodeKeyword)
				command = lang.shell
			case lang.calls && strings.HasPrefix(line[end:], "("):
				mark(i, end, components.CodeFunction)
			case command:
				mark(i, end, components.CodeFunction)
				command = false
			}
			i = end
		case c == '|' || c == ';' || c == '&' || c == '(':
			command = lang.shell
			i++
		default:
			_, size := utf8.DecodeRuneInString(line[i:])
			i += size
		}
	}
	return dst
}

// stringEnd is where the string that opens at line[start] ends: after its closing
// quote, or at the end of the line.
func stringEnd(line string, start int) int {
	quote := line[start]
	for i := start + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case quote:
			return i + 1
		}
	}
	return len(line)
}

// variableEnd is where the shell variable that starts at line[start] ends.
func variableEnd(line string, start int) int {
	i := start + 1
	if i >= len(line) {
		return i
	}
	switch c := line[i]; {
	case c == '{':
		if at := strings.IndexByte(line[i:], '}'); at >= 0 {
			return i + at + 1
		}
		return len(line)
	case c == '(':
		return i // a command substitution: the command inside is lexed on
	case c >= '0' && c <= '9' || c == '?' || c == '$' || c == '#' || c == '@' || c == '*' || c == '!':
		return i + 1
	}
	for i < len(line) && isCodeWord(line[i], "") {
		i++
	}
	return i
}

func isCodeLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isCodeWord(c byte, extra string) bool {
	return isCodeLetter(c) || c >= '0' && c <= '9' || c == '_' || strings.IndexByte(extra, c) >= 0
}
