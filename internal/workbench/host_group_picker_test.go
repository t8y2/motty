package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestHostGroupPickerSelectsCreatesClearsAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.hosts[0].Group = "已有分组"
	state.refreshHostGroups()
	state.openEditHostID(state.hosts[0].ID)
	p := state.hostGroupPicker()
	if p.control.Value() != "已有分组" || len(p.control.Choices()) < 1 {
		t.Fatal("existing group not preloaded", p.control.Choices())
	}
	typeInto(state, hitDialogGroup, "上海机房")
	state.Key(gfx.KeyEvent{Target: hitDialogGroup, Key: gfx.KeyEnter})
	if state.modal != modalNewHost || p.control.Open || state.editor.value(fieldGroup) != "上海机房" {
		t.Fatal("Enter submitted the form or did not select", state.editor.value(fieldGroup))
	}
	state.Click(hitDialogGroup)
	for i, choice := range p.control.Choices() {
		if choice.Value == "已有分组" {
			state.Click(hitHostGroupChoices.At(i))
			break
		}
	}
	if state.editor.value(fieldGroup) != "已有分组" || p.control.Open {
		t.Fatal("single selection did not replace")
	}
	state.Click(hitHostGroupClear)
	if state.editor.value(fieldGroup) != "" || p.control.Value() != "" {
		t.Fatal("group was not cleared")
	}
	typeInto(state, hitDialogGroup, " 保存时创建 ")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.hosts[0].Group != "保存时创建" {
		t.Fatal("pending group was not committed", state.dialogError, state.hosts[0].Group)
	}
	restored := newTestState(t, options)
	if restored.hosts[0].Group != "保存时创建" {
		t.Fatal("group was not persisted")
	}
	state.openCopyHost()
	if state.hostGroupPicker().control.Value() != "保存时创建" {
		t.Fatal("copy lost its source group")
	}
}

func TestHostGroupPickerCancelDoesNotCreateCatalogEntries(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitAddHost)
	typeInto(state, hitDialogGroup, "不保存的分组")
	state.Key(gfx.KeyEvent{Target: hitDialogGroup, Key: gfx.KeyEnter})
	state.Click(hitDialogCancel)
	if len(state.hosts) != 0 || state.namedHostGroups() != 0 {
		t.Fatal("canceled group leaked into catalog")
	}
	state.Click(hitAddHost)
	if p := state.hostGroupPicker(); p.control.Value() != "" || len(p.control.Choices()) != 0 {
		t.Fatal("new form retained canceled choice")
	}
}

func TestHostGroupPickerInvalidQueryKeepsTheExistingGroup(t *testing.T) {
	for _, query := range []string{"bad\nname", "bad\rname", "bad\x00name"} {
		state := newTestState(t, testOptions(t))
		previous := state.hosts[0].Group
		state.openEditHostID(state.hosts[0].ID)
		typeInto(state, hitDialogGroup, query)
		state.Click(hitDialogSubmit)
		if state.modal != modalNewHost || state.dialogError != state.messages.ErrHostGroupInvalid || state.editor.value(fieldGroup) != previous || state.hosts[0].Group != previous {
			t.Fatal("invalid query changed the saved group or bypassed validation")
		}
	}
}

func BenchmarkPaintHostGroupDialog(b *testing.B) {
	for _, name := range []string{"new", "edit"} {
		b.Run(name, func(b *testing.B) {
			state := newTestState(b, testOptions(b))
			if name == "new" {
				state.Click(hitAddHost)
			} else {
				state.openEditHostID(state.hosts[0].ID)
			}
			var canvas gfx.Canvas
			size := gfx.Size{Width: 1400, Height: 900}
			state.Paint(&canvas, size)
			b.ReportAllocs()
			for b.Loop() {
				canvas.Reset()
				state.Paint(&canvas, size)
			}
		})
	}
}

// Enter with nothing typed means the form's Enter: it saves. The picker must
// close its list without choosing the first group, and the form must submit.
func TestEnterInAnEmptyGroupFieldSavesTheHost(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "box")
	typeInto(state, hitDialogAddress, "10.0.0.1")
	state.Click(hitDialogGroup)
	state.Key(gfx.KeyEvent{Target: hitDialogGroup, Key: gfx.KeyEnter})
	if modal := state.modal; modal != modalNewHost || state.hostGroupPicker().control.Open {
		t.Fatalf("first Enter left modal=%v open=%v", modal, state.hostGroupPicker().control.Open)
	}
	state.Key(gfx.KeyEvent{Target: hitDialogGroup, Key: gfx.KeyEnter})
	if state.modal != modalNone || len(state.hosts) != 1 || state.hosts[0].Group != "" {
		t.Fatalf("Enter on an empty group did not save: modal=%v hosts=%d group=%q error=%q",
			state.modal, len(state.hosts), state.hosts[0].Group, state.dialogError)
	}
}

func TestHostGroupPickerMatchesAnExistingGroupIgnoringCase(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Group = "Servers"
	state.refreshHostGroups()
	state.openEditHostID(state.hosts[0].ID)
	typeInto(state, hitDialogGroup, "servers")
	state.Key(gfx.KeyEvent{Target: hitDialogGroup, Key: gfx.KeyEnter})
	if got := state.hostGroupPicker().control.Value(); got != "Servers" {
		t.Fatalf("case-different query created %q, want the existing Servers", got)
	}
	state.Click(hitDialogSubmit)
	matching := 0
	for _, group := range state.hostGroups {
		if strings.EqualFold(group.Name, "Servers") {
			matching++
		}
	}
	if state.hosts[0].Group != "Servers" || matching != 1 {
		t.Fatalf("duplicate groups: group=%q matching=%d", state.hosts[0].Group, matching)
	}
}
