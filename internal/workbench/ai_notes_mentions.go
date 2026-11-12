package workbench

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const notesReferenceMarker = "\n\n[Note references:]\n"

// AI references include only tags actually present on notes. Keep this
// separate from ordinary-tag editor candidates, which exclude associations.
func (s *State) noteTagReferences() []aiNoteReference {
	refs := []aiNoteReference{}
	seen := map[string]bool{}
	for _, n := range s.notes.list {
		for _, tag := range s.noteBrowserNote(n).Tags {
			r := aiNoteReference{Kind: "tag", ID: tag, Title: tag}
			if h, ok := parseNoteHost(tag); ok {
				r.Kind, r.ID, r.Title = "host", h.CatalogID, noteHostLabel(h)
			} else if group, ok := parseNoteHostGroup(tag); ok {
				r.Kind, r.ID, r.Title = "host_group", group, group
			}
			key := r.Kind + "\x00" + r.ID
			if !seen[key] {
				seen[key] = true
				refs = append(refs, r)
			}
		}
	}
	slices.SortFunc(refs, func(a, b aiNoteReference) int {
		if c := strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
			return c
		}
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return refs
}
func noteTagSlug(title string) string {
	parts := strings.Split(title, "/")
	for i := range parts {
		parts[i] = mentionSlug(parts[i])
	}
	return strings.Join(parts, "/")
}
func noteReferencePrefix(kind string) string {
	if kind == "note" {
		return "note:"
	}
	return "note:" + kind + ":"
}
func noteReferenceAllowsSlash(word string) bool {
	word = strings.ToLower(word)
	for _, kind := range []string{"tag", "folder", "host", "host_group"} {
		if strings.HasPrefix(word, noteReferencePrefix(kind)) {
			return true
		}
	}
	return false
}
func (s *State) noteFolderReferencePath(id string) (string, bool) {
	parts := []string{}
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return "", false
		}
		seen[id] = true
		folder, ok := s.noteFolder(id)
		if !ok {
			return "", false
		}
		parts = append(parts, folder.Name)
		id = folder.Parent
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/"), len(parts) > 0
}

func (s *State) noteMentionNames() []mentionName {
	refs := []aiNoteReference{}
	for _, n := range s.notes.list {
		refs = append(refs, aiNoteReference{Kind: "note", ID: n.ID, Title: s.noteBrowserNote(n).Title})
	}
	refs = append(refs, s.noteTagReferences()...)
	for _, folder := range s.notes.folders {
		if path, ok := s.noteFolderReferencePath(folder.ID); ok {
			refs = append(refs, aiNoteReference{Kind: "folder", ID: folder.ID, Title: path})
		}
	}
	counts := map[string]int{}
	baseToken := func(r aiNoteReference) string {
		slug := mentionSlug(r.Title)
		if r.Kind == "note" {
			// Reserve typed namespaces without changing the note's display title.
			for _, kind := range []string{"tag", "folder", "host", "host_group"} {
				if strings.HasPrefix(strings.ToLower(slug), kind+":") {
					slug = strings.Replace(slug, ":", "-", 1)
					break
				}
			}
		} else {
			slug = noteTagSlug(r.Title)
		}
		if slug == "" {
			slug = "untitled"
		}
		return noteReferencePrefix(r.Kind) + slug
	}

	for _, r := range refs {
		counts[strings.ToLower(baseToken(r))]++
	}
	used := map[string]bool{}
	for _, terminal := range s.mentionNames(nil) {
		used[strings.ToLower(terminal.name)] = true
	}
	names := []mentionName{}
	for _, r := range refs {
		base := baseToken(r)
		token := base
		if r.Kind != "note" && counts[strings.ToLower(base)] > 1 {
			token = base + "-" + r.Kind + "-" + noteHash(r.ID)[:8]
		}
		stem := token
		for i := 2; used[strings.ToLower(token)]; i++ {
			token = fmt.Sprintf("%s-%d", stem, i)
		}
		for _, old := range s.ai.noteMentions {
			if strings.EqualFold(old.Token, token) && (old.Kind != r.Kind || old.ID != r.ID) {
				token = stem + ":" + noteHash(r.Kind + "\x00" + r.ID)[:12]
			}
		}
		stem = token
		for i := 2; used[strings.ToLower(token)]; i++ {
			token = fmt.Sprintf("%s-%d", stem, i)
		}
		used[strings.ToLower(token)] = true
		r.Token = token
		detail := s.noteText("Note", "笔记")
		switch r.Kind {
		case "folder":
			detail = s.noteText("Folder", "文件夹")
		case "tag":
			detail = s.noteText("Tag", "普通标签")
		case "host":
			detail = s.noteText("Host tag", "主机标签")
		case "host_group":
			detail = s.noteText("Group tag", "分组标签")
		}
		names = append(names, mentionName{name: token, title: r.Title, detail: detail, noteReference: &r})
	}
	return names
}

