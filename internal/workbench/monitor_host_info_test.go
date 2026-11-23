package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
)

func TestMonitorHostInfoUsesTargetLoginAndConfiguredHost(t *testing.T) {
	state, session := monitorFixture(t)
	session.host.Label = "h5-game"
	session.host.Address = "game.example.test"
	session.host.User = "saved-user"
	state.renameSession(session, "custom-tab-title")
	session.endpoint = sshconn.Endpoint{Host: "game.example.test", User: "connected-user"}
	session.role = sshconn.EndpointTarget
	want := [3]string{"h5-game", "game.example.test", "connected-user"}
	if got := state.monitorHostInfo(session); got != want {
		t.Fatalf("host information = %q, want %q", got, want)
	}
	// Editing a saved account must not mislabel an already connected session.
	session.host.User = "next-login"
	if got := state.monitorHostInfo(session); got != want {
		t.Fatalf("editing host changed the connected account: %q", got)
	}
	session.endpoint = sshconn.Endpoint{Host: "jump.example.test", User: "jump-user"}
	session.role = sshconn.EndpointProxyJump
	want[2] = "next-login"
	if got := state.monitorHostInfo(session); got != want {
		t.Fatalf("jump account replaced target account: %q", got)
	}
	session.host.User = ""
	want[2] = state.systemUser
	if got := state.monitorHostInfo(session); got != want {
		t.Fatalf("missing default login: %q, want %q", got, want)
	}
}

func TestMonitorCurrentSSHUserUsesAuthenticatedTargetAndPrivacy(t *testing.T) {
	state, session := monitorFixture(t)
	session.host.User = "saved-user"
	session.endpoint = sshconn.Endpoint{Host: "host.example", User: "authenticated-user"}
	session.role = sshconn.EndpointTarget
	session.phase = sshconn.PhaseConnected
	if got := state.monitorCurrentSSHUser(session); got != "authenticated-user" {
		t.Fatalf("current SSH user = %q", got)
	}
	session.stats.panel.data.LoginsAvailable = true
	session.stats.panel.data.Logins = []remotestats.Login{{User: "authenticated-user", Terminal: "pts/1", Since: "today"}}
	if got := state.monitorCurrentSSHUser(session); got != "" {
		t.Fatalf("listed login duplicated as %q", got)
	}
	session.stats.panel.data.Logins = nil
	state.hostPrivacy = hostPrivacyState{enabled: true, maskUser: true}
	if got := state.monitorCurrentSSHUser(session); got != "" {
		t.Fatalf("hidden SSH user exposed as %q", got)
	}
	state.hostPrivacy = hostPrivacyState{}
	session.role = sshconn.EndpointProxyJump
	if got := state.monitorCurrentSSHUser(session); got != "" {
		t.Fatalf("jump host user exposed as target user %q", got)
	}
	session.role = sshconn.EndpointTarget
	session.phase = sshconn.PhaseFailed
	if got := state.monitorCurrentSSHUser(session); got != "" {
		t.Fatalf("disconnected SSH user exposed as active %q", got)
	}
}

func TestMonitorHostInfoPrivacyAndNoTooltips(t *testing.T) {
	state, session := monitorFixture(t)
	session.host.Label = privacyTestName
	session.host.Address = privacyTestAddress
	session.host.User = privacyTestUser
	session.endpoint = sshconn.Endpoint{}
	values := [3]string{privacyTestName, privacyTestAddress, privacyTestUser}
	ids := [3]gfx.ID{hitMonitorHostName, hitMonitorHostAddress, hitMonitorHostUser}
	for mask := 0; mask < 8; mask++ {
		state.hostPrivacy = hostPrivacyState{enabled: true, maskName: mask&1 != 0, maskAddress: mask&2 != 0, maskUser: mask&4 != 0}
		canvas := hostPrivacyCanvas(t)
		state.paintHostInfoText(&canvas, gfx.Rect{Width: 640, Height: 18}, session)
		for i, value := range values {
			hit, ok := hitByID(&canvas, ids[i])
			if !ok {
				t.Fatalf("mask %d: missing field %d", mask, i)
			}
			if mask&(1<<i) != 0 {
				assertHostSecretsAbsent(t, &canvas, value)
			} else if !canvasContainsText(&canvas, value) {
				t.Fatalf("mask %d: visible field %q missing from text: %+v", mask, value, hit)
			} else if i == 1 {
				if hit.Label != state.messages.Monitor.AddressCopy || hit.Value != value {
					t.Fatalf("mask %d: copy button = %+v, want the copy label and address value", mask, hit)
				}
			} else if hit.Label != value {
				t.Fatalf("mask %d: visible field %q missing from hit: %+v", mask, value, hit)
			}
			if hit.Hint != "" {
				t.Fatalf("host information still has a tooltip: %+v", hit)
			}
			if i == 1 && mask&2 == 0 && (hit.Role != gfx.RoleButton || hit.Cursor != gfx.PointerCursorHand) {
				t.Fatalf("visible address is not a copy button: %+v", hit)
			}
		}
	}
}

