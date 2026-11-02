package sshconfig

import (
	"fmt"
	"strings"
)

func parseLine(line string) (string, []string, error) {
	fields, err := splitFields(line)
	if err != nil || len(fields) == 0 {
		return "", nil, err
	}
	name := fields[0]
	args := fields[1:]
	if before, after, found := strings.Cut(name, "="); found {
		name = before
		if after != "" {
			args = append([]string{after}, args...)
		}
	} else if len(args) > 0 {
		if args[0] == "=" {
			args = args[1:]
		} else if strings.HasPrefix(args[0], "=") {
			args[0] = strings.TrimPrefix(args[0], "=")
		}
	}
	if name == "" {
		return "", nil, fmt.Errorf("missing directive name")
	}
	return strings.ToLower(name), args, nil
}

func splitFields(line string) ([]string, error) {
	var fields []string
	var current strings.Builder
	var quote byte
	started := false
	flush := func() {
		if !started {
			return
		}
		fields = append(fields, current.String())
		current.Reset()
		started = false
	}
	for index := 0; index < len(line); index++ {
		character := line[index]
		if character == '\\' && quote != '\'' {
			// Any other backslash is kept, so a Windows path such as
			// C:\Users\me\.ssh\id_ed25519 reads back as written.
			if index+1 < len(line) && escapes(line[index+1], quote) {
				index++
				character = line[index]
			}
			current.WriteByte(character)
			started = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
				started = true
				continue
			}
			current.WriteByte(character)
			started = true
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
			started = true
		case '#':
			flush()
			return fields, nil
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteByte(character)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return fields, nil
}

// escapes reports whether a backslash before next stands for next itself. It
// is the set OpenSSH reads (argv_split): the backslash, both quotes and, outside
// quotes, a space; before anything else the backslash is an ordinary character.
func escapes(next, quote byte) bool {
	switch next {
	case '\\', '\'', '"':
		return true
	case ' ':
		return quote == 0
	}
	return false
}

func matchPatternList(arguments []string, alias string) bool {
	matched := false
	for _, argument := range arguments {
		for _, pattern := range strings.Split(argument, ",") {
			negated := strings.HasPrefix(pattern, "!")
			if negated {
				pattern = strings.TrimPrefix(pattern, "!")
			}
			if pattern == "" || !matchGlob(pattern, alias) {
				continue
			}
			if negated {
				return false
			}
			matched = true
		}
	}
	return matched
}

func matchGlob(pattern, value string) bool {
	patternRunes := []rune(strings.ToLower(pattern))
	valueRunes := []rune(strings.ToLower(value))
	patternIndex, valueIndex := 0, 0
	starIndex, starValueIndex := -1, -1
	for valueIndex < len(valueRunes) {
		if patternIndex < len(patternRunes) && (patternRunes[patternIndex] == '?' || patternRunes[patternIndex] == valueRunes[valueIndex]) {
			patternIndex++
			valueIndex++
			continue
		}
		if patternIndex < len(patternRunes) && patternRunes[patternIndex] == '*' {
			starIndex = patternIndex
			starValueIndex = valueIndex
			patternIndex++
			continue
		}
		if starIndex >= 0 {
			patternIndex = starIndex + 1
			starValueIndex++
			valueIndex = starValueIndex
			continue
		}
		return false
	}
	for patternIndex < len(patternRunes) && patternRunes[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(patternRunes)
}
