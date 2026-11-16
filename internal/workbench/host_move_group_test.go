package workbench

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func hostMoveGroupChoice(t *testing.T, state *State, value string) gfx.ID {
	t.Helper()
	canvas, _ := paintedTexts(state)
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitHostGroupItemList && hit.Value == value && (hit.Role == gfx.RoleOption || hit.Role == gfx.RoleMenuItem) {
			return hit.ID
		}
	}
	t.Fatalf("no visible move-group choice for %q", value)
	return gfx.ID{}
}

func TestMoveHostGroupWithNoNamedDestinationsKeepsItsCommands(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for index := range state.hosts {
		state.hosts[index].Group = ""
	}
	state.hostDataChanged()
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.Click(hitMoveHostGroup)
	canvas, _ := paintedTexts(state)
	for _, id := range []gfx.ID{hitHostGroupNone, hitHostGroupNew} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("empty catalog lost %s", tree.Text(id))
		}
	}
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitHostGroupItemList {
			t.Fatalf("unnamed bucket became an extra choice: %+v", hit)
		}
	}
	before := append([]Host(nil), state.hosts...)
	state.Click(hitHostGroupNew)
	if state.modal != modalMoveHostGroup || state.hostGroupEditor.String() != "" || !reflect.DeepEqual(state.hosts, before) {
		t.Fatal("New group modified hosts before submission")
	}
	state.Click(hitMoveGroupCancel)
	if state.modal != modalNone || !reflect.DeepEqual(state.hosts, before) {
		t.Fatal("cancelling New group modified hosts")
	}
}

func BenchmarkPaintHostMoveGroupMenu(b *testing.B) {
	state := newTestState(b, testOptions(b))
	for index := 0; index < 50; index++ {
		state.hostGroups = append(state.hostGroups, hostGroup{Name: fmt.Sprintf("Group %02d/Branch/Leaf", index), Explicit: true})
	}
	state.hostDataChanged()
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.Click(hitMoveHostGroup)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1200, Height: 800}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}