func TestMonitorHostInfoFollowsActiveSession(t *testing.T) {
	state, session := monitorFixture(t)
	session.host.Label, session.host.Address, session.host.User = "h5-game", "192.0.2.10", "root"
	session.endpoint = sshconn.Endpoint{}
	firstID := session.ID
	second := *session
	second.ID++
	second.host.Label, second.host.Address, second.host.User = "database", "192.0.2.20", "admin"
	state.sessions = append(state.sessions, second)
	for _, id := range []uint32{firstID, second.ID, firstID} {
		state.activeSessionID = id
		canvas := hostPrivacyCanvas(t)
		state.paintHostInfoText(&canvas, gfx.Rect{X: 900 + monitorInset, Y: infoBarHeight + monitorControlsHeight, Width: 380 - 2*monitorInset, Height: 18}, state.contextSession())
		want := state.monitorHostInfo(state.contextSession())
		for i, fieldID := range []gfx.ID{hitMonitorHostName, hitMonitorHostAddress, hitMonitorHostUser} {
			hit, ok := hitByID(&canvas, fieldID)
			wantLabel := want[i]
			if i == 1 {
				wantLabel = state.messages.Monitor.AddressCopy
			}
			if !ok || hit.Label != wantLabel || (i == 1 && hit.Value != want[i]) {
				t.Fatalf("session %d: stale host information: %+v, want %q", id, hit, want[i])
			}
		}
	}
}

func TestMonitorHostInfoLayoutAndScrollAcrossPages(t *testing.T) {
	state, session := monitorFixture(t)
	session.host.Label = strings.Repeat("production-game-", 8)
	session.host.Address = "2001:db8:1234:5678:abcd:ef01:2345:6789"
	session.host.User = "root"
	session.endpoint = sshconn.Endpoint{}
	for _, width := range []float32{280, 360, 640} {
		for page := range 3 {
			p := &session.stats.panel
			p.page = page
			b := gfx.Rect{X: 20, Y: 40, Width: width, Height: 320}
			canvas := hostPrivacyCanvas(t)
			state.paintHostInfoText(&canvas, gfx.Rect{X: b.X + monitorInset, Y: b.Y + infoBarHeight + monitorControlsHeight, Width: b.Width - 2*monitorInset, Height: 18}, session)
			previousRight := b.X
			for _, id := range []gfx.ID{hitMonitorHostName, hitMonitorHostAddress, hitMonitorHostUser} {
				hit, ok := hitByID(&canvas, id)
				if !ok || hit.Bounds.X < previousRight || hit.Bounds.X+hit.Bounds.Width > b.X+b.Width-monitorInset+1 {
					t.Fatalf("width %g, page %d: overlapping or missing field: %+v", width, page, hit)
				}
				if hit.Bounds.Y != b.Y+infoBarHeight+monitorControlsHeight {
					t.Fatalf("host information is not below navigation: %+v", hit)
				}
				previousRight = hit.Bounds.X + hit.Bounds.Width
			}
			if hit, _ := hitByID(&canvas, hitMonitorHostName); hit.Hint != "" || hit.Label != session.host.Label {
				t.Fatal("truncated name lost its accessible label or kept its tooltip")
			}
			state.monitorRect = b
			geometry := state.monitorScrollArea(p).Geometry()
			if geometry.Viewport.Y != b.Y+infoBarHeight+monitorControlsHeight {
				t.Fatalf("scroll viewport overlaps host information: %+v", geometry.Viewport)
			}
			state.scrollMonitor(gfx.ScrollEvent{X: b.X + 100, Y: b.Y + 200, DeltaY: 10000})
			if p.scroll != geometry.MaxOffsetY {
				t.Fatalf("scroll cannot reach content bottom: %g, want %g", p.scroll, geometry.MaxOffsetY)
			}
		}
	}
}

