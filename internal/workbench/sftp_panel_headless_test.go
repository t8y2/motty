//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	layoutcore "motty/internal/workspace"
)

func TestSFTPPanelBackgroundMenuOpensNewFolder(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local-%t", local), func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			view := session.files
			width := 1400
			if local {
				openLocalPane(t, state, session, t.TempDir())
				width = 800 // Exercise the narrow drawer with an empty directory.
			} else {
				view.active().selectRow(0, 0)
			}
			preview, err := headless.Open(app.Config{
				Title: "File panel background", Width: width, Height: 900, Tree: Tree(),
				Paint: state.Paint, OnPointer: state.Pointer, OnResize: state.Resize,
			}, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			list, ok := hitByIDSnapshot(preview.Snapshot().Hits, hitSFTPListBackground)
			if !ok {
				t.Fatal("missing file-list background")
			}
			if err := preview.RightClick(list.Bounds.X+24, list.Bounds.Y+list.Bounds.Height-12); err != nil {
				t.Fatal(err)
			}
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			if !state.sftpMenu.open || selectedSFTPCount(view) != 0 {
				t.Fatal("right-click did not open the menu for the current directory")
			}
			for _, hit := range preview.Snapshot().Hits {
				if hit.Layer != dialogLayer || hit.ID != hitSFTPNewDirectory || hit.Disabled {
					continue
				}
				if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if view.dialog != sftpDialogCreateDirectory || view.local != local {
					t.Fatalf("New Folder opened dialog %v with local=%t", view.dialog, view.local)
				}
				return
			}
			t.Fatal("missing enabled New Folder menu item")
		})
	}
}

func TestSFTPPanelDockingThroughNativeInputAtDisplayScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, session, remote, oldPanel, paths := dropFixture(t)
			files, worker := session.files, session.files.worker
			state.ai.open = true
			preview, err := headless.Open(app.Config{
				Title: "File panel", Width: 1600, Height: 900, Tree: Tree(), Paint: state.Paint,
				OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize, OnFileDrop: state.FileDrop,
			}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			click := func(id gfx.ID) {
				t.Helper()
				hit, ok := findSnapshotHit(preview.Snapshot(), id)
				if !ok {
					t.Fatal("missing control", id)
				}
				if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			move, ok := findSnapshotHit(preview.Snapshot(), contextID(hitSFTPPanelSide))
			close, _ := findSnapshotHit(preview.Snapshot(), contextID(hitSFTPClose))
			if !ok || move.Bounds.Y != close.Bounds.Y || move.Bounds.X+move.Bounds.Width > close.Bounds.X || move.Label != state.messages.Tools.FilesMoveRight {
				t.Fatal("move action must be beside Close on its left and name its destination")
			}
			click(contextID(hitSFTPPanelSide))
			context, _ := state.deck.Context(state.deck.ActiveWorkspace())
			panel, terminal, assistant := state.contextRect, state.workspacePixelBounds(gfx.Size{Width: 1600, Height: 900}), state.ai.panel.rect
			if context.Side != layoutcore.ContextRight || panel.X <= 0 || terminal.X != 0 ||
				terminal.X+terminal.Width > panel.X || panel.X+panel.Width > assistant.X || assistant.Width == 0 {
				t.Fatalf("right panel overlaps terminal/AI: terminal=%+v files=%+v AI=%+v", terminal, panel, assistant)
			}
			if session.files != files || files.worker != worker || files.path != "/home/test" || state.sftpPanelSide != layoutcore.ContextLeft {
				t.Fatal("moving changed files, connection or the configured default")
			}
			divider, ok := findSnapshotHit(preview.Snapshot(), hitContextDivider)
			if !ok {
				t.Fatal("missing right-side divider")
			}
			y := divider.Bounds.Y + divider.Bounds.Height/2
			x := panel.X + panel.Width - 400 - .5
			if err := preview.Press(divider.Bounds.X+divider.Bounds.Width/2, y); err != nil {
				t.Fatal(err)
			}
			if err := preview.Move(x, y); err != nil {
				t.Fatal(err)
			}
			if err := preview.Release(x, y); err != nil {
				t.Fatal(err)
			}
			context, _ = state.deck.Context(state.deck.ActiveWorkspace())
			if math.Abs(float64(context.Width-400)) > 1 {
				t.Fatalf("right divider reversed or resized against AI: %+v", context)
			}
			// Where the panel was is the terminal now: it may take an upload of its
			// own, but the panel's drop target must not show there.
			state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: oldPanel.X + 10, Y: oldPanel.Y + 70, Paths: paths[:1]})
			if files.dropping {
				t.Fatal("the old left-panel location still shows the panel's drop target")
			}
			state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropLeave})
			row := -1
			for i, candidate := range files.rows {
				if candidate.entry.Name == "docs" {
					row = i
				}
			}
			hit, ok := findSnapshotHit(preview.Snapshot(), sftpListRowID(t, files.active(), row))
			if row < 0 || !ok {
				t.Fatal("missing destination folder")
			}
			if accepted, err := preview.Drop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2, Paths: paths[:1]}); err != nil || !accepted {
				t.Fatal("right-side folder did not accept upload", err)
			}
			waitTransfersDone(t, state, session, 1)
			remote.mu.Lock()
			actual := string(remote.files["/home/test/docs/dropped.txt"])
			remote.mu.Unlock()
			if actual != "dropped" {
				t.Fatal("moving changed the drop target")
			}
			state.Click(hitTransfersClose)
			state.ai.open = false
			if err := preview.Resize(800, 640); err != nil {
				t.Fatal(err)
			}
			if state.contextRect.X+state.contextRect.Width/2 <= 400 || state.workspacePixelBounds(gfx.Size{Width: 800, Height: 640}) != state.baseWorkspacePixelBounds(gfx.Size{Width: 800, Height: 640}) {
				t.Fatal("right drawer reserved terminal width or appeared on the left")
			}
			click(contextID(hitSFTPPanelSide))
			context, _ = state.deck.Context(state.deck.ActiveWorkspace())
			if state.contextRect.X+state.contextRect.Width/2 >= 400 || context.Side != layoutcore.ContextLeft || session.files != files || files.worker != worker {
				t.Fatalf("moving the narrow drawer reset its contents or stayed on the right: panel=%+v context=%+v sameView=%v sameWorker=%v", state.contextRect, context, session.files == files, files.worker == worker)
			}
		})
	}
}
