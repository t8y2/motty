package workbench

import "testing"

// TestFontThickening walks the setting the way a user does: it starts off, reaches
// the platform when turned on, survives a save and a reload, and the row is hidden
// where the platform answers that it cannot draw a thicker stroke.
func TestFontThickening(t *testing.T) {
	var applied []bool
	options := testOptions(t)
	options.SetFontThickening = func(on bool) bool {
		applied = append(applied, on)
		return true
	}
	state := newTestState(t, options)
	if state.fontThickening || !state.fontThickeningSupported {
		t.Fatalf("default: thicken %v, row shown %v; want off and shown", state.fontThickening, state.fontThickeningSupported)
	}
	if len(applied) != 1 || applied[0] {
		t.Fatalf("startup applied %v, want the default once", applied)
	}
	if !state.setFontThickening(true) {
		t.Fatal("turning thickening on reported no change")
	}
	if last := applied[len(applied)-1]; !last {
		t.Fatalf("turning thickening on applied %v, want on", applied)
	}
	state.save()

	reloaded := newTestState(t, options)
	if !reloaded.fontThickening {
		t.Fatal("reloaded thickening is off, want on")
	}
	if last := applied[len(applied)-1]; !last {
		t.Fatalf("reloaded state applied %v, want on", applied)
	}

	// A reload hands the stored value to restoreShared, which draws it at once
	// rather than at the next start.
	restored := newTestState(t, options)
	restored.restoreShared(savedState{})
	if restored.fontThickening {
		t.Fatal("a restored weight was not kept")
	}
	if last := applied[len(applied)-1]; last {
		t.Fatalf("a restored weight applied %v, want off", applied)
	}

	unsupported := testOptions(t)
	unsupported.SetFontThickening = func(bool) bool { return false }
	if hidden := newTestState(t, unsupported); hidden.fontThickeningSupported {
		t.Fatal("the row is shown though the platform cannot draw a thicker stroke")
	}
}
