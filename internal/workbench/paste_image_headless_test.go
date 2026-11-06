//go:build darwin || windows || linux

package workbench

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

func TestImageOnlyClipboardPastesIntoLocalTerminalThroughNativeShortcut(t *testing.T) {
	for _, shortcut := range []bool{true, false} {
		t.Run(fmt.Sprintf("app_shortcut_%v", shortcut), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("LOCALAPPDATA", t.TempDir())
			state, owner := openLocal(t)
			state.Wake()
			if owner.term == nil || owner.term.shell == nil {
				t.Fatal("local shell did not start")
			}
			config := app.Config{Title: "local image paste", Width: 1000, Height: 700, Tree: Tree(),
				Paint: state.Paint, TextInput: state.TerminalInput, OnPointer: state.Pointer,
				OnKey: state.Key, OnResize: state.Resize}
			if shortcut {
				config.OnShortcut = state.Shortcut
			}
			preview, err := headless.Open(config, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			if err := preview.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, owner.Address)); err != nil {
				t.Fatal(err)
			}
			picture := actualPastedPNG(t)
			if err := preview.Driver().SetClipboardImage(picture); err != nil {
				t.Fatal(err)
			}
			owner.term.send([]byte("test -s "))
			if handled, err := preview.Driver().Shortcut('v'); err != nil || !handled {
				t.Fatalf("paste: handled=%v err=%v", handled, err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for owner.pastingImage && time.Now().Before(deadline) {
				state.Wake()
				time.Sleep(time.Millisecond)
			}
			if owner.pastingImage {
				t.Fatal("local image paste timed out")
			}
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			cache, err := os.UserCacheDir()
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(cache, "motty", "paste"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("saved images = %v, err = %v", entries, err)
			}
			name := filepath.Join(cache, "motty", "paste", entries[0].Name())
			if data, err := os.ReadFile(name); err != nil || !bytes.Equal(data, picture) {
				t.Fatalf("saved picture %q: %v", name, err)
			}
			marker := filepath.Join(t.TempDir(), "readable")
			owner.term.send([]byte("&& printf ok > " + shellEscapePath(marker) + "\n"))
			for time.Now().Before(deadline) {
				state.Wake()
				if _, err := os.Stat(marker); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("local shell did not read the pasted picture %q: %v; screen: %q", name, err, screenText(state, 0))
			}
		})
	}
}
