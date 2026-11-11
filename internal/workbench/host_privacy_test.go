package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

const (
	privacyTestName       = "SENSITIVE-NAME-471"
	privacyTestAddress    = "secret-471.example.test"
	privacyTestUser       = "hidden-user-471"
	privacyTestVNCAddress = "10.0.0.9"
)

func hostPrivacyStateForTest(t *testing.T) (*State, Options) {
	t.Helper()
	_, options := newWorld(t)
	if err := saveState(options.StatePath, savedState{
		Hosts: []savedHost{{
			Label: privacyTestName, Address: privacyTestAddress, User: privacyTestUser, Port: 2222,
		}},
		HostPrivacy: true,
	}); err != nil {
		t.Fatal(err)
	}
	return newTestState(t, options), options
}

func hostPrivacyVNCStateForTest(t *testing.T, viaSSH bool) *State {
	t.Helper()
	_, options := newWorld(t)
	if err := saveState(options.StatePath, savedState{
		Hosts: []savedHost{{
			Label: privacyTestName, Address: privacyTestVNCAddress, User: privacyTestUser, Port: defaultVNCPort,
			savedTransport: savedTransport{Protocol: "vnc", ViaSSH: viaSSH},
		}},
		HostPrivacy: true, HostPrivacyShowName: true, HostPrivacyShowAddress: true,
	}); err != nil {
		t.Fatal(err)
	}
	return newTestState(t, options)
}

func hostPrivacyCanvas(t *testing.T) gfx.Canvas {
	t.Helper()
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine is unavailable")
	}
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	return canvas
}

func assertHostSecretsAbsent(t *testing.T, canvas *gfx.Canvas, values ...string) {
	t.Helper()
	for _, text := range canvas.Texts() {
		for _, value := range values {
			if strings.Contains(text.Value, value) {
				t.Fatalf("canvas text leaked %q in %q", value, text.Value)
			}
		}
	}
	for _, hit := range canvas.Hits() {
		for _, value := range values {
			if strings.Contains(hit.Label, value) || strings.Contains(hit.Hint, value) {
				t.Fatalf("semantic hit leaked %q: %+v", value, hit)
			}
		}
	}
}

func canvasContainsText(canvas *gfx.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, value) {
			return true
		}
	}
	return false
}

func TestHostPrivacyDoesNotPaintSensitiveValuesInListOrCards(t *testing.T) {
	state, _ := hostPrivacyStateForTest(t)
	size := gfx.Size{Width: 1400, Height: 900}

	for _, view := range []hostViewMode{hostViewList, hostViewGrid} {
		state.hostView = view
		canvas := hostPrivacyCanvas(t)
		state.Paint(&canvas, size)
		assertHostSecretsAbsent(t, &canvas, privacyTestName, privacyTestAddress, privacyTestUser)
		privacyImages := 0
		for _, image := range canvas.Images() {
			if image.Bitmap.ID > hostPrivacyBitmapIDBase {
				privacyImages++
			}
		}
		wantPrivacyImages := 3
		if view == hostViewGrid {
			wantPrivacyImages = 2 // name + combined "user · address"
		}
		if privacyImages < wantPrivacyImages {
			t.Fatalf("view %v painted %d privacy bitmaps, want at least %d", view, privacyImages, wantPrivacyImages)
		}
		hiddenSemantic := false
		for _, hit := range canvas.Hits() {
			if hit.Label == state.messages.Tools.HostPrivacyHiddenHost {
				hiddenSemantic = true
				break
			}
		}
		if !hiddenSemantic {
			t.Fatalf("view %v has no hidden host semantic label", view)
		}
	}
}

func TestHostPrivacyMasksVNCUsernamesWithoutHidingAddresses(t *testing.T) {
	for _, viaSSH := range []bool{false, true} {
		state := hostPrivacyVNCStateForTest(t, viaSSH)
		for _, view := range []hostViewMode{hostViewList, hostViewGrid} {
			state.hostView = view
			canvas := hostPrivacyCanvas(t)
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
			assertHostSecretsAbsent(t, &canvas, privacyTestUser)
			if !canvasContainsText(&canvas, privacyTestVNCAddress) {
				t.Fatalf("viaSSH=%v view=%v hid the address with username-only privacy", viaSSH, view)
			}
		}
	}
}

func TestHostPrivacyMasksVNCTunnelAddressWithoutHidingUsername(t *testing.T) {
	state := hostPrivacyVNCStateForTest(t, true)
	state.hostPrivacy.maskUser = false
	state.hostPrivacy.maskAddress = true
	for _, view := range []hostViewMode{hostViewList, hostViewGrid} {
		state.hostView = view
		canvas := hostPrivacyCanvas(t)
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		assertHostSecretsAbsent(t, &canvas, privacyTestVNCAddress)
		if !canvasContainsText(&canvas, privacyTestUser) {
			t.Fatalf("view %v hid the username with address-only privacy", view)
		}
	}
}

