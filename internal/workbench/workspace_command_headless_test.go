//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func openWorkspaceCommandWindow(t *testing.T, state *State, width int) *headless.Session {
	t.Helper()
	var input app.TextInput
	view, err := headless.Open(app.Config{Title: "Workspace command", Width: width, Height: 800, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize, TextInput: func(context app.TextInputContext) *app.TextInput {
		input.Editor = state.EditorFor(context.ID)
		if input.Editor == nil {
			return nil
		}
		return &input
	}}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

func clickWorkspaceCommandHit(t *testing.T, view *headless.Session, id gfx.ID) {
	t.Helper()
	hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
	if !ok || hit.Block || hit.Disabled {
		t.Fatalf("unavailable control %v: %+v", id, hit)
	}
	if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	// A page control that opens more content lays it out in the repaint the
	// click schedules; let that frame land before reading the next target.
	if err := view.Wait(5 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceCommandThroughWindowDriver(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.hosts[0].LoginCommand = "sudo -i"
	state.openWorkspaceDraft(workspaceDraftTemporary)
	view := openWorkspaceCommandWindow(t, state, 1280)
	click := func(id gfx.ID) { t.Helper(); clickWorkspaceCommandHit(t, view, id) }
	click(workspaceDraftHostID(0))
	click(hitWorkspaceCommandEdit.At(int(state.hosts[0].ID)))
	hints := 0
	hint := state.messages.WorkspaceCommandOrderHint
	for _, label := range view.Snapshot().Texts {
		visibleHint, elided := strings.CutSuffix(label.Value, "…")
		if label.Value == hint || elided && visibleHint != "" && strings.HasPrefix(hint, visibleHint) {
			hints++
		}
	}
	if hints != 1 {
		t.Fatalf("command order hint painted %d times, want 1", hints)
	}
	click(hitWorkspaceCommandField.At(int(state.hosts[0].ID)))
	if err := view.Type("cd /srv"); err != nil {
		t.Fatal(err)
	}
	workspaceEditorClickLabel(t, view, state.messages.WorkspaceDraftLaunch)
	if len(world.conns) != 1 || len(state.sessions) != 1 {
		t.Fatal("workspace did not launch")
	}
	finishWorkspaceConnection(t, state, world.conns[0])
	session := &state.sessions[0]
	shell := world.conns[0].shell(t)
	workspaceCommandPrompt(state, session, shell)
	if err := view.Driver().Resize(1280, 800); err != nil {
		t.Fatal(err)
	}
	click(hitWorkspaceCommandRun.At(int(session.ID)))
	if shell.typed() != "sudo -i\rcd /srv\r" {
		t.Fatalf("manual continue: %q", shell.typed())
	}
}

func TestWorkspaceInlineWindow(t *testing.T) {
	state := templateManagementState(t, 1)
	host := state.templates[0].HostIDs[0]
	state.templates[0].Commands = map[string]WorkspaceCommand{host: {Command: "cd /srv"}}
	view := openWorkspaceCommandWindow(t, state, 1280)
	click := func(id gfx.ID) { t.Helper(); clickWorkspaceCommandHit(t, view, id) }
	click(hitWorkspaceInlineEdit.At(0))
	if view.Snapshot().Focused != hitWorkspaceInlineField {
		t.Fatal("editor not focused")
	}
	if err := view.Type("tail -f app.log"); err != nil {
		t.Fatal(err)
	}
	// Tab past every editor control; Escape must still cancel the active edit.
	for i := 0; i < 12 && workspaceInlineOwns(view.Snapshot().Focused); i++ {
		if err := view.Driver().Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
	}
	if workspaceInlineOwns(view.Snapshot().Focused) {
		t.Fatal("could not leave editor")
	}
	if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.workspaceCommandEdit.template != "" || state.templates[0].Commands[host].Command != "cd /srv" {
		t.Fatal("escape did not cancel")
	}
	click(hitWorkspaceInlineEdit.At(0))
	if err := view.Type("ls -lh"); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.templates[0].Commands[host].Command != "ls -lh" {
		t.Fatal("enter did not save")
	}
}

func TestWorkspaceCommandLayouts(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.TraditionalChinese, i18n.English} {
		for _, width := range []int{1280, MinimumWindowWidth} {
			t.Run(fmt.Sprintf("%s-%d", language, width), func(t *testing.T) {
				state := templateManagementState(t, 1)
				state.setLanguage(language)
				host := &state.hosts[0]
				host.Label = "Workspace SSH"
				if width == MinimumWindowWidth {
					host.Label += " " + strings.Repeat("long host name ", 10)
				}
				host.User, host.Address, host.Port = "operator", "workbench.example.test", 22022
				state.templates[0].Commands = map[string]WorkspaceCommand{host.CatalogID: {Command: "cd /srv"}}
				state.openWorkspaceDraftEdit(state.templates[0].ID)
				state.workspaceDraft.commandHost = host.ID
				view := openWorkspaceCommandWindow(t, state, width)
				snapshot := view.Snapshot()
				field, ok := hitByIDSnapshot(snapshot.Hits, hitWorkspaceCommandField.At(int(host.ID)))
				if !ok || field.Bounds.Width <= 0 || field.Bounds.X < state.workspaceDraft.bounds.X || field.Bounds.X+field.Bounds.Width > state.workspaceDraft.bounds.X+state.workspaceDraft.bounds.Width {
					t.Fatal("command field outside host list")
				}
				button, ok := hitByIDSnapshot(snapshot.Hits, hitWorkspaceCommandEdit.At(int(host.ID)))
				if !ok || button.Bounds.Width <= 0 {
					t.Fatal("workspace command button is missing")
				}
				var title, address gfx.Rect
				for _, label := range snapshot.Texts {
					if label.Bounds.X >= button.Bounds.X || label.Bounds.Bottom() < button.Bounds.Y || label.Bounds.Y >= field.Bounds.Y {
						continue
					}
					visibleName, elided := strings.CutSuffix(label.Value, "…")
					switch {
					case label.Value == host.Label || elided && visibleName != "" && strings.HasPrefix(host.Label, visibleName):
						title = label.Bounds
					case label.Value == state.sessionAddress(*host):
						address = label.Bounds
					}
				}
				if title.Width == 0 || title.Right() > button.Bounds.X || title.Y >= button.Bounds.Bottom() {
					t.Errorf("workspace command button %v should follow the host name %v", button.Bounds, title)
				}
				if address.Width == 0 || address.Y < button.Bounds.Bottom() {
					t.Errorf("full host address and port should fit below the button: address %v, button %v", address, button.Bounds)
				}
				if dir := os.Getenv("MOTTY_COMMAND_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := view.Wait(300 * time.Millisecond); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("%s-%d-configuration.png", language, width))); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
