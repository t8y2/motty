package workbench

import "testing"

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("snippet-page", func(t *testing.T, s *State) {
			snippetPageScene(s)
		}),
		plainScene("snippet-page-filtered", func(t *testing.T, s *State) {
			snippetPageScene(s)
			s.snippets.filter = snippetFilter{group: "Ops"}
			s.refreshSnippets()
		}),
		plainScene("snippet-page-expanded", func(t *testing.T, s *State) {
			snippetPageScene(s)
			s.snippets.expanded = 1
		}),
	)
}

// snippetPageScene shows the snippets catalog with grouped commands, one with
// a shortcut and one that runs itself.
func snippetPageScene(s *State) {
	s.snippets.list = []snippet{
		{Name: "Deploy staging", Group: "Ops", Command: "kubectl apply -f staging/\nwatch pods", Run: true},
		{Name: "Tail logs", Group: "Ops", Command: "journalctl -fu app"},
		{Name: "Open editor", Group: "dev", Command: "code ."},
		{Name: "Cleanup", Command: "rm -rf ./tmp"},
	}
	s.refreshSnippets()
	s.activeSection = sectionSnippets
}
