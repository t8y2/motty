//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// TestCopyInfoSubmenuHeadless verifies the Copy Info submenu end-to-end through
// the headless driver on both the session tab right-click menu and the host
// card right-click menu, and writes PNG screenshots when
// MOTTY_COPY_INFO_SCREENSHOTS is set.
func TestCopyInfoSubmenuHeadless(t *testing.T) {
	dir := os.Getenv("MOTTY_COPY_INFO_SCREENSHOTS")
	_, options := newWorld(t)
	var copied string
	options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	state := newTestState(t, options)
	useChinese(t, state)
	state.setAppearance(appearanceDark)
	state.hostView = hostViewGrid

	target := state.hosts[0]
	secretName, ok := hostPasswordSecretName(target)
	if !ok {
		t.Fatal("default host does not support password secrets")
	}
	if err := state.secrets.vault.Set(secretName, "Prod@2026!"); err != nil {
		t.Fatal(err)
	}

	view, err := headless.Open(app.Config{
		Title: "Copy Info Submenu", Width: 1400, Height: 900,
		Tree:      Tree(),
		Paint:     state.Paint,
		OnResize:  state.Resize,
		OnPointer: state.Pointer,
		OnKey:     state.Key,
	}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()

	center := func(hit gfx.Hit) (float32, float32) {
		return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
	}
	menuRow := func(label string) gfx.Hit {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.Role == gfx.RoleMenuItem && hit.Label == label {
				return hit
			}
		}
		t.Fatalf("menu row %q not found in snapshot", label)
		return gfx.Hit{}
	}

	// 1. Host card right-click menu -> Copy Info submenu.
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	hostCard, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostList.At(int(target.ID)))
	if !ok {
		t.Fatal("host card hit not found")
	}
	if err := view.RightClick(hostCard.Bounds.X+80, hostCard.Bounds.Y+hostCard.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.hostMenu.open {
		t.Fatal("host card context menu did not open")
	}
	trigger := menuRow(state.messages.CopyInfo)
	if err := view.Click(center(trigger)); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	credRow := menuRow(state.messages.CopyConnectionInfoWithCredentials)
	if credRow.Disabled || credRow.Bounds.X < trigger.Bounds.X+trigger.Bounds.Width-2 {
		t.Fatalf("host submenu row = %+v, trigger = %+v", credRow.Bounds, trigger.Bounds)
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "host-card-copy-info.png")); err != nil {
			t.Fatal(err)
		}
	}
	copied = ""
	if err := view.Click(center(credRow)); err != nil {
		t.Fatal(err)
	}
	if state.hostMenu.open || !strings.Contains(copied, "Prod@2026!") {
		t.Fatalf("host card copy with credentials: open=%v copied=%q", state.hostMenu.open, copied)
	}

	// 2. Session tab right-click menu -> Copy Info submenu.
	if err := view.RightClick(hostCard.Bounds.X+80, hostCard.Bounds.Y+hostCard.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Click(center(menuRow(state.messages.Connect))); err != nil {
		t.Fatal(err)
	}
	workspace := state.deck.ActiveWorkspace()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	tabHit, ok := hitByIDSnapshot(view.Snapshot().Hits, hitTabList.At(int(workspace)))
	if !ok {
		t.Fatal("session tab hit not found")
	}
	if err := view.RightClick(center(tabHit)); err != nil {
		t.Fatal(err)
	}
	if !state.tabMenu.open {
		t.Fatal("session tab context menu did not open")
	}
	trigger = menuRow(state.messages.CopyInfo)
	if err := view.Click(center(trigger)); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	credRow = menuRow(state.messages.CopyConnectionInfoWithCredentials)
	if credRow.Disabled || credRow.Bounds.X < trigger.Bounds.X+trigger.Bounds.Width-2 {
		t.Fatalf("tab submenu row = %+v, trigger = %+v", credRow.Bounds, trigger.Bounds)
	}
	if dir != "" {
		if err := view.Screenshot(filepath.Join(dir, "tab-menu-copy-info.png")); err != nil {
			t.Fatal(err)
		}
	}
	copied = ""
	if err := view.Click(center(credRow)); err != nil {
		t.Fatal(err)
	}
	if state.tabMenu.open || !strings.Contains(copied, "Prod@2026!") {
		t.Fatalf("tab menu copy with credentials: open=%v copied=%q", state.tabMenu.open, copied)
	}
}
