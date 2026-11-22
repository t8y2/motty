package workbench

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"motty/internal/desktop"
	"motty/internal/sshconn"
)

// A host that never chose a quality is saved without one, so an older record
// stays what it was, and one that did survives the round trip.
func TestDesktopQualityIsSavedOnlyWhenChosen(t *testing.T) {
	for _, protocol := range []hostProtocol{protocolVNC, protocolRDP} {
		host := Host{Label: "h", Address: "h.example.com", Protocol: protocol}
		host.normalizeEndpoint()
		host.finish()
		encoded, err := json.Marshal(host.transport())
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"color_depth", "desktop_encoding", "effects"} {
			if strings.Contains(string(encoded), key) {
				t.Errorf("protocol %d: a host that never chose is saved with %s: %s", protocol, key, encoded)
			}
		}
	}

	vnc := Host{Protocol: protocolVNC, ColorDepth: 8, PreferredEncoding: desktop.EncodingHextile}
	var restored Host
	if !restored.applyTransport(vnc.transport()) || restored.ColorDepth != 8 || restored.PreferredEncoding != desktop.EncodingHextile {
		t.Fatalf("VNC restored = %+v", restored)
	}
	// A depth another protocol has is not one of VNC's; an unknown encoding is
	// the automatic one.
	odd := vnc.transport()
	odd.ColorDepth, odd.DesktopEncoding = 24, "tight-maybe"
	restored = Host{}
	if !restored.applyTransport(odd) || restored.ColorDepth != 0 || restored.PreferredEncoding != desktop.EncodingAuto {
		t.Fatalf("VNC restored from a foreign record = %+v", restored)
	}

	effects := desktop.Effects{Wallpaper: true, Animations: true}
	rdp := Host{Protocol: protocolRDP, ColorDepth: 16, Effects: &effects}
	restored = Host{}
	if !restored.applyTransport(rdp.transport()) || restored.ColorDepth != 16 || restored.Effects == nil || *restored.Effects != effects {
		t.Fatalf("RDP restored = %+v", restored)
	}
	// Every effect off is a choice of its own, not the default.
	none := Host{Protocol: protocolRDP, Effects: &desktop.Effects{}}
	restored = Host{}
	if !restored.applyTransport(none.transport()) || restored.Effects == nil || restored.desktopEffects() != (desktop.Effects{}) {
		t.Fatalf("RDP with every effect off restored = %+v", restored)
	}
	if got := (Host{Protocol: protocolRDP}).desktopEffects(); got != desktop.DefaultEffects() {
		t.Fatalf("a host that never chose has effects %+v", got)
	}
}

// What the form shows is what the host gets: the choices of the dialog reach
// the saved host, and the effect switches are RDP's only.
func TestHostDialogSavesTheDesktopQualityChoices(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.setProtocol(protocolVNC)
	canvas, _ := paintAt(state, gfx.Size{Width: 1200, Height: 1400})
	if _, ok := hitByID(canvas, hitDialogEffectWallpaper); ok {
		t.Fatal("a VNC host was offered an RDP effect")
	}
	state.editor.setProtocol(protocolRDP)
	canvas, _ = paintAt(state, gfx.Size{Width: 1200, Height: 1400})
	if _, ok := hitByID(canvas, hitDialogEffectWallpaper); !ok {
		t.Fatal("an RDP host was not offered the effects")
	}
	state.Click(hitDialogEffectWallpaper)
	state.Click(hitDialogEffectCursorShadow)
	state.editor.colorDepth = 16
	typeInto(state, hitDialogLabel, "Slow link")
	typeInto(state, hitDialogAddress, "slow.example.com")
	typeInto(state, hitDialogUser, `CORP\alice`)
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("submit: modal=%d error=%q", state.modal, state.dialogError)
	}
	index := state.hostIndexByEndpoint(Host{Protocol: protocolRDP, Address: "slow.example.com", User: `CORP\alice`, Port: 3389})
	if index < 0 {
		t.Fatal("the RDP host was not added")
	}
	host := state.hosts[index]
	want := desktop.DefaultEffects()
	want.Wallpaper, want.CursorShadow = true, false
	if host.ColorDepth != 16 || host.desktopEffects() != want {
		t.Fatalf("saved host: depth %d, effects %+v, want 16 and %+v", host.ColorDepth, host.desktopEffects(), want)
	}

	// Editing it shows the same choices, and changing nothing keeps the host's.
	state.openEditHostID(host.ID)
	if state.editor.colorDepth != 16 || state.editor.effects != want {
		t.Fatalf("editor: depth %d, effects %+v", state.editor.colorDepth, state.editor.effects)
	}
	if got := state.editor.host(host.ID); got.ColorDepth != 16 || got.desktopEffects() != want {
		t.Fatalf("host from the editor: %+v", got)
	}
}

