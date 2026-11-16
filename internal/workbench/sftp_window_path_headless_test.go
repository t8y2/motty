//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/keymap"
	"motty/internal/sftpclient"
)

func TestSFTPWindowBreadcrumbsNavigateAndEditBothPanes(t *testing.T) {
	state, window, _, host, root := dualSFTPFixture(t, 1.25)
	preview := host.windows[0].view
	for _, local := range []bool{false, true} {
		side := sideIndex(local)
		parent, child := "/home/test", "/home/test/docs"
		if local {
			parent, child = root, filepath.Join(root, "tree")
		}
		window.navigate(local, child)
		dualWait(t, state, window, preview, func() bool { return !window.navigationBusy() })
		crumbs := tree.Sub(sftpWindowLocal, tree.Name("breadcrumbs"))
		edit := tree.ID(sftpWindowLocal, "path-edit")
		if !local {
			crumbs = tree.Sub(sftpWindowRemote, tree.Name("breadcrumbs"))
			edit = tree.ID(sftpWindowRemote, "path-edit")
		}
		folder := window.folder(local)
		parentIndex := -1
		for index, crumb := range folder.crumbs {
			if crumb.path == parent {
				parentIndex = index
			}
		}
		if parentIndex < 0 {
			t.Fatal("parent breadcrumb was not constructed")
		}
		dualClickID(t, preview, crumbs.At(parentIndex))
		dualWait(t, state, window, preview, func() bool { return !window.navigationBusy() && folder.path == parent })
		bounds := dualHit(t, preview, edit)
		if err := preview.Click(bounds.X+bounds.Width-3, bounds.Y+bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if preview.Snapshot().Focused != sftpWindowPath[side] || !folder.pathEditing {
			t.Fatal("trailing blank click did not mount and focus the path editor")
		}
		if err := preview.Driver().SelectText(0, folder.pathInput().RuneCount()); err != nil {
			t.Fatal(err)
		}
		if err := preview.Type(child); err != nil {
			t.Fatal(err)
		}
		if err := preview.Driver().Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
		dualWait(t, state, window, preview, func() bool { return !window.navigationBusy() && folder.path == child })
		if folder.pathEditing {
			t.Fatal("accepted navigation did not return to breadcrumbs")
		}
		if preview.Snapshot().Focused != edit {
			t.Fatalf("accepted navigation left focus at %v, want the breadcrumb edit button %v", preview.Snapshot().Focused, edit)
		}
	}
	if output := os.Getenv("MOTTY_SFTP_WINDOW_PATH_EVIDENCE"); output != "" {
		if err := preview.Screenshot(filepath.Join(output, "881-both-breadcrumbs.png")); err != nil {
			t.Fatal(err)
		}
	}
}

// A pending server listing must not take the local path field's editor away:
// the panes are independent, and the local side is not waiting for the server.
func TestSFTPWindowLocalPathStaysEditableWhileRemoteNavigates(t *testing.T) {
	_, window, _, host, _ := dualSFTPFixture(t, 1.25)
	preview := host.windows[0].view
	dualClickID(t, preview, sftpWindowPath[0])
	if got := preview.Snapshot().Focused; got != sftpWindowPath[0] {
		t.Fatalf("the local edit button focused %v", got)
	}
	// The server pane is listing; the local pane is not.
	window.view.busy = true
	config := host.windows[0].config
	if err := preview.Resize(config.Width+1, config.Height); err != nil {
		t.Fatal(err)
	}
	if err := preview.Type("x"); err != nil {
		t.Fatalf("the local path field stopped accepting typing while the server listed: %v", err)
	}
	if draft := window.view.lfolder.pathInput().String(); !strings.Contains(draft, "x") {
		t.Fatalf("local path draft = %q, want the typed x", draft)
	}
}

func TestSFTPWindowBreadcrumbHostedAndPermanentPathInput(t *testing.T) {
	for _, tab := range []bool{false, true} {
		for _, permanent := range []bool{false, true} {
			t.Run(fmt.Sprintf("tab-%t/permanent-%t", tab, permanent), func(t *testing.T) {
				state, _, window, _, _ := openSFTPTabFixture(t)
				state.sftpPathInput = permanent
				config := window.config()
				if tab {
					config = app.Config{Title: "File transfer paths", Width: 1100, Height: 720, Tree: Tree(),
						Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
						OnResize: state.Resize, OnWake: state.Wake, TextInput: state.SFTPTabInput}
				}
				preview, err := headless.Open(config, nil, headless.Options{Scale: 1.5})
				if err != nil {
					t.Fatal(err)
				}
				defer preview.Close()
				dualClickID(t, preview, dualRow(t, window, false, "notes.txt"))
				if _, err := preview.Driver().ShortcutChord(keymap.Primary, 'l'); err != nil {
					t.Fatal(err)
				}
				if preview.Snapshot().Focused != sftpWindowPath[1] {
					t.Fatal("path shortcut did not focus the remote path input")
				}
				if err := preview.Type("docs"); err != nil {
					t.Fatal(err)
				}
				if err := preview.Driver().Key(gfx.KeyEnter, 0); err != nil {
					t.Fatal(err)
				}
				dualWait(t, state, window, preview, func() bool { return !window.navigationBusy() && window.view.path == "/home/test/docs" })
				if err := preview.Resize(config.Width+1, config.Height); err != nil {
					t.Fatal(err)
				}
				if _, input := hitByIDSnapshot(preview.Snapshot().Hits, sftpWindowPath[1]); input != permanent {
					t.Fatalf("accepted path ignored permanent-input preference: input=%t permanent=%t editing=%t request=%q", input, permanent, window.view.pathEditing, window.view.pathRequest)
				}
				if window.view.lfolder.path == window.view.path {
					t.Fatal("remote editing changed the independent local path")
				}
				long := "/home/test/" + strings.Repeat("long directory/", 12) + "logs"
				window.navigate(false, long)
				dualWait(t, state, window, preview, func() bool { return !window.navigationBusy() })
				if err := preview.Resize(640, 600); err != nil {
					t.Fatal(err)
				}
				if !permanent {
					bounds := dualHit(t, preview, sftpWindowPathEdit[1])
					if bounds.Width <= 0 || bounds.X < 0 || bounds.X+bounds.Width > 640 {
						t.Fatal("long breadcrumb path overflowed its narrow pane")
					}
				}
				if output := os.Getenv("MOTTY_SFTP_WINDOW_PATH_EVIDENCE"); output != "" {
					if err := preview.Screenshot(filepath.Join(output, fmt.Sprintf("881-tab-%t-permanent-%t-narrow.png", tab, permanent))); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestSFTPWindowBreadcrumbDraftCancelsOnEscapeAndFocusLoss(t *testing.T) {
	for _, mode := range []string{"escape", "tab", "row"} {
		t.Run(mode, func(t *testing.T) {
			_, window, _, host, _ := dualSFTPFixture(t, 1)
			preview := host.windows[0].view
			edit := tree.ID(sftpWindowRemote, "path-edit")
			bounds := dualHit(t, preview, edit)
			if err := preview.Click(bounds.X+bounds.Width-3, bounds.Y+bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := preview.Type("unsubmitted"); err != nil {
				t.Fatal(err)
			}
			if mode == "row" {
				dualClickID(t, preview, dualRow(t, window, false, "notes.txt"))
			} else {
				key := gfx.KeyEscape
				if mode == "tab" {
					key = gfx.KeyTab
				}
				if err := preview.Driver().Key(key, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			if window.view.pathEditing || window.view.pathInput().String() != window.view.path || window.view.path != "/home/test" {
				t.Fatal("canceled draft survived or changed the current directory")
			}
			dualHit(t, preview, edit)
		})
	}
}

func TestSFTPWindowBreadcrumbEditSupportsKeyboardActivation(t *testing.T) {
	for _, key := range []gfx.Key{gfx.KeyEnter, gfx.KeySpace} {
		t.Run(fmt.Sprint(key), func(t *testing.T) {
			_, window, _, host, _ := dualSFTPFixture(t, 1)
			preview := host.windows[0].view
			dualClickID(t, preview, sftpWindowPath[1])
			if err := preview.Driver().Key(gfx.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if preview.Snapshot().Focused != sftpWindowPathEdit[1] {
				t.Fatal("Escape did not return focus to the breadcrumb edit button")
			}
			if err := preview.Driver().Key(key, 0); err != nil {
				t.Fatal(err)
			}
			if !window.view.pathEditing || preview.Snapshot().Focused != sftpWindowPath[1] {
				t.Fatal("keyboard activation did not reopen and focus the path editor")
			}
		})
	}
}

func TestSFTPWindowBreadcrumbIMEAndFailedNavigationKeepFocusedDraft(t *testing.T) {
	state, window, remote, host, _ := dualSFTPFixture(t, 1)
	preview := host.windows[0].view
	dualClickID(t, preview, sftpWindowPath[1])
	if err := preview.Compose("\u76ee\u5f55"); err != nil {
		t.Fatal(err)
	}
	if err := preview.Resize(1100, 720); err != nil {
		t.Fatal(err)
	}
	if _, composing := window.view.pathInput().Composition(); !composing || !window.view.pathEditing {
		t.Fatal("repainting canceled the focused path composition")
	}
	if err := preview.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if window.view.busy || window.view.path != "/home/test" {
		t.Fatal("IME Enter submitted a directory navigation")
	}
	if err := preview.Compose(""); err != nil {
		t.Fatal(err)
	}
	window.view.worker.Close()
	<-window.view.worker.Done()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	window.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) {
		return pendingPathErrorRemote{remote, gate}, nil
	}, window.invalidate)
	if err := preview.Driver().SelectText(0, window.view.pathInput().RuneCount()); err != nil {
		t.Fatal(err)
	}
	if err := preview.Type("missing"); err != nil {
		t.Fatal(err)
	}
	if err := preview.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !window.view.busy || !window.view.pathEditing || preview.Snapshot().Focused != sftpWindowPath[1] {
		t.Fatal("pending navigation lost its focused path editor")
	}
	if err := preview.Resize(1101, 720); err != nil {
		t.Fatal(err)
	}
	if window.view.pathInput().String() != "missing" || !window.view.pathEditing {
		t.Fatal("pending read-only repaint replaced the submitted draft")
	}
	release()
	dualWait(t, state, window, preview, func() bool { return !window.navigationBusy() })
	if window.view.path != "/home/test" || window.view.pathInput().String() != "missing" || !window.view.pathEditing || preview.Snapshot().Focused != sftpWindowPath[1] {
		t.Fatal("failed navigation discarded the draft or its focus")
	}
}
