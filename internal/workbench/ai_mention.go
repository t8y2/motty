package workbench

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/vivaui/viva/text"
	"motty/internal/ai/agent"
)

// An @name in a message to the assistant brings that session's recent terminal
// text along with the question, so the assistant need not be told to go and read
// it and the user decides which hosts it sees. The name is the tab's title with
// spaces made into dashes; two tabs of the same title are told apart by a number.

const (
	maxMentionSuggestions = 5
	// maxMentions bounds how many sessions one message can pull in.
	maxMentions = 6
	// defaultMentionLines is how much of a mentioned session is sent when the
	// user has turned off the automatic context, which is about the active tab.
	defaultMentionLines = 60
)

type mentionName struct {
	name    string
	session uint32
	detail  string // the tab's title and address, to tell sessions apart in the list
}

// mentionSlug is a title written as one word.
func mentionSlug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(title) {
		if mentionRune(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func mentionRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' || r == ':'
}

// mentionNames is the name of every session a message can mention.
func (state *State) mentionNames(dst []mentionName) []mentionName {
	seen := map[string]int{}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.term == nil {
			continue
		}
		name := mentionSlug(session.Title)
		if name == "" {
			name = "session-" + sessionHandle(session.ID)
		}
		key := strings.ToLower(name)
		seen[key]++
		if seen[key] > 1 {
			name += "-" + strconv.Itoa(seen[key])
		}
		dst = append(dst, mentionName{name: name, session: session.ID, detail: session.Title + " · " + session.Address})
	}
	return dst
}

// findMentions lists the sessions a message names, in order of first mention.
func findMentions(message string, names []mentionName) []uint32 {
	var found []uint32
	runes := []rune(message)
	for at := 0; at < len(runes); at++ {
		if runes[at] != '@' || at > 0 && mentionRune(runes[at-1]) {
			continue // an e-mail address is not a mention
		}
		end := at + 1
		for end < len(runes) && mentionRune(runes[end]) {
			end++
		}
		word := strings.TrimRight(string(runes[at+1:end]), ".:-")
		for _, candidate := range names {
			if !strings.EqualFold(candidate.name, word) {
				continue
			}
			already := false
			for _, id := range found {
				already = already || id == candidate.session
			}
			if !already && len(found) < maxMentions {
				found = append(found, candidate.session)
			}
			break
		}
		at = end - 1
	}
	return found
}

// aiMentionContext is the terminal text of the sessions a message names, except
// the one aiContext already sent.
func (state *State) aiMentionContext(message string, skip uint32) string {
	var scratch [8]mentionName
	ids := findMentions(message, state.mentionNames(scratch[:0]))
	if len(ids) == 0 {
		return ""
	}
	lines := state.ai.settings.contextLines
	if lines <= 0 {
		lines = defaultMentionLines
	}
	var b strings.Builder
	for _, id := range ids {
		session, ok := state.sessionByIDPointer(id)
		if !ok || session.term == nil || id == skip {
			continue
		}
		text := lastLines(session.term.screen.Text(), lines)
		if text == "" {
			text = "(empty)"
		}
		if state.ai.settings.redact {
			text = agent.Redact(text)
		}
		fmt.Fprintf(&b, "\n\n[Terminal of session %s: %s (%s), last lines:]\n%s", sessionHandle(session.ID), session.Title, session.Address, text)
	}
	return b.String()
}

// mentionQuery is the @word the caret is at the end of, with the range it takes
// in the message box. It is only a candidate: whether it names a session is for
// the list to say.
func (state *State) mentionQuery() (partial string, span text.Range, ok bool) {
	input := &state.ai.input
	runes := []rune(input.String())
	caret := input.Selection().End
	if caret > len(runes) || input.Selection().Start != caret {
		return "", text.Range{}, false
	}
	start := caret
	for start > 0 && mentionRune(runes[start-1]) {
		start--
	}
	if start == 0 || runes[start-1] != '@' || start >= 2 && mentionRune(runes[start-2]) {
		return "", text.Range{}, false
	}
	return string(runes[start:caret]), text.Range{Start: start - 1, End: caret}, true
}

// mentionSuggestions fills dst with the sessions whose names start with what is
// typed after an @, or contain it when none start with it.
func (state *State) mentionSuggestions(dst []mentionName) ([]mentionName, text.Range) {
	partial, span, ok := state.mentionQuery()
	if !ok {
		return dst[:0], text.Range{}
	}
	var all [16]mentionName
	names := state.mentionNames(all[:0])
	partial = strings.ToLower(partial)
	dst = dst[:0]
	for _, name := range names {
		if strings.HasPrefix(strings.ToLower(name.name), partial) && len(dst) < cap(dst) {
			dst = append(dst, name)
		}
	}
	if len(dst) == 0 && partial != "" {
		for _, name := range names {
			if strings.Contains(strings.ToLower(name.name), partial) && len(dst) < cap(dst) {
				dst = append(dst, name)
			}
		}
	}
	return dst, span
}

// acceptMention puts a suggested name in place of what was typed after the @.
func (state *State) acceptMention(name string) {
	_, span, ok := state.mentionQuery()
	if !ok {
		return
	}
	input := &state.ai.input
	input.SetSelection(span)
	input.Insert("@" + name + " ")
}