// Slashes are allowed in typed note references, never terminal names.
func noteReferenceWords(message string) []string {
	words := []string{}
	runes := []rune(message)
	for at := 0; at < len(runes); at++ {
		if runes[at] != '@' || at > 0 && mentionEmailPrefix(runes[at-1]) {
			continue
		}
		end := at + 1
		allowsSlash := noteReferenceAllowsSlash(string(runes[end:]))
		for end < len(runes) && (mentionRune(runes[end]) || allowsSlash && runes[end] == '/') {
			end++
		}
		words = append(words, strings.TrimRight(string(runes[at+1:end]), ".:-/"))
		at = end - 1
	}
	return words
}
func (s *State) aiNoteReferences(message string) []aiNoteReference {
	refs := []aiNoteReference{}
	words := noteReferenceWords(message)
	if len(words) == 0 {
		return refs
	}
	candidates := s.noteMentionNames()
	terminals := s.mentionNames(nil)
	add := func(r aiNoteReference) {
		if !slices.ContainsFunc(words, func(word string) bool { return strings.EqualFold(word, r.Token) }) {
			return
		}
		for _, old := range refs {
			if strings.EqualFold(old.Token, r.Token) {
				return
			}
		}
		refs = append(refs, r)
	}
	// Selected references win over current title resolution, including after deletion.
	for i := len(s.ai.noteMentions) - 1; i >= 0; i-- {
		add(s.ai.noteMentions[i])
	}
	for _, candidate := range candidates {
		add(*candidate.noteReference)
	}
	// Unresolved names remain textual focus hints. Ambiguous current labels
	// require selecting a typed candidate instead of guessing an identity.
	for _, token := range words {
		kind := ""
		lower := strings.ToLower(token)
		if strings.HasPrefix(lower, "note:") {
			kind = "note"
		}
		for _, typed := range []string{"tag", "folder", "host", "host_group"} {
			if strings.HasPrefix(lower, noteReferencePrefix(typed)) {
				kind = typed
				break
			}
		}

		for _, terminal := range terminals {
			if strings.EqualFold(terminal.name, token) {
				kind = ""
				break
			}
		}
		if kind != "" {
			ambiguous := false
			if kind != "note" {
				matches := 0
				for _, candidate := range candidates {
					r := candidate.noteReference
					if r.Kind == kind && strings.EqualFold(noteReferencePrefix(kind)+noteTagSlug(r.Title), token) {
						matches++
					}
				}
				ambiguous = matches > 1
			}

			add(aiNoteReference{Kind: kind, Token: token, Title: token, Ambiguous: ambiguous})
		}
	}

	return refs
}

func aiNoteReferenceContext(refs []aiNoteReference) string {
	if len(refs) == 0 {
		return ""
	}
	// Context wire format is independent of history metadata: tag and
	// host_group keep their identities internally but do not send an id field.
	type wireReference struct {
		Kind  string  `json:"kind"`
		ID    *string `json:"id,omitempty"`
		Title string  `json:"title"`
		Token string  `json:"token"`
	}
	wire := make([]wireReference, 0, len(refs))
	for _, r := range refs {
		item := wireReference{Kind: r.Kind, Title: r.Title, Token: r.Token}
		if r.Kind == "note" || r.Kind == "folder" || r.Kind == "host" {
			id := r.ID
			item.ID = &id
		}
		wire = append(wire, item)
	}
	raw, _ := json.Marshal(wire)
	return notesReferenceMarker + string(raw)
}

func (s *State) aiRememberNoteMention(name string) {
	for _, candidate := range s.noteMentionNames() {
		if candidate.name != name {
			continue
		}
		r := *candidate.noteReference
		s.aiRememberNoteReference(r)
		return
	}
}

func (s *State) aiRememberNoteReference(r aiNoteReference) {
	s.ai.noteMentions = slices.DeleteFunc(s.ai.noteMentions, func(old aiNoteReference) bool { return strings.EqualFold(old.Token, r.Token) })
	s.ai.noteMentions = append(s.ai.noteMentions, r)
}

// Search input is an alias, not a reference token. Only accepting a candidate
// inserts the canonical token and binds its identity.
func (s *State) noteMentionSuggestions(dst []mentionName, partial string) []mentionName {
	input := strings.ToLower(partial)
	query := strings.TrimPrefix(strings.TrimPrefix(input, "note"), ":")
	kind := ""
	for _, candidate := range []string{"tag", "folder", "host", "host_group"} {
		if query == candidate {
			kind = candidate
			query = ""
			break
		}
		if strings.HasPrefix(query, candidate+":") {
			kind = candidate
			query = strings.TrimPrefix(query, candidate+":")
			break
		}
	}
	type match struct {
		name mentionName
		rank int
	}
	matches := []match{}
	for _, name := range s.noteMentionNames() {
		if kind != "" && name.noteReference.Kind != kind {
			continue
		}
		rank := 4
		for _, value := range []string{strings.ToLower(name.title), strings.ToLower(noteTagSlug(name.title))} {
			switch {
			case value == query:
				rank = min(rank, 0)
			case strings.HasPrefix(value, query):
				rank = min(rank, 1)
			case strings.Contains(value, query):
				rank = min(rank, 2)
			}
		}
		// Keep namespace completion and exact, disambiguated canonical tokens usable.
		if strings.HasPrefix(strings.ToLower(name.name), input) {
			rank = min(rank, 3)
		}
		if rank < 4 {
			matches = append(matches, match{name, rank})
		}
	}
	// A terminal whose name starts with "note" must remain selectable. Exact
	// terminal names precede note matches; other session matches follow them.
	for _, name := range s.mentionNames(nil) {
		value := strings.ToLower(name.name)
		rank := 4
		switch {
		case strings.EqualFold(name.name, partial):
			rank = -1
		case strings.HasPrefix(value, input):
			rank = 3
		case strings.Contains(value, input):
		default:
			continue
		}
		matches = append(matches, match{name, rank})
	}
	slices.SortStableFunc(matches, func(a, b match) int { return a.rank - b.rank })
	dst = dst[:0]
	for _, m := range matches {
		if len(dst) == cap(dst) {
			break
		}
		dst = append(dst, m.name)
	}
	return dst
}
