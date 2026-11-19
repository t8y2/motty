package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// The scenes of the small surfaces closed last: a remote desktop's bars and the
// page its connection ends on, the command drawer's menus, and the save box the
// password prompt offers. They are here so the migration of those surfaces can
// be diffed pixel for pixel against how the canvas painted them.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		desktopScene("misc-desktop", func(*State, *Session) {}),
		desktopScene("misc-desktop-ended", func(state *State, session *Session) {
			session.desk.ended = true
			session.term.ended = true
		}),
		desktopFullscreenScene("misc-desktop-fullscreen"),
		commandsScene("misc-commands-filter", func(state *State) { state.commands.filterOpen = true }),
		commandsScene("misc-commands-sort", func(state *State) { state.commands.sortOpen = true }),
		passwordSaveScene("misc-password-save", false),
		passwordSaveScene("misc-password-save-checked", true),
	)
}

// desktopScene is a window with one connected remote desktop in a pane.
func desktopScene(name string, setup func(*State, *Session)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state := newTestState(t, testOptions(t))
		openHost(state)
		session := &state.sessions[0]
		session.term.live = true
		session.desk = newDesktopView(newFollowingDesktop(800, 600, false), false)
		setup(state, session)
		return state, func(c *gfx.Canvas, size gfx.Size) {
			state.Resize(size)
			state.Paint(c, size)
		}
	}}
}

// desktopFullscreenScene is a window whose remote desktop fills it.
func desktopFullscreenScene(name string) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state := newTestState(t, testOptions(t))
		openHost(state)
		session := &state.sessions[0]
		session.term.live = true
		session.desk = newDesktopView(newFollowingDesktop(800, 600, false), false)
		session.desk.fullscreen = true
		state.winChrome.fullscreen = true
		state.winChrome.fullscreenSession = session.ID
		return state, func(c *gfx.Canvas, size gfx.Size) {
			state.Resize(size)
			state.Paint(c, size)
		}
	}}
}

// commandsScene is a window on the terminal page with the command drawer shown.
func commandsScene(name string, setup func(*State)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state := newTestState(t, freshOptions(t))
		state.deck = layoutcore.Deck{}
		if _, _, err := state.deck.Create("test"); err != nil {
			t.Fatal(err)
		}
		state.activeSection = sectionTerminal
		state.modal = modalNone
		state.snippets.list = []snippet{
			{Name: "ls", Command: "ls", Run: true},
			{Name: "pwd", Command: "pwd", Group: "shell"},
		}
		state.commands.shown = true
		setup(state)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

// passwordSaveScene is the password prompt of a connection that offers to keep
// the secret.
func passwordSaveScene(name string, checked bool) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		world, options := newWorld(t)
		state := newTestState(t, options)
		fake := connect(t, state, world)
		fake.set(func(status *sshconn.Status) {
			status.Phase, status.Attempt = sshconn.PhasePasswordPrompt, 1
			status.Questions = []sshconn.Question{{Text: "Password: ", Secret: true}}
			status.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
		})
		state.Wake()
		state.secrets.canSave = true
		state.secrets.save = checked
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}
