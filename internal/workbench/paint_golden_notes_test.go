package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The notes surface's scenes: the library with its tree, header and source
// editor, the preview and split modes, the tags and host popovers, the tree
// menu, the delete question and the terminal side panel. They are painted
// through the workbench page, as the window paints it.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		notesPaintScene("notes-library", func(t *testing.T, s *State) {
			notesPaintLibrary(t, s, noteSource)
		}),
		notesPaintScene("notes-split", func(t *testing.T, s *State) {
			notesPaintLibrary(t, s, noteSplit)
		}),
		notesPaintScene("notes-preview", func(t *testing.T, s *State) {
			notesPaintLibrary(t, s, notePreview)
		}),
		notesPaintScene("notes-tags-popup", func(t *testing.T, s *State) {
			notesPaintLibrary(t, s, noteSource)
			s.notes.view.popup = "tags"
			s.notes.view.overlayFrom = hitNoteTagsOpen
		}),
		notesPaintScene("notes-hosts-popup", func(t *testing.T, s *State) {
			notesPaintLibrary(t, s, noteSource)
			s.notes.view.popup = "hosts"
			s.notes.view.overlayFrom = hitNoteHostsOpen
		}),
		notesPaintScene("notes-menu", func(t *testing.T, s *State) {
			notesPaintLibrary(t, s, noteSource)
			// A narrow library overflows its tools, which is where the actions
			// menu is opened from.
			s.notes.view.width = 220
			s.notes.view.menu = "tree"
			s.notes.view.overlayFrom = hitNoteTreeMore
		}),
		notesPaintScene("notes-confirm", func(t *testing.T, s *State) {
			notesPaintLibrary(t, s, noteSource)
			s.notes.confirm = "note"
		}),
		plainScene("notes-exit", func(t *testing.T, s *State) {
			s.noteExit = &noteExitRequest{allow: func() {}}
		}),
		paintScene{name: "notes-sidebar", sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			world, options := newSharingWorld(t)
			s := newTestState(t, options)
			world.connectFirst(t, s)
			s.restoreNotes([]savedNote{
				{ID: "runbook", Title: "Release runbook", Body: "# Release runbook\n\nRoll back the **gateway** first.\n\n```sh\nmotty --version\n```\n", Order: 1, Created: "2026-01-02T03:04:05Z", Updated: "2026-01-02T03:04:05Z"},
				{ID: "ideas", Title: "Ideas", Body: "A shorter note.", Order: 2, Created: "2026-01-03T03:04:05Z", Updated: "2026-01-03T03:04:05Z"},
			}, nil)
			s.Click(hitNotesPanel)
			n := &s.noteSidebarState().content
			n.sideAll = true
			n.selected = "runbook"
			s.syncNoteSidebarContent(n)
			return s, func(c *gfx.Canvas, size gfx.Size) { s.Paint(c, size) }
		}},
		noteWindowPaintScene("notes-window", noteSource),
		noteWindowPaintScene("notes-window-preview", notePreview),
	)
}

func notesPaintScene(name string, setup func(*testing.T, *State)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state := newTestState(t, testOptions(t))
		setup(t, state)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

// notesPaintLibrary fills the library with a folder and two notes and selects
// the runbook in the given mode.
func notesPaintLibrary(t *testing.T, s *State, mode noteMode) {
	t.Helper()
	s.Click(hitNotes)
	var tags []string
	if len(s.hosts) > 0 {
		tags = append(tags, encodeNoteHost(noteHost{CatalogID: s.hosts[0].CatalogID, Label: s.hosts[0].Label, Address: s.hosts[0].Address, User: s.hosts[0].User}))
	}
	tags = append(tags, "operations", "runbook")
	body := strings.Join([]string{
		"# Release runbook",
		"",
		"Roll back the **gateway** first, then the workers.",
		"",
		"```sh",
		"motty --version",
		"```",
		"",
		"- [x] drain traffic",
		"- [ ] restore the database",
		"",
		"See [the dashboard](https://example.com/status) for the current state.",
		"",
	}, "\n")
	s.restoreNotes([]savedNote{
		{ID: "runbook", Title: "Release runbook", Body: body, Tags: tags, Order: 1, Folder: "ops", Created: "2026-01-02T03:04:05Z", Updated: "2026-01-02T03:04:05Z"},
		{ID: "ideas", Title: "Ideas", Body: "A shorter note.", Order: 2, Created: "2026-01-03T03:04:05Z", Updated: "2026-01-03T03:04:05Z"},
	}, []savedNoteFolder{{ID: "ops", Name: "Operations"}})
	s.selectNote("runbook")
	s.notes.view.expanded["ops"] = true
	s.notes.view.mode = mode
}

// noteWindowPaintScene paints a note in its own document window, as the window
// page and paint do.
func noteWindowPaintScene(name string, mode noteMode) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		s := newTestState(t, testOptions(t))
		s.restoreNotes([]savedNote{
			{ID: "runbook", Title: "Release runbook", Body: "# Release runbook\n\nRoll back the **gateway** first.\n\n```sh\nmotty --version\n```\n", Created: "2026-01-02T03:04:05Z", Updated: "2026-01-02T03:04:05Z"},
		}, nil)
		d := noteEditingWindow(t, s, "runbook")
		d.note.content.view.mode = mode
		return s, func(c *gfx.Canvas, size gfx.Size) { s.fileWindows.paint(c, size) }
	}}
}
