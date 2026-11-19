package workbench

import "testing"

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("g4-snippet-dialog", func(t *testing.T, s *State) { s.openSnippetDialog(-1) }),
		plainScene("g4-snippet-dialog-error", func(t *testing.T, s *State) {
			s.openSnippetDialog(-1)
			s.snippets.name.Insert("deploy")
			s.snippets.dialogError = "Name the snippet."
		}),
	)
}
