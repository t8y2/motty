package workbench

import (
	"regexp"
	"strings"
)

// snippetVariableDef defines a dynamic variable placeholder inside a snippet command.
type snippetVariableDef struct {
	Name         string
	DefaultValue string
	HasDefault   bool
}

var variablePattern = regexp.MustCompile(`\{\{([^}:]+)(?::([^}]*))?\}\}`)

var goTemplateControlActions = map[string]bool{
	"break":    true,
	"continue": true,
	"else":     true,
	"end":      true,
	"if":       true,
	"range":    true,
	"with":     true,
}

var (
	goTemplateFieldRe1   = regexp.MustCompile(`(?:^|[\s(])\.[A-Za-z_]`)
	goTemplateFieldRe2   = regexp.MustCompile(`(?:^|[\s(])\.(?:$|[\s),|])`)
	goTemplateFieldRe3   = regexp.MustCompile(`(?:^|[\s(])\$[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_]`)
	goTemplateAssignedRe = regexp.MustCompile(`(\$[A-Za-z_][A-Za-z0-9_]*)(?:\s*(?:,|:=|=))`)
)

func normalizeGoTemplateAction(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "-")
	name = strings.TrimSuffix(name, "-")
	return strings.TrimSpace(name)
}

func hasGoTemplateFieldReference(name string) bool {
	action := normalizeGoTemplateAction(name)
	return action == "." ||
		goTemplateFieldRe1.MatchString(action) ||
		goTemplateFieldRe2.MatchString(action) ||
		goTemplateFieldRe3.MatchString(action)
}

func hasGoTemplateVariableReference(action string, variables map[string]bool) bool {
	for variable := range variables {
		pattern := `(?:^|[^A-Za-z0-9_])` + regexp.QuoteMeta(variable) + `(?:$|[^A-Za-z0-9_])`
		if matched, _ := regexp.MatchString(pattern, action); matched {
			return true
		}
	}
	return false
}

func isGoTemplateControlAction(name string) bool {
	action := normalizeGoTemplateAction(name)
	if action == "break" || action == "continue" || action == "else" || action == "end" {
		return true
	}
	fields := strings.Fields(action)
	if len(fields) > 0 && goTemplateControlActions[fields[0]] && strings.HasPrefix(action, fields[0]+" ") {
		return true
	}
	return false
}

type variableMatchInfo struct {
	token      string
	name       string
	defaultVal string
	hasDefault bool
	start      int
	end        int
}

func collectVariableMatches(text string) ([]variableMatchInfo, map[int]bool) {
	locs := variablePattern.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return nil, nil
	}

	matches := make([]variableMatchInfo, 0, len(locs))
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		token := text[start:end]
		nameStart, nameEnd := loc[2], loc[3]
		var name string
		if nameStart >= 0 && nameEnd >= 0 {
			name = text[nameStart:nameEnd]
		}
		var defVal string
		hasDef := false
		if loc[4] >= 0 && loc[5] >= 0 {
			defVal = text[loc[4]:loc[5]]
			hasDef = true
		}
		matches = append(matches, variableMatchInfo{
			token:      token,
			name:       name,
			defaultVal: defVal,
			hasDefault: hasDef,
			start:      start,
			end:        end,
		})
	}

	goTemplateOffsets := make(map[int]bool)
	goTemplateVariables := make(map[string]bool)
	blockDepth := 0

	for _, match := range matches {
		body := match.token
		if strings.HasPrefix(body, "{{") && strings.HasSuffix(body, "}}") {
			body = body[2 : len(body)-2]
		}
		action := normalizeGoTemplateAction(body)
		fields := strings.Fields(action)
		firstWord := ""
		if len(fields) > 0 {
			firstWord = fields[0]
		}

		hasFieldRef := hasGoTemplateFieldReference(action)
		hasKnownVarRef := hasGoTemplateVariableReference(action, goTemplateVariables)
		insideBlock := blockDepth > 0
		isBlockStart := (firstWord == "if" || firstWord == "range" || firstWord == "with") &&
			(hasFieldRef || hasKnownVarRef)
		isBlockEnd := action == "end"
		isGoTemplateToken := hasFieldRef ||
			hasKnownVarRef ||
			isBlockStart ||
			(insideBlock && isGoTemplateControlAction(action))

		if isGoTemplateToken {
			goTemplateOffsets[match.start] = true
			for _, sub := range goTemplateAssignedRe.FindAllStringSubmatch(action, -1) {
				if len(sub) > 1 {
					goTemplateVariables[sub[1]] = true
				}
			}
		}

		if isBlockStart {
			blockDepth++
		} else if isBlockEnd && insideBlock {
			blockDepth--
		}
	}

	return matches, goTemplateOffsets
}