// The quality of a connection is the host's: a change of any of them is a
// change a live session must not be reused across.
func TestDesktopQualityIsPartOfTheReusePolicy(t *testing.T) {
	base := Host{Protocol: protocolVNC, Address: "h", Port: 5900}
	for name, changed := range map[string]Host{
		"depth":    {Protocol: protocolVNC, Address: "h", Port: 5900, ColorDepth: 16},
		"encoding": {Protocol: protocolVNC, Address: "h", Port: 5900, PreferredEncoding: desktop.EncodingRaw},
		"effects":  {Protocol: protocolVNC, Address: "h", Port: 5900, Effects: &desktop.Effects{Wallpaper: true}},
	} {
		if reusePolicyFor(base) == reusePolicyFor(changed) {
			t.Errorf("a changed %s leaves the reuse policy as it was", name)
		}
	}
}

// qualityTracker is a desktop session that takes a quality change without a new
// connection, as VNC does, and records it.
type qualityTracker struct {
	*pictureOnly
	depth    int
	encoding desktop.Encoding
	calls    int
}

func (q *qualityTracker) SetQuality(depth int, encoding desktop.Encoding) error {
	q.depth, q.encoding = depth, encoding
	q.calls++
	return nil
}

func openedDesktopSession(t *testing.T, host Host, session desktop.Session) (*State, *Session) {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	state.dialDesktop = func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		return session, nil
	}
	addStreamHost(state, host)
	openHost(state)
	opened, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool {
		return opened.desk != nil && opened.desk.session != nil && opened.phase == sshconn.PhaseConnected
	})
	return state, opened
}

// A VNC session takes the choice at once and saves it with its host, and no
// question is asked: nothing has to be connected again.
func TestDesktopQualityMenuAdjustsARunningVNCSession(t *testing.T) {
	tracker := &qualityTracker{pictureOnly: newPictureOnly()}
	state, session := openedDesktopSession(t, Host{Label: "Box", Address: "box.example.com", Protocol: protocolVNC}, tracker)
	state.setDesktopQuality(session, func(host *Host) { host.ColorDepth = 16; host.PreferredEncoding = desktop.EncodingRaw })
	if tracker.calls != 1 || tracker.depth != 16 || tracker.encoding != desktop.EncodingRaw {
		t.Fatalf("session was asked for %d bits and %q (%d calls)", tracker.depth, tracker.encoding, tracker.calls)
	}
	if state.modal != modalNone {
		t.Fatalf("a running VNC session was asked to reconnect (modal %d)", state.modal)
	}
	saved, _ := state.hostByID(session.HostID)
	if saved.ColorDepth != 16 || saved.PreferredEncoding != desktop.EncodingRaw {
		t.Fatalf("saved host = %+v", saved)
	}
	// Choosing what is already in effect changes nothing.
	state.setDesktopQuality(session, func(host *Host) { host.ColorDepth = 16 })
	if tracker.calls != 1 {
		t.Fatalf("an unchanged choice reached the server again (%d calls)", tracker.calls)
	}
}

// RDP fixes depth and effects in its handshake: the choice is saved and the
// user is asked to connect again.
func TestDesktopQualityMenuAsksAnRDPSessionToReconnect(t *testing.T) {
	state, session := openedDesktopSession(t, Host{Label: "Win", Address: "win.example.com", Protocol: protocolRDP}, newPictureOnly())
	state.setDesktopQuality(session, func(host *Host) { host.ColorDepth = 16 })
	if state.modal != modalSessionLink || state.sessionLink.disconnect || state.sessionLink.session != session.ID {
		t.Fatalf("modal %d, link %+v: want the reconnect question for this session", state.modal, state.sessionLink)
	}
	saved, _ := state.hostByID(session.HostID)
	if saved.ColorDepth != 16 {
		t.Fatalf("saved host = %+v", saved)
	}
}
