//go:build windows || darwin || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestFileTransferHostOpensManagerWithoutTerminal(t *testing.T) {
	for _, transfer := range []transferMode{transferSFTP, transferSCP} {
		for _, entry := range []string{"list", "grid", "enter", "menu", "new-tab", "panel", "file-tab"} {
			t.Run(Host{Transfer: transfer}.savedTransfer()+"/"+entry, func(t *testing.T) {
				state, original, _, native, _ := dualSFTPFixture(t, 1)
				target := original.session.host
				target.ID += 1000
				target.CatalogID = "file-transfer-target"
				target.Transfer = transfer
				state.hosts = append(state.hosts, target)
				state.hostDataChanged()
				state.activeSection = sectionHosts
				state.selectedHostID = target.ID
				sessions := len(state.sessions)
				state.pendingWorkspace = state.deck.ActiveWorkspace()
				state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
				switch entry {
				case "list", "grid":
					state.hostView = hostViewList
					if entry == "grid" {
						state.hostView = hostViewGrid
					}
					var canvas gfx.Canvas
					state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
					var row gfx.ID
					for _, hit := range canvas.Hits() {
						if host, ok := state.hostForRow(hit.ID); ok && host.ID == target.ID {
							row = hit.ID
							break
						}
					}
					if row.IsZero() {
						t.Fatal("saved host row was not painted")
					}
					doubleClick(state, row)
				case "enter":
					state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitHostList.At(int(target.ID))})
				case "menu":
					state.hostMenu = hostMenu{host: target.ID}
					state.chooseHostMenu(hitHostMenuConnect)
				case "new-tab":
					state.chooseNewTabRow(hitNewTabHostList.At(len(state.hosts) - 1))
				case "file-tab":
					state.sftpOpenMode = sftpOpenTab
					doubleClick(state, hitHostList.At(int(target.ID)))
				case "panel":
					state.connectHostsPanelHost(target)
				}
				if len(state.sessions) != sessions {
					t.Fatal("opening a file-transfer host created a terminal session")
				}
				var manager *sftpWindow
				for _, w := range state.sftpWindows {
					if w.session.HostID == target.ID {
						manager = w
					}
				}
				if manager == nil || !manager.ownsConnection || manager.session.term != nil {
					t.Fatal("file-transfer host did not open a standalone manager")
				}
				if manager.session.host.Transfer != transfer {
					t.Fatal("file manager did not retain the selected protocol or open a window")
				}
				if entry == "file-tab" {
					if state.activeSFTPTab != manager.session.ID || len(native.windows) != 1 {
						t.Fatal("file manager did not follow the tab preference")
					}
				} else if len(native.windows) != 2 {
					t.Fatal("file manager did not open a window")
				}
				if state.pendingPane != 0 || state.pendingWorkspace != 0 {
					t.Fatal("file manager left a pending terminal destination")
				}
			})
		}
	}
}
