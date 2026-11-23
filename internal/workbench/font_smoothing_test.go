package workbench

import "testing"

// TestFontSmoothing walks the setting the way a user does: it starts on, reaches the
// platform when turned off, survives a save and a reload, and the row is hidden where
// the platform answers that it has no such choice.
func TestFontSmoothing(t *testing.T) {
	var applied []bool
	options := testOptions(t)
	options.SetFontSmoothing = func(on bool) bool {
		applied = append(applied, on)
		return true
	}
	state := newTestState(t, options)
	if state.fontSmoothingOff || !state.fontSmoothingSupported {
		t.Fatalf("default: smoothing off %v, row shown %v; want on and shown", state.fontSmoothingOff, state.fontSmoothingSupported)
	}
	if len(applied) == 0 || !applied[len(applied)-1] {
		t.Fatalf("startup applied %v, want the default on", applied)
	}
	if !state.setFontSmoothing(false) {
		t.Fatal("turning smoothing off reported no change")
	}
	if last := applied[len(applied)-1]; last {
		t.Fatalf("turning smoothing off applied %v, want off", applied)
	}
	state.save()

	reloaded := newTestState(t, options)
	if !reloaded.fontSmoothingOff {
		t.Fatal("reloaded smoothing is on, want off")
	}
	if last := applied[len(applied)-1]; last {
		t.Fatalf("reloaded state applied %v, want off", applied)
	}

	// A reload hands the stored value to restoreShared, which draws it at once
	// rather than at the next start; a profile that never chose keeps the default.
	restored := newTestState(t, options)
	restored.fontSmoothingOff = true
	restored.restoreShared(savedState{})
	if restored.fontSmoothingOff {
		t.Fatal("a profile with no saved choice did not return to smoothing on")
	}
	if last := applied[len(applied)-1]; !last {
		t.Fatalf("a restored default applied %v, want on", applied)
	}

	unsupported := testOptions(t)
	unsupported.SetFontSmoothing = func(bool) bool { return false }
	if hidden := newTestState(t, unsupported); hidden.fontSmoothingSupported {
		t.Fatal("the row is shown though the platform has no font smoothing choice")
	}
}
