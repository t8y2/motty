package workbench

import "strings"

func (s *State) noteMatchesSearch(note savedNote, query string) bool {
	query = strings.ToLower(query)
	if query == "" {
		return true
	}
	terms := []string{query}
	if strings.Contains(query, "|") {
		terms = nil
		for _, term := range strings.Split(query, "|") {
			if term = strings.TrimSpace(term); term != "" {
				terms = append(terms, term)
			}
		}
		if len(terms) == 0 {
			return true
		}
	}
	note = s.noteBrowserNote(note)
	contains := func(value string) bool {
		value = strings.ToLower(value)
		for _, term := range terms {
			if strings.Contains(value, term) {
				return true
			}
		}
		return false
	}
	if contains(note.Title + "\n" + note.Body) {
		return true
	}
	for _, tag := range note.Tags {
		if host, ok := parseNoteHost(tag); ok {
			if contains(host.Label) || contains(host.Address) || contains(host.User) {
				return true
			}
		} else if path, ok := parseNoteHostGroup(tag); ok {
			if contains(path) {
				return true
			}
		} else if contains(tag) {
			return true
		}
	}
	return false
}
