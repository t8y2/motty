package workbench

func (s *State) noteMatchesSearch(note savedNote, query string) bool {
	return noteMatchesSearch(note, query, s.noteBrowserNote)
}