func TestMonitorHostInfoSeparatorsRequireVisibleFields(t *testing.T) {
	for _, test := range []struct {
		name           string
		width          float32
		wantFields     int
		wantSeparators int
	}{
		{name: "no field space", width: 2},
		{name: "all fields", width: 640, wantFields: 3, wantSeparators: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, session := monitorFixture(t)
			canvas := hostPrivacyCanvas(t)
			state.paintHostInfoText(&canvas, gfx.Rect{Width: test.width, Height: 18}, session)
			fields := 0
			for _, id := range []gfx.ID{hitMonitorHostName, hitMonitorHostAddress, hitMonitorHostUser} {
				if _, ok := hitByID(&canvas, id); ok {
					fields++
				}
			}
			if fields != test.wantFields {
				t.Fatalf("visible fields = %d, want %d", fields, test.wantFields)
			}
			separators := 0
			for _, text := range canvas.Texts() {
				if strings.TrimSpace(text.Value) == "·" {
					separators++
				}
			}
			if separators != test.wantSeparators {
				t.Fatalf("separators = %d, want %d", separators, test.wantSeparators)
			}
		})
	}
}

func TestMonitorHostAddressCopyReportsSuccessAndFailure(t *testing.T) {
	state, session := monitorFixture(t)
	address := "2001:db8:1234:5678:abcd:ef01:2345:6789"
	session.host.Address = address
	var copied string
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	state.Click(hitMonitorHostAddress)
	if copied != address {
		t.Fatalf("copied %q, want the complete address %q without user or port", copied, address)
	}
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].title != state.messages.Monitor.AddressCopied || state.fileNotices.items[0].kind != components.ToastSuccess {
		t.Fatalf("copy did not show a success toast: %+v", state.fileNotices.items)
	}
	state.fileNotices = fileNotices{}
	state.options.SetClipboardText = func(string) error { return errors.New("clipboard unavailable") }
	state.Click(hitMonitorHostAddress)
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].title != state.messages.Monitor.AddressCopyFailed || state.fileNotices.items[0].kind != components.ToastError {
		t.Fatalf("missing error feedback: %+v", state.fileNotices.items)
	}
	state.fileNotices = fileNotices{}
	state.hostPrivacy = hostPrivacyState{enabled: true, maskAddress: true}
	state.Click(hitMonitorHostAddress)
	if len(state.fileNotices.items) != 0 {
		t.Fatal("hidden address can still activate the copy action")
	}
}

func TestMonitorHostAddressCopyLeavesTheAddressLineUnchanged(t *testing.T) {
	state, session := monitorFixture(t)
	state.options.SetClipboardText = func(string) error { return nil }
	session.host.Address = "192.0.2.10"
	state.Click(hitMonitorHostAddress)
	canvas := hostPrivacyCanvas(t)
	state.paintHostInfoText(&canvas, gfx.Rect{Width: 280, Height: 18}, session)
	found := false
	for _, text := range canvas.Texts() {
		if text.Value == session.host.Address {
			found = true
			if text.Style.Color != state.monitorColors().cpu {
				t.Fatal("address copy feedback changed the address text color")
			}
		}
	}
	if !found {
		t.Fatal("address disappeared after copying")
	}
	if canvasContainsText(&canvas, state.messages.Monitor.AddressCopied) {
		t.Fatal("copy feedback was appended to the host information line")
	}
}
