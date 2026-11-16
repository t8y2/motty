package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"motty/internal/i18n"
)

func hostTagNames(state *State) []string {
	var names []string
	for _, entry := range state.hostTagInventory() {
		names = append(names, entry.name)
	}
	return names
}

func pickerTagNames(state *State) []string {
	state.openEditHostID(state.hosts[0].ID)
	var names []string
	for _, choice := range state.hostTagPicker().control.Choices() {
		names = append(names, choice.Value)
	}
	state.closeModal()
	return names
}

func TestHostTagInventoryCountsEachTagOnceAcrossCase(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Tags = "A1, 开发, db"
	host := state.hosts[0]
	host.ID, host.Tags = host.ID+1000, "a1, web"
	state.hosts = append(state.hosts, host)
	entries := state.hostTagInventory()
	want := []hostTagEntry{
		{name: "开发", hosts: 1, builtin: "dev"}, {name: "测试", builtin: "test"}, {name: "生产", builtin: "prod"},
		{name: "A1", hosts: 2}, {name: "db", hosts: 1}, {name: "web", hosts: 1},
	}
	if !slices.Equal(entries, want) {
		t.Fatalf("inventory = %v, want %v", entries, want)
	}
}

// Deleting a tag takes it off every host and out of the host form's
// suggestions, and a deleted built-in suggestion stays gone after a restart.
func TestDeletingAHostTagThroughSettings(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.hosts[0].Tags = "A1, keep, 开发"
	state.save()
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3000})
	a1 := slices.Index(hostTagNames(state), "A1")
	if a1 < 0 || !hasHit(&canvas, hitHostTagDeleteList.At(a1)) {
		t.Fatalf("the tag has no delete button: %v", hostTagNames(state))
	}
	state.Click(hitHostTagDeleteList.At(a1))
	if state.modal != modalDeleteRule {
		t.Fatalf("modal = %v, want the confirmation", state.modal)
	}
	state.Click(hitRuleDeleteConfirm)
	if state.hosts[0].Tags != "keep, 开发" {
		t.Fatalf("tags = %q", state.hosts[0].Tags)
	}
	state.Click(hitHostTagDeleteList.At(slices.Index(hostTagNames(state), "开发")))
	state.Click(hitRuleDeleteConfirm)
	state.Click(hitHostTagDeleteList.At(slices.Index(hostTagNames(state), "测试")))
	state.Click(hitRuleDeleteConfirm)
	if state.hosts[0].Tags != "keep" || !slices.Equal(state.hiddenHostTags, []string{"dev", "test"}) {
		t.Fatalf("tags = %q, hidden = %v", state.hosts[0].Tags, state.hiddenHostTags)
	}
	restarted := newTestState(t, options)
	if got := pickerTagNames(restarted); !slices.Equal(got, []string{"生产", "keep"}) {
		t.Fatalf("after a restart the host form suggests %v", got)
	}
	restarted.Click(hitSettings)
	restarted.selectSettingsCategory(settingsConnection)
	restarted.Paint(&canvas, gfx.Size{Width: 1400, Height: 3000})
	if !hasHit(&canvas, hitHostTagRestoreHits) {
		t.Fatal("there is no way to bring the suggestions back")
	}
	restarted.Click(hitHostTagRestoreHits)
	if got := pickerTagNames(restarted); !slices.Equal(got, []string{"开发", "测试", "生产", "keep"}) {
		t.Fatalf("after restoring the host form suggests %v", got)
	}
}

func TestCancellingATagDeleteKeepsIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Tags = "A1"
	state.Click(hitHostTagDeleteList.At(slices.Index(hostTagNames(state), "A1")))
	state.Click(hitRuleDeleteCancel)
	if state.hosts[0].Tags != "A1" || len(state.hiddenHostTags) != 0 {
		t.Fatalf("tags = %q, hidden = %v", state.hosts[0].Tags, state.hiddenHostTags)
	}
}

func TestRenamingAHostTagMergesAndValidates(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.hosts[0].Tags = "A1, B2, 开发"
	state.Click(hitHostTagRenameList.At(slices.Index(hostTagNames(state), "A1")))
	if state.modal != modalForm || state.form.value(0) != "A1" {
		t.Fatalf("modal = %v, field = %q", state.modal, state.form.value(0))
	}
	state.form.set(0, "a,b")
	if message := state.form.run(state, &state.form); message != state.messages.ErrHostTagsInvalid {
		t.Fatalf("an invalid name was taken: %q", message)
	}
	// Renaming onto a tag the host has merges the two.
	state.form.set(0, " b2 ")
	if message := state.form.run(state, &state.form); message != "" {
		t.Fatalf("form refused: %q", message)
	}
	if state.hosts[0].Tags != "b2, 开发" {
		t.Fatalf("tags = %q", state.hosts[0].Tags)
	}
	// A renamed built-in suggestion is not offered under its old name.
	if message := state.renameHostTag("开发", "dev"); message != "" {
		t.Fatal(message)
	}
	if state.hosts[0].Tags != "b2, dev" || !slices.Equal(state.hiddenHostTags, []string{"dev"}) {
		t.Fatalf("tags = %q, hidden = %v", state.hosts[0].Tags, state.hiddenHostTags)
	}
	if got := pickerTagNames(newTestState(t, options)); !slices.Equal(got, []string{"测试", "生产", "b2", "dev"}) {
		t.Fatalf("after a restart the host form suggests %v", got)
	}
}

// A suggestion no host has has nothing to rename.
func TestAnUnusedSuggestionCannotBeRenamed(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Tags = ""
	state.Click(hitHostTagRenameList.At(0))
	if state.modal == modalForm || len(state.hiddenHostTags) != 0 {
		t.Fatal("an unused suggestion was renamed away")
	}
}

func TestHiddenHostTagsKeepOnlyKnownKeysInOrder(t *testing.T) {
	if got := normalizeHiddenHostTags([]string{"prod", "x", "dev", "prod"}); !slices.Equal(got, []string{"dev", "prod"}) {
		t.Fatalf("got %v", got)
	}
}

// The built-in suggestions are named in the interface's Chinese, and one
// deleted in one language stays deleted in another.
func TestBuiltinHostTagsFollowTheLanguage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Tags = ""
	state.hiddenHostTags = []string{"test"}
	state.language = i18n.TraditionalChinese
	if got := hostTagNames(state); !slices.Equal(got, []string{"開發", "生產"}) {
		t.Fatalf("got %v", got)
	}
}
