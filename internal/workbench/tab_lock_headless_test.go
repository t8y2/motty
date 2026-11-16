//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestLockedTabMenuAndConfirmationAtDifferentScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, tabs := fourTabs(t)
			state.setLanguage(i18n.Chinese)
			_ = state.deck.Rename(tabs[3], strings.Repeat("很长的标签名", 15))
			lockTab(t, state, tabs[3])
			view, err := headless.Open(app.Config{Title: "Tab protection", Width: 760, Height: 560, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if dir := os.Getenv("MOTTY_TAB_SCREENSHOTS"); dir != "" {
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("tab-locked-%g.png", scale))); err != nil {
					t.Fatal(err)
				}
			}
			close, ok := hitByIDSnapshot(view.Snapshot().Hits, hitTabCloseList.At(int(tabs[3])))
			if !ok {
				t.Fatalf("the locked tab lost its close button in a narrow window; active %d section %d geometry %+v", state.deck.ActiveWorkspace(), state.activeSection, state.chrome.geometry)
			}
			if err := view.Click(close.Bounds.X+close.Bounds.Width/2, close.Bounds.Y+close.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalCloseTabs || len(tabsOf(state)) != 4 {
				t.Fatal("the physical click bypassed locked-tab confirmation")
			}
			for _, label := range []string{state.messages.Cancel, state.messages.CloseTabsConfirm} {
				hit, ok := view.Hit(label)
				if !ok || hit.Bounds.Y < 0 || hit.Bounds.Y+hit.Bounds.Height > 560 || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 760 {
					t.Fatalf("dialog action %q is clipped: %+v", label, hit.Bounds)
				}
			}
			if err := view.ClickLabel(state.messages.Cancel); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalNone || !state.deck.Locked(tabs[3]) {
				t.Fatal("cancel changed the tab lock")
			}
		})
	}
}
