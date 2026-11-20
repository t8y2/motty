//go:build windows || darwin || linux

package workbench

import (
	"testing"
)

// TestSFTPLocationProfileQuickMenuOpensOnFirstClickAndAppliesRow covers the
// pointer path that used to need a second click: a menu that only opened after
// a hover preview had been dismissed. A click on the chevron now opens the menu
// under it immediately, and a row opens its paired folders.
func TestSFTPLocationProfileQuickMenuOpensOnFirstClickAndAppliesRow(t *testing.T) {
	state, w, _, host, directory := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	profiles := []sftpBookmark{
		{host: w.session.host.CatalogID, name: "Profile one", local: directory, path: "/home/test"},
		{host: w.session.host.CatalogID, name: "Profile two", local: directory, path: "/home/test/docs"},
	}
	state.sftpBookmarks = profiles
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	trigger := dualHit(t, view, hitSFTPProfileQuick)
	x, y := trigger.X+trigger.Width/2, trigger.Y+trigger.Height/2
	if err := view.Move(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	if !w.ui.profileQuickOpen {
		t.Fatal("the first click on the chevron did not open the location profile menu")
	}

	// The menu hangs off the trigger: below it and starting within its width,
	// not at the page corner where a missing anchor put it.
	row, ok := snapshotMenuHit(view, profileQuickLabel(profiles[1]))
	if !ok {
		t.Fatal("the open menu did not paint the second profile")
	}
	if row.Bounds.Y < trigger.Y+trigger.Height {
		t.Fatalf("menu row at y=%g is above the trigger's bottom %g", row.Bounds.Y, trigger.Y+trigger.Height)
	}
	if row.Bounds.X < trigger.X || row.Bounds.X-trigger.X > trigger.Width {
		t.Fatalf("menu row at x=%g does not hang off the trigger at x=%g", row.Bounds.X, trigger.X)
	}

	// The row is chosen by a real press, not only by the window's own dispatch:
	// a page menu closes before it delivers the chosen row.
	if err := view.Click(row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	dualWait(t, state, w, view, func() bool {
		return !w.navigationBusy() && w.view.lfolder.path == directory && w.view.path == "/home/test/docs"
	})
	if w.ui.profileQuickOpen {
		t.Fatal("choosing a profile left its menu open")
	}
}

// TestSFTPPaneMoreMenuOpensUnderItsButton pins the same anchor contract for the
// pane's More menu: a page control anchors its menu from the box it is laid out
// at, not from a rectangle read before layout.
func TestSFTPPaneMoreMenuOpensUnderItsButton(t *testing.T) {
	_, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	more := dualHit(t, view, sftpWindowMore[0])
	if err := view.Click(more.X+more.Width/2, more.Y+more.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	if !w.folder(true).headerMenuOpen {
		t.Fatal("More did not open its menu")
	}
	row, ok := snapshotMenuHit(view, w.headerMenuRowLabel(0, sftpWindowHistoryBack[0]))
	if !ok {
		t.Fatal("the More menu did not paint its first row")
	}
	// The menu is wider than the room right of the button, so the page clamps it
	// inside the viewport horizontally; only the side relative to the button is
	// stable across fonts and path lengths.
	if row.Bounds.Y < more.Y+more.Height {
		t.Fatalf("More's first row at y=%g is above the button's bottom %g", row.Bounds.Y, more.Y+more.Height)
	}
}
