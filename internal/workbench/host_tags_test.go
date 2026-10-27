package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestHostTagSelectPresetsExistingTagsAndPersistence(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.hosts[0].Tags = "数据库, production, 数据库"
	state.openEditHostID(state.hosts[0].ID)
	p := state.hostTagPicker()
	choices := p.control.Choices()
	if len(choices) != 5 || choices[0].Value != "开发" || choices[1].Value != "测试" || choices[2].Value != "生产" {
		t.Fatal("presets or existing tags missing", choices)
	}
	state.Click(hitHostTagOptions.At(0))
	state.Click(hitHostTagOptions.At(2))
	typeInto(state, hitDialogTags, "上海机房")
	state.Key(ui.KeyEvent{Target: hitDialogTags, Key: ui.KeyEnter})
	if state.modal != modalNewHost || !slices.Equal(p.control.Values(), []string{"数据库", "production", "开发", "生产", "上海机房"}) {
		t.Fatal("tag Enter submitted form or lost multi-selection", p.control.Values())
	}
	state.Click(hitHostTagRemove.At(0))
	state.Click(hitHostTagsDismiss)
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.hosts[0].Tags != "production, 开发, 生产, 上海机房" {
		t.Fatal("tag save failed", state.hosts[0].Tags, state.dialogError)
	}
	restored := newTestState(t, options)
	if restored.hosts[0].Tags != state.hosts[0].Tags {
		t.Fatal("tag persistence changed")
	}
	state.openCopyHost()
	if p := state.hostTagPicker(); !slices.Equal(p.control.Values(), []string{"production", "开发", "生产", "上海机房"}) {
		t.Fatal("copy did not preload chips", p.control.Values())
	}
}

func TestHostTagSelectCommitsPendingQueryAndKeepsValidation(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitAddHost)
	state.editor.label.Insert("tagged")
	state.editor.address.Insert("192.0.2.42")
	typeInto(state, hitDialogTags, strings.Repeat("长", 33))
	state.Key(ui.KeyEvent{Target: hitDialogTags, Key: ui.KeyEnter})
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrHostTagsInvalid || len(state.hostTagPicker().control.Values()) != 0 {
		t.Fatal("invalid create bypassed validation")
	}
	p := state.hostTagPicker()
	p.control.Query = text.Editor{}
	p.control.Query.Insert("生产")
	state.Click(hitDialogSubmit) // no Enter: the pending query must still be saved
	if state.modal != modalNone || len(state.hosts) != 1 || state.hosts[0].Tags != "生产" {
		t.Fatal("save dropped pending query", state.dialogError)
	}
	state.openEditHostID(state.hosts[0].ID)
	p = state.hostTagPicker()
	values := make([]string, maxHostTags)
	for i := range values {
		values[i] = string(rune('a' + i))
	}
	p.control.SetValues(values)
	state.syncHostTags()
	typeInto(state, hitDialogTags, "extra")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrHostTagsInvalid {
		t.Fatal("selection limit bypassed on save")
	}
}