func isSnippetVariableName(name string) bool {
	return strings.TrimSpace(name) != ""
}

// snippetHasVariables reports whether the command has any dynamic variable placeholders.
func snippetHasVariables(command string) bool {
	return len(parseSnippetVariables(command)) > 0
}

// parseSnippetVariables returns the unique dynamic variables in the command, preserving first-seen order.
func parseSnippetVariables(command string) []snippetVariableDef {
	matches, goTemplateOffsets := collectVariableMatches(command)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]int)
	var defs []snippetVariableDef

	for _, match := range matches {
		name := strings.TrimSpace(match.name)
		if goTemplateOffsets[match.start] || !isSnippetVariableName(name) {
			continue
		}
		if idx, exists := seen[name]; exists {
			// If previously seen without default, but this one has default, update it.
			if !defs[idx].HasDefault && match.hasDefault {
				defs[idx].DefaultValue = match.defaultVal
				defs[idx].HasDefault = true
			}
			continue
		}

		seen[name] = len(defs)
		def := snippetVariableDef{
			Name:         name,
			DefaultValue: match.defaultVal,
			HasDefault:   match.hasDefault,
		}
		defs = append(defs, def)
	}

	return defs
}

func replaceSnippetVariableTokens(command string, replacementFor func(name string, token string) string) string {
	matches, goTemplateOffsets := collectVariableMatches(command)
	if len(matches) == 0 {
		return command
	}

	var sb strings.Builder
	lastIdx := 0
	for _, match := range matches {
		sb.WriteString(command[lastIdx:match.start])
		name := strings.TrimSpace(match.name)
		if goTemplateOffsets[match.start] || !isSnippetVariableName(name) {
			sb.WriteString(match.token)
		} else {
			sb.WriteString(replacementFor(name, match.token))
		}
		lastIdx = match.end
	}
	sb.WriteString(command[lastIdx:])
	return sb.String()
}

func resolveVariableValue(def snippetVariableDef, values map[string]string) (string, bool) {
	if val, ok := values[def.Name]; ok && strings.TrimSpace(val) != "" {
		return val, true
	}
	if def.HasDefault {
		return def.DefaultValue, true
	}
	return "", false
}

// applySnippetVariables replaces variables with values, returning the result and whether all required variables were resolved.
func applySnippetVariables(command string, values map[string]string) (string, bool) {
	defs := parseSnippetVariables(command)
	if len(defs) == 0 {
		return command, true
	}

	resolved := make(map[string]string)
	for _, def := range defs {
		val, ok := resolveVariableValue(def, values)
		if !ok {
			return "", false
		}
		resolved[def.Name] = val
	}

	res := replaceSnippetVariableTokens(command, func(name string, token string) string {
		if val, ok := resolved[name]; ok {
			return val
		}
		return token
	})
	return res, true
}

// previewSnippetCommand generates a live preview of the command with current variable values.
func previewSnippetCommand(command string, values map[string]string) string {
	defs := parseSnippetVariables(command)
	if len(defs) == 0 {
		return command
	}

	defMap := make(map[string]snippetVariableDef)
	for _, def := range defs {
		defMap[def.Name] = def
	}

	return replaceSnippetVariableTokens(command, func(name string, token string) string {
		def, ok := defMap[name]
		if !ok {
			return token
		}
		if val, found := resolveVariableValue(def, values); found {
			return val
		}
		return token
	})
}
