//go:build darwin || windows || linux

package workbench

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// TestCopyInfoSubmenuHeadless verifies the top-level quick copy actions, the
// Copy Info submenu, and the credential confirmation dialog end-to-end through
// the headless driver on both the host card menu and the session tab menu.
func TestCopyInfoSubmenuHeadless(t *testing.T) {
	dir := os.Getenv("MOTTY_COPY_INFO_SCREENSHOTS")
	_, options := newWorld(t)
	var copied string
	options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	options.ClipboardText = func() (string, error) {
		return copied, nil
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

	// 1. Host card right-click menu -> one-click Copy IP:PORT stays at top level.
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
	wantAddr := net.JoinHostPort(target.Address, strconv.Itoa(int(target.Port)))
	if err := view.Click(center(menuRow(state.messages.CopyAddressAndPort))); err != nil {
		t.Fatal(err)
	}
	if state.hostMenu.open || copied != wantAddr {
		t.Fatalf("host top-level Copy IP:PORT: open=%v copied=%q want=%q", state.hostMenu.open, copied, wantAddr)
	}

	// 2. Host card right-click menu -> Copy Info submenu -> confirm credential copy.
	if err := view.RightClick(hostCard.Bounds.X+80, hostCard.Bounds.Y+hostCard.Bounds.Height/2); err != nil {
		t.Fatal(err)
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
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.hostMenu.open || state.modal != modalCopyCredentialConfirm || copied != "" {
		t.Fatalf("host card credential confirm: open=%v modal=%v copied=%q", state.hostMenu.open, state.modal, copied)
	}
	if dir != "" {
		if err := view.Screenshot(filepath.Join(dir, "copy-credential-confirm.png")); err != nil {
			t.Fatal(err)
		}
	}
	confirmBtn, ok := hitByIDSnapshot(view.Snapshot().Hits, hitCopyCredentialConfirmOK)
	if !ok {
		t.Fatal("credential confirmation button missing")
	}
	if err := view.Click(center(confirmBtn)); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || !strings.Contains(copied, "Prod@2026!") {
		t.Fatalf("host card copy with credentials after confirm: modal=%v copied=%q", state.modal, copied)
	}

	// 3. Session tab right-click menu -> Copy Info submenu -> confirm credential copy.
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
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	confirmBtn, ok = hitByIDSnapshot(view.Snapshot().Hits, hitCopyCredentialConfirmOK)
	if !ok {
		t.Fatal("tab credential confirmation button missing")
	}
	if err := view.Click(center(confirmBtn)); err != nil {
		t.Fatal(err)
	}
	if state.tabMenu.open || state.modal != modalNone || !strings.Contains(copied, "Prod@2026!") {
		t.Fatalf("tab menu copy with credentials: open=%v modal=%v copied=%q", state.tabMenu.open, state.modal, copied)
	}
}
