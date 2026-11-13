package workbench

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
		state.paintMonitorHostInfo(&canvas, gfx.Rect{Width: 640, Height: monitorHostInfoHeight}, session)
		for i, value := range values {
			hit, ok := hitByID(&canvas, ids[i])
			if !ok {
				t.Fatalf("mask %d: missing field %d", mask, i)
			}
			if mask&(1<<i) != 0 {
				assertHostSecretsAbsent(t, &canvas, value)
			} else if !canvasContainsText(&canvas, value) || hit.Label != value {
				t.Fatalf("mask %d: visible field %q missing from text: %+v", mask, value, hit)
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
		state.paintIndependentMonitor(&canvas, gfx.Size{Width: 1280, Height: 900})
		want := state.monitorHostInfo(state.contextSession())
		for i, fieldID := range []gfx.ID{hitMonitorHostName, hitMonitorHostAddress, hitMonitorHostUser} {
			hit, ok := hitByID(&canvas, fieldID)
			if !ok || hit.Label != want[i] {
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
			state.paintMonitor(&canvas, b, session)
			previousRight := b.X
			for _, id := range []gfx.ID{hitMonitorHostName, hitMonitorHostAddress, hitMonitorHostUser} {
				hit, ok := hitByID(&canvas, id)
				if !ok || hit.Bounds.X < previousRight || hit.Bounds.X+hit.Bounds.Width > b.X+b.Width-monitorInset+1 {
					t.Fatalf("width %g, page %d: overlapping or missing field: %+v", width, page, hit)
				}
				if hit.Bounds.Y != b.Y+infoBarHeight+monitorControlsHeight+6 {
					t.Fatalf("host information is not below navigation: %+v", hit)
				}
				previousRight = hit.Bounds.X + hit.Bounds.Width
			}
			if hit, _ := hitByID(&canvas, hitMonitorHostName); hit.Hint != "" || hit.Label != session.host.Label {
				t.Fatal("truncated name lost its accessible label or kept its tooltip")
			}
			geometry := state.monitorScrollArea(p).Geometry()
			if geometry.Viewport.Y != b.Y+infoBarHeight+monitorControlsHeight+monitorHostInfoHeight {
				t.Fatalf("scroll viewport overlaps host information: %+v", geometry.Viewport)
			}
			state.scrollMonitor(gfx.ScrollEvent{X: b.X + 100, Y: b.Y + 200, DeltaY: 10000})
			if p.scroll != geometry.MaxOffsetY {
				t.Fatalf("scroll cannot reach content bottom: %g, want %g", p.scroll, geometry.MaxOffsetY)
			}
		}
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
	if session.stats.panel.copiedAddress != address || len(state.fileNotices.items) != 0 {
		t.Fatalf("copy did not set inline feedback or still showed an alert: %+v", state.fileNotices.items)
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

func TestMonitorHostAddressCopyColorRestoresAfterTwoSeconds(t *testing.T) {
	state, session := monitorFixture(t)
	now := 10 * time.Second
	state.options.Now = func() time.Duration { return now }
	state.options.SetClipboardText = func(string) error { return nil }
	session.host.Address = "192.0.2.10"
	for _, dark := range []bool{false, true} {
		if dark {
			state.setAppearance(appearanceDark)
		}
		state.Click(hitMonitorHostAddress)
		for _, elapsed := range []time.Duration{0, 1999 * time.Millisecond, 2 * time.Second} {
			now = session.stats.panel.addressCopiedUntil - 2*time.Second + elapsed
			canvas := hostPrivacyCanvas(t)
			state.paintMonitorHostInfo(&canvas, gfx.Rect{Width: 280, Height: monitorHostInfoHeight}, session)
			wantColor := state.pal.success
			if elapsed == 2*time.Second {
				wantColor = state.monitorColors().cpu
			}
			found := false
			for _, text := range canvas.Texts() {
				if text.Value == session.host.Address {
					found = true
					if text.Style.Color != wantColor {
						t.Fatalf("dark=%v, elapsed=%v: IP color did not match feedback state", dark, elapsed)
					}
				}
			}
			if !found || canvasContainsText(&canvas, state.messages.Monitor.AddressCopied) != (elapsed < 2*time.Second) {
				t.Fatalf("dark=%v, elapsed=%v: missing IP or incorrect inline feedback", dark, elapsed)
			}
		}
	}
}
