package workbench

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"motty/internal/desktop"
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
