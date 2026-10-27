package workbench

import (
	"path"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func TestLocalSFTPMenuShowsAndUploadsToServerDirectory(t *testing.T) {
	for _, entry := range []string{"report.txt", "photos"} {
		t.Run(entry, func(t *testing.T) {
			state, session, _, remote := openTestSFTP(t)
			view := openLocalPane(t, state, session, localFixture(t))
			clickSFTPLocalRow(t, state, view, entry)
			view.path = "/home/test/目标目录"
			if err := remote.Mkdir(view.path); err != nil {
				t.Fatal(err)
			}
			state.sftpMenu = sftpMenu{open: true, session: session.ID, at: ui.Point{X: 400.4, Y: 200.4}}
			canvas, _ := paintedTexts(state)
			send, ok := hitByID(canvas, hitSFTPMenuSend)
			if !ok || send.Value != view.path || send.Hint != view.path || send.Disabled {
				t.Fatalf("upload target missing from action: %+v", send)
			}
			var label, directory ui.Rect
			for _, text := range canvas.Texts() {
				if text.Value == state.messages.Tools.SendToServer {
					label = text.Bounds
				}
				if text.Value == view.path {
					directory = text.Bounds
				}
			}
			if directory.Width <= 0 || directory.Y < label.Y+label.Height || directory.Y+directory.Height > send.Bounds.Y+send.Bounds.Height {
				t.Fatalf("target is not the action's second line: label=%+v directory=%+v row=%+v", label, directory, send.Bounds)
			}
			state.Click(hitSFTPMenuSend)
			waitSFTP(t, state, session, func(view *sftpView) bool {
				return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
			})
			name := entry
			if entry == "photos" {
				name = path.Join(entry, "a.jpg")
			}
			remote.mu.Lock()
			_, uploaded := remote.files[path.Join(view.path, name)]
			remote.mu.Unlock()
			if !uploaded {
				t.Fatal("upload destination differs from the displayed directory")
			}
		})
	}
}

func TestLocalSFTPMenuFitsFractionalPositionsAndLongPaths(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openLocalPane(t, state, session, localFixture(t))
	clickSFTPLocalRow(t, state, view, "report.txt")
	view.path = "/home/test/" + strings.Repeat("很长的目录/", 30)
	for _, scale := range []float32{1, 1.25, 1.5, 1.75, 2} {
		for _, at := range []ui.Point{{X: 200, Y: 100.4}, {X: 990.4, Y: 690.4}} {
			var canvas ui.Canvas
			canvas.SetPixelScale(scale)
			state.sftpMenu = sftpMenu{open: true, session: session.ID, at: at}
			state.Paint(&canvas, ui.Size{Width: 1000, Height: 700})
			if _, ok := hitByID(&canvas, hitMenuScrollBar); ok {
				t.Fatalf("scale=%v at=%+v: fitting menu has a scrollbar", scale, at)
			}
			send, ok := hitByID(&canvas, hitSFTPMenuSend)
			if !ok || send.Bounds.X < 0 || send.Bounds.Y < 0 || send.Bounds.X+send.Bounds.Width > 1000 || send.Bounds.Y+send.Bounds.Height > 700 {
				t.Fatalf("menu outside viewport: %+v", send)
			}
			if send.Hint != view.path {
				t.Fatal("long path hint was truncated")
			}
			found := false
			for _, text := range canvas.Texts() {
				if strings.HasPrefix(text.Value, "/home/test/") && strings.HasSuffix(text.Value, "…") {
					found = true
				}
			}
			if !found {
				t.Fatal("long path is not ellipsized")
			}
		}
	}
}
