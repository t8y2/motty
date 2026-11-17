package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestZZIssue902Repro(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	driver := sftpPathFocusDriver(t, state)
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	index := sftpRowIndex(t, view, "docs")
	hit := hitOf(t, driver, sftpListRowID(t, view.active(), index))
	if err := driver.DoubleClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	focused := driver.Snapshot().Focused
	t.Logf("after entering: focused=%v listScope=%v path=%q", focused, view.active().listRowsScope, view.path)
	t.Logf("onListRow=%v selected=%d", view.active().onListRow(focused), view.active().selected)
	if err := driver.Key(gfx.KeyLeft, gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	t.Logf("after Alt+Left: path=%q busy=%v history=%d cursor=%d", view.path, view.busy, len(view.history.entries), view.history.cursor)
	if view.path != "/home/test" {
		t.Fatalf("Alt+Left did not go back; focused=%v", driver.Snapshot().Focused)
	}
}
