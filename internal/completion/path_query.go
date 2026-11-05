package completion

import (
	"path"
	"strings"
	"unicode/utf8"
)

type PathQuery struct {
	Line            string
	CWD             string
	Root            string
	Word            string
	Directory       string
	Prefix          string
	DirectoriesOnly bool
}

var pathCommands = map[string]bool{
	"cd": true, "pushd": true, "mkdir": true, "rmdir": true,
	"ls": true, "dir": true, "cat": true, "tail": true, "head": true,
	"less": true, "more": true, "cp": true, "mv": true, "rm": true,
	"touch": true, "ln": true, "grep": true, "find": true,
	"du": true, "df": true, "tar": true, "gzip": true,
	"wc": true, "sort": true, "uniq": true, "cut": true,
	"tee": true, "vim": true, "nano": true,
}

func ParsePathQuery(line, cwd string) (PathQuery, bool) {
	if len(line) == 0 || len(line) > 4096 || len(cwd) == 0 || len(cwd) > 2048 || !path.IsAbs(cwd) || !utf8.ValidString(cwd) ||
		strings.IndexFunc(cwd, func(value rune) bool { return value < 32 || value == 127 }) >= 0 ||
		strings.HasPrefix(line, " ") || strings.Contains(line, "  ") ||
		strings.ContainsAny(line, "\t\r\n") {
		return PathQuery{}, false
	}
	tokens := strings.Split(line, " ")
	if len(tokens) < 2 || !pathCommands[tokens[0]] {
		return PathQuery{}, false
	}
	for _, token := range tokens {
		if token != "" && !SafeValue(token) {
			return PathQuery{}, false
		}
	}
	word := tokens[len(tokens)-1]
	if len(word) == 0 || len(word) > 256 || strings.HasPrefix(word, "-") {
		return PathQuery{}, false
	}
	numeric := true
	for _, value := range word {
		numeric = numeric && value >= '0' && value <= '9'
	}
	if numeric {
		return PathQuery{}, false
	}
	directory, prefix := path.Dir(word), path.Base(word)
	if strings.HasSuffix(word, "/") {
		directory, prefix = word, ""
	}
	if !path.IsAbs(directory) {
		directory = path.Join(cwd, directory)
	}
	return PathQuery{
		Line: line, CWD: cwd, Root: tokens[0], Word: word, Directory: path.Clean(directory), Prefix: prefix,
		DirectoriesOnly: tokens[0] == "cd" || tokens[0] == "pushd" || tokens[0] == "mkdir" || tokens[0] == "rmdir",
	}, true
}
