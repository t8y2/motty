package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// fillHostForm types a host into the new-host dialog and submits it, without
// judging the result: the caller decides whether the form should have taken it.
func fillHostForm(state *State, label, group string) {
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, label)
	typeInto(state, hitDialogAddress, "bastion.example.com")
	typeInto(state, hitDialogUser, "ops")
	typeInto(state, hitDialogGroup, group)
	state.Click(hitDialogSubmit)
}

// A host is filed under a group, and the same server may be worth keeping in
// two of them under its own name: the user put it in both on purpose, and the
// two rows are told apart by the group they sit in. Only a repeat inside one
// group is the accidental duplicate the form refuses.
func TestASecondGroupMayHoldTheSameHostName(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = state.hosts[:0]

	fillHostForm(state, "Production", "Servers")
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("first host: modal=%d error=%q", state.modal, state.dialogError)
	}
	fillHostForm(state, "Production", "Lab")
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("the same name in a second group was refused: modal=%d error=%q", state.modal, state.dialogError)
	}
	if len(state.hosts) != 2 {
		t.Fatalf("hosts = %+v, want the same server in two groups", state.hosts)
	}
	if !sameEndpoint(state.hosts[0], state.hosts[1]) {
		t.Fatalf("the two rows are not one destination: %+v", state.hosts)
	}
	if state.hosts[0].Group != "Servers" || state.hosts[1].Group != "Lab" {
		t.Fatalf("groups = %q, %q", state.hosts[0].Group, state.hosts[1].Group)
	}
	if state.hosts[0].CatalogID == state.hosts[1].CatalogID {
		t.Fatalf("both copies carry one catalog ID: %q", state.hosts[0].CatalogID)
	}

	// The same name in the group it is already in is still the accidental
	// repeat the form exists to catch.
	fillHostForm(state, "production", "Lab")
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrAddressTaken {
		t.Fatalf("a repeat inside one group was accepted: modal=%d error=%q", state.modal, state.dialogError)
	}
	state.Click(hitDialogCancel)
}

// The two rows are the host as the user filed it, so the fold that reconciles a
// host saved on two devices must leave them alone: folding them would drop the
// copy the form just allowed on the next start.
func TestAHostInTwoGroupsSurvivesARestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.hosts = state.hosts[:0]
	fillHostForm(state, "Production", "Servers")
	fillHostForm(state, "Production", "Lab")
	if len(state.hosts) != 2 {
		t.Fatalf("hosts = %+v, want two", state.hosts)
	}
	restarted := newTestState(t, options)
	if len(restarted.hosts) != 2 {
		t.Fatalf("the host kept in two groups came back as %d rows: %+v", len(restarted.hosts), restarted.hosts)
	}
	if !sameEndpoint(restarted.hosts[0], restarted.hosts[1]) {
		t.Fatalf("the restored rows are not one destination: %+v", restarted.hosts)
	}
	groups := []string{restarted.hosts[0].Group, restarted.hosts[1].Group}
	if groups[0] == groups[1] {
		t.Fatalf("both rows came back in one group: %v", groups)
	}
}

// Every way a host changes group has to keep the one-group rule the form keeps:
// a move that would put a second copy of the host in a group is refused instead
// of making a row the next start would fold away.
func TestAGroupMoveDoesNotMakeADuplicate(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = state.hosts[:0]
	fillHostForm(state, "Production", "")
	fillHostForm(state, "Production", "Lab")
	if len(state.hosts) != 2 {
		t.Fatalf("hosts = %+v, want two", state.hosts)
	}
	root, lab := state.hosts[0], state.hosts[1]

	// A drop on another group.
	if state.moveHostToGroup(lab.ID, "") {
		t.Fatal("a host was dropped into a group that already holds it")
	}
	if state.status != state.messages.ErrAddressTaken || state.hosts[1].Group != "Lab" {
		t.Fatalf("the refused drop: status=%q group=%q", state.status, state.hosts[1].Group)
	}

	// A drop beside a host in that group.
	if state.reorderHostBeside(lab.ID, root.ID, false) {
		t.Fatal("a host was dropped beside one in a group that already holds it")
	}
	if state.status != state.messages.ErrAddressTaken || state.hosts[1].Group != "Lab" {
		t.Fatalf("the refused drop beside: status=%q group=%q", state.status, state.hosts[1].Group)
	}

	// "Move to group" over a selection.
	state.hostActionIDs = []uint32{lab.ID}
	state.hostGroupCopy = false
	state.applyHostGroup("")
	if state.status != state.messages.ErrAddressTaken || state.hosts[1].Group != "Lab" {
		t.Fatalf("the refused move: status=%q group=%q", state.status, state.hosts[1].Group)
	}

	// Deleting a group moves its hosts into the parent, where this one already
	// has a copy.
	if state.deleteHostGroup("Lab", false) {
		t.Fatal("a group was deleted onto a host the parent already holds")
	}
	if !strings.Contains(state.dialogError, "Production") {
		t.Fatalf("the refused delete: error=%q", state.dialogError)
	}
	if state.hosts[1].Group != "Lab" {
		t.Fatalf("the refused delete still moved the host: %q", state.hosts[1].Group)
	}
}