func TestHostPrivacyVisibleFieldsKeepTheirNormalFit(t *testing.T) {
	_, options := newWorld(t)
	name := "NAME-" + strings.Repeat("n", 240)
	user := "USER-" + strings.Repeat("u", 240)
	address := "address-" + strings.Repeat("a", 240) + ".example"
	if err := saveState(options.StatePath, savedState{
		Hosts:       []savedHost{{Label: name, Address: address, User: user, Port: 2222}},
		HostPrivacy: true, HostPrivacyShowName: true, HostPrivacyShowAddress: true, HostPrivacyShowUser: true,
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	for _, view := range []hostViewMode{hostViewList, hostViewGrid} {
		state.hostView = view
		canvas := hostPrivacyCanvas(t)
		state.Paint(&canvas, gfx.Size{Width: 900, Height: 700})
		for _, text := range canvas.Texts() {
			if text.Value == name || text.Value == user || strings.Contains(text.Value, address) {
				t.Fatalf("view %v painted an unfitted privacy-mode line %q", view, text.Value)
			}
		}
	}
}

func TestHostPrivacyBlurHasTransparentTailSpace(t *testing.T) {
	state, _ := hostPrivacyStateForTest(t)
	canvas := hostPrivacyCanvas(t)
	canvas.SetPixelScale(2)
	bounds := gfx.Rect{Width: 240, Height: 22}
	style := gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightSemibold, Color: state.pal.text}

	bitmap, width, height := state.hostPrivacyBlurBitmap(&canvas, privacyTestName, style, bounds)
	if len(bitmap.Pix) == 0 {
		t.Fatal("privacy blur bitmap is empty")
	}
	if width <= canvas.TextWidthStyle(privacyTestName, style) || height <= bounds.Height {
		t.Fatalf("blur bitmap logical size = %gx%g, want padding around text line", width, height)
	}
	for x := 0; x < bitmap.Width; x++ {
		if bitmap.Pix[x*4+3] != 0 || bitmap.Pix[(bitmap.Height-1)*bitmap.Stride+x*4+3] != 0 {
			t.Fatalf("blur reaches top/bottom bitmap edge at x=%d", x)
		}
	}
	for y := 0; y < bitmap.Height; y++ {
		if bitmap.Pix[y*bitmap.Stride+3] != 0 || bitmap.Pix[y*bitmap.Stride+(bitmap.Width-1)*4+3] != 0 {
			t.Fatalf("blur reaches left/right bitmap edge at y=%d", y)
		}
	}
}

func TestHostPrivacyAlsoHidesSortHandleSemanticLabel(t *testing.T) {
	state, _ := hostPrivacyStateForTest(t)
	state.hostSort.enabled = true
	canvas := hostPrivacyCanvas(t)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	handle, ok := hitByID(&canvas, hitHostSortHandles.At(int(state.hosts[0].ID)))
	if !ok {
		t.Fatal("host sort handle is not painted")
	}
	if handle.Label != state.messages.Tools.HostPrivacyHiddenHost {
		t.Fatalf("sort handle semantic label = %q, want privacy label", handle.Label)
	}
}

func TestHostPrivacyButtonSitsToTheRightOfHostSort(t *testing.T) {
	state, _ := hostPrivacyStateForTest(t)
	canvas := hostPrivacyCanvas(t)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	sortHit, sortOK := hitByID(&canvas, hitHostSort)
	privacyHit, privacyOK := hitByID(&canvas, hitHostPrivacy)
	if !sortOK || !privacyOK {
		t.Fatalf("sort/privacy hits found = %v/%v", sortOK, privacyOK)
	}
	if privacyHit.Bounds.X <= sortHit.Bounds.X+sortHit.Bounds.Width {
		t.Fatalf("privacy button %v is not to the right of sort button %v", privacyHit.Bounds, sortHit.Bounds)
	}
}

func TestHostPrivacyFieldChoicesPersistIndependently(t *testing.T) {
	state, options := hostPrivacyStateForTest(t)
	size := gfx.Size{Width: 1400, Height: 900}

	canvas := hostPrivacyCanvas(t)
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitHostPrivacy); !ok {
		t.Fatal("privacy button is not painted")
	}
	state.Click(hitHostPrivacy)
	if !state.hostPrivacy.menu {
		t.Fatal("privacy menu did not open")
	}
	canvas = hostPrivacyCanvas(t)
	state.Paint(&canvas, size)
	if state.popups.owner != hitHostPrivacyDismiss {
		t.Fatalf("privacy menu scroll owner = %v, want %v", state.popups.owner, hitHostPrivacyDismiss)
	}
	for _, id := range []gfx.ID{hitHostPrivacyEnabled, hitHostPrivacyName, hitHostPrivacyAddress, hitHostPrivacyUser} {
		hit, ok := hitByID(&canvas, id)
		if !ok || !hit.Checked {
			t.Fatalf("privacy menu item %v = %+v, found %v", id, hit, ok)
		}
	}

	state.Click(hitHostPrivacyName)
	if state.hostPrivacy.maskName || !state.hostPrivacy.maskAddress || !state.hostPrivacy.maskUser {
		t.Fatalf("field choices = %+v", state.hostPrivacy)
	}

	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.HostPrivacy || !saved.HostPrivacyShowName || saved.HostPrivacyShowAddress || saved.HostPrivacyShowUser {
		t.Fatalf("saved privacy fields = enabled %v showName %v showAddress %v showUser %v",
			saved.HostPrivacy, saved.HostPrivacyShowName, saved.HostPrivacyShowAddress, saved.HostPrivacyShowUser)
	}

	restored := newTestState(t, options)
	if !restored.hostPrivacy.enabled || restored.hostPrivacy.maskName || !restored.hostPrivacy.maskAddress || !restored.hostPrivacy.maskUser {
		t.Fatalf("restored privacy = %+v", restored.hostPrivacy)
	}
	canvas = hostPrivacyCanvas(t)
	restored.Paint(&canvas, size)
	if !canvasContainsText(&canvas, privacyTestName) {
		t.Fatal("unmasked host name is not visible")
	}
	assertHostSecretsAbsent(t, &canvas, privacyTestAddress, privacyTestUser)
}
