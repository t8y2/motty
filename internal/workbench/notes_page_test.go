package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// noteTreeRowHit finds the tree row of the note or folder id, as the page
// declared it: the rows carry the page's identities, not the application's.
func noteTreeRowHit(c *gfx.Canvas, n *notesUI, id string) (gfx.Hit, bool) {
	rows := n.view.treeScratch.rows
	for i := range rows {
		if rows[i].id != id {
			continue
		}
		hit, ok := hitByID(c, n.page.tree.RowID(i))
		if ok {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// clickNoteTree selects the tree's row named label as the page delivers it.
func clickNoteTree(t testing.TB, s *State, label string) {
	t.Helper()
	clickPageLabel(t, s, gfx.RoleButton, label)
}

// clickNoteTreeRow clicks the tree's row of a note or folder id, in the
// event pass of a paint, where the page delivers its choice.
func clickNoteTreeRow(t testing.TB, s *State, n *notesUI, id string) {
	t.Helper()
	size := s.viewport
	if size.Width == 0 {
		size = gfx.Size{Width: 1400, Height: 900}
	}
	var canvas gfx.Canvas
	s.Paint(&canvas, size)
	row := -1
	for i, entry := range n.view.treeScratch.rows {
		if entry.id == id {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatalf("no tree row for %q", id)
	}
	hit, ok := hitByID(&canvas, n.page.tree.RowID(row))
	if !ok {
		t.Fatalf("tree row %q is not painted", id)
	}
	canvas = gfx.Canvas{}
	canvas.BeginEventPass(hit.ID, gfx.ClickInfo{})
	s.Paint(&canvas, size)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
}

// hitByIDRoleLabel finds the hit of an identity with a role and label.
func hitByIDRoleLabel(c *gfx.Canvas, id gfx.ID, role gfx.Role, label string) (gfx.Hit, bool) {
	for _, hit := range c.Hits() {
		if hit.ID == id && hit.Role == role && hit.Label == label {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// hasSeparator reports whether the page paints the splitter named label.
func hasSeparator(c *gfx.Canvas, label string) bool {
	for _, hit := range c.Hits() {
		if (hit.Role == gfx.RoleSeparator || hit.Role == gfx.RoleSlider) && hit.Label == label {
			return true
		}
	}
	return false
}

// separatorHit finds the splitter named label.
func separatorHit(t testing.TB, c *gfx.Canvas, label string) gfx.Hit {
	t.Helper()
	for _, hit := range c.Hits() {
		if (hit.Role == gfx.RoleSeparator || hit.Role == gfx.RoleSlider) && hit.Label == label {
			return hit
		}
	}
	t.Fatalf("no separator named %q", label)
	return gfx.Hit{}
}

// noteTreeRowLabel is the name the tree shows for a note or folder id.
func noteTreeRowLabel(n *notesUI, id string) string {
	for _, row := range n.view.treeScratch.rows {
		if row.id == id {
			return row.label
		}
	}
	return ""
}