// The selected hosts move together, so a move the group cannot take in full has
// to leave every one of them where it was: moving the rest would report the same
// refusal over a group that did change, and which hosts ended up moved would
// depend on the order they were selected in.
func TestAGroupMoveMovesNothingWhenOneHostWouldRepeat(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = state.hosts[:0]
	fillHostForm(state, "Production", "")
	fillHostForm(state, "Box", "")
	fillHostForm(state, "Production", "Lab")
	if len(state.hosts) != 3 {
		t.Fatalf("hosts = %+v, want three", state.hosts)
	}
	state.hostActionIDs = []uint32{state.hosts[0].ID, state.hosts[1].ID}
	state.hostGroupCopy = false
	state.applyHostGroup("Lab")
	if state.status != state.messages.ErrAddressTaken {
		t.Fatalf("the refused move: status=%q", state.status)
	}
	if state.hosts[0].Group != "" || state.hosts[1].Group != "" {
		t.Fatalf("the refused move still moved part of the selection: %q, %q", state.hosts[0].Group, state.hosts[1].Group)
	}
}

// The "Move to group" dialog asks for a group name, so a refusal has to come
// back into that field: closing the dialog would drop the name the user typed
// and leave the refusal in a status line the dialog covers.
func TestAMoveDialogKeepsTheGroupWhenTheMoveIsRefused(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = state.hosts[:0]
	fillHostForm(state, "Production", "")
	fillHostForm(state, "Production", "Lab")
	state.hostActionIDs = []uint32{state.hosts[0].ID}
	state.hostGroupCopy = false
	state.groupActions.op = hostGroupMoveHosts
	state.dialogError = ""
	state.modal = modalMoveHostGroup

	submitHostGroupName(state, "Lab")
	if state.modal != modalMoveHostGroup {
		t.Fatalf("the refused move closed the dialog: modal=%d", state.modal)
	}
	if state.dialogError != state.messages.ErrAddressTaken {
		t.Fatalf("the refused move: error=%q", state.dialogError)
	}
	if state.hosts[0].Group != "" {
		t.Fatalf("the refused move still moved the host: %q", state.hosts[0].Group)
	}
}

// A deletion the parent group cannot take leaves the dialog open, so the reason
// has to be painted in it: the status line sits behind the dialog's scrim.
func TestDeletingAGroupSaysWhyItDidNotHappen(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = state.hosts[:0]
	fillHostForm(state, "Production", "")
	fillHostForm(state, "Production", "Lab")
	chooseHostGroupAction(t, state, "Lab", "delete")
	if state.modal != modalMoveHostGroup {
		t.Fatal("delete did not ask for confirmation")
	}
	state.Click(hitMoveGroupSubmit)
	if state.modal != modalMoveHostGroup {
		t.Fatalf("the refused deletion closed the dialog: modal=%d", state.modal)
	}
	// The refusal names the host and the group it already sits in, so the user
	// has a next step: the same server cannot be deleted into a group that
	// already holds it until this copy is renamed or removed.
	if !strings.Contains(state.dialogError, "Production") || !strings.Contains(state.dialogError, state.messages.HostGroupDefault) {
		t.Fatalf("the refusal did not name the host and where it exists: %q", state.dialogError)
	}
	canvas, texts := paintAt(state, gfx.Size{Width: 1280, Height: 800})
	var said *gfx.Text
	for at, text := range canvas.Texts() {
		if strings.Contains(text.Value, "Production") {
			said = &canvas.Texts()[at]
		}
	}
	if said == nil {
		t.Fatalf("the dialog did not say why the group stayed: %q", texts)
	}
	// The reason sits above the choices, not on top of them: the body is a
	// column, so the error line takes its own row and the radios keep theirs.
	first, ok := canvas.HitBounds(hitGroupDeleteParent)
	if !ok {
		t.Fatal("the dialog's first choice is not painted")
	}
	if said.Bounds.Y+said.Bounds.Height > first.Y {
		t.Fatalf("the reason overlaps the choices: reason=%+v first choice=%+v", said.Bounds, first)
	}
	// The two choices move into different groups, so the reason that named one of
	// them must not linger after the user switches to the other. The radio's own
	// press sets ungroup; the id it also dispatches has to clear the reason.
	state.groupActions.ungroup = true
	state.clickMoveHostGroupDialog(hitGroupDeleteUngroup)
	if state.dialogError != "" {
		t.Fatalf("the reason survived a switch of choice: %q", state.dialogError)
	}
}

// A group name typed with spaces around the separators names the same folder as
// the folded form the next start reads back: the move must store the folded
// form, or the host would sit in one group now and jump to another on restart.
func TestAGroupMoveStoresTheFoldedName(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.hosts = state.hosts[:0]
	fillHostForm(state, "Production", "")
	state.hostActionIDs = []uint32{state.hosts[0].ID}
	state.hostGroupCopy = false
	if !state.applyHostGroup("Team / Prod") {
		t.Fatalf("the move was refused: status=%q", state.status)
	}
	if got := state.hosts[0].Group; got != "Team/Prod" {
		t.Fatalf("stored group = %q, want the folded form", got)
	}
	restarted := newTestState(t, options)
	if len(restarted.hosts) != 1 || restarted.hosts[0].Group != "Team/Prod" {
		t.Fatalf("after a restart: %+v", restarted.hosts)
	}
}
