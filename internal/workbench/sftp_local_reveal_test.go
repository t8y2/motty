package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestLocalSFTPContextMenuRevealsSelectedFileAndFolderInFileManager(t *testing.T) {
	for _, localOnly := range []bool{false, true} {
		mode := map[bool]string{false: "ssh-panel", true: "local-terminal"}[localOnly]
		for _, target := range []string{"report.txt", "photos"} {
			t.Run(mode+"/"+target, func(t *testing.T) {
				state, session, _, _ := openTestSFTP(t)
				root := localFixture(t)
				view := openLocalPane(t, state, session, root)
				view.localOnly = localOnly
				clickSFTPLocalRow(t, state, view, target)
				revealed := make(chan string, 1)
				state.options.RevealPath = func(path string) error {
					revealed <- path
					return nil
				}
				state.sftpMenu = sftpMenu{open: true, session: session.ID, at: gfx.Point{X: 300, Y: 200}}
				canvas, _ := paintedTexts(state)
				if hit, ok := menuHit(canvas, state, hitSFTPMenuReveal); !ok || hit.Disabled {
					t.Fatal("local reveal menu action is missing or disabled")
				}
				chooseMenu(t, state, hitSFTPMenuReveal)
				waitFor(t, state, func() bool { return !state.revealBusy })
				want := filepath.Join(root, target)
				select {
				case got := <-revealed:
					if got != want {
						t.Fatalf("revealed %q, want %q", got, want)
					}
				default:
					t.Fatal("file manager did not receive the selected path")
				}
			})
		}
	}
}

func TestLocalSFTPContextMenuBackgroundOpensCurrentFolderInFileManager(t *testing.T) {
	for _, localOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "ssh-panel", true: "local-terminal"}[localOnly], func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			root := localFixture(t)
			view := openLocalPane(t, state, session, root)
			view.localOnly = localOnly
			view.lfolder.clearSelection()
			opened := make(chan string, 1)
			state.options.OpenFolder = func(directory string) error {
				opened <- directory
				return nil
			}
			state.sftpMenu = sftpMenu{open: true, session: session.ID, at: gfx.Point{X: 300, Y: 200}}
			canvas, _ := paintedTexts(state)
			if hit, ok := menuHit(canvas, state, hitSFTPMenuOpenFolder); !ok || hit.Disabled {
				t.Fatal("local open-folder menu action is missing or disabled")
			}
			chooseMenu(t, state, hitSFTPMenuOpenFolder)
			waitFor(t, state, func() bool { return !state.revealBusy })
			select {
			case got := <-opened:
				if got != root {
					t.Fatalf("opened %q, want %q", got, root)
				}
			default:
				t.Fatal("file manager did not receive the current folder")
			}
		})
	}
}

func TestRemoteSFTPContextMenuDoesNotOfferLocalFileManagerActions(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	if len(view.rows) == 0 {
		t.Fatal("remote fixture has no rows")
	}
	view.sftpFolder.selectRow(0, 0)
	ids := sftpMenuRowIDs(state.sftpMenuRows(session, view))
	if ids[hitSFTPMenuReveal] || ids[hitSFTPMenuOpenFolder] {
		t.Fatalf("remote selection menu offered local file manager actions: %+v", ids)
	}
	view.sftpFolder.clearSelection()
	ids = sftpMenuRowIDs(state.sftpMenuRows(session, view))
	if ids[hitSFTPMenuReveal] || ids[hitSFTPMenuOpenFolder] {
		t.Fatalf("remote background menu offered local file manager actions: %+v", ids)
	}
}
