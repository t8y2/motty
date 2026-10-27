package workbench

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	hostTagsHits       = tree.Feature("host-tags")
	hitHostTagsTrigger = tree.ID(hostTagsHits, "trigger")
	hitHostTagsDismiss = tree.ID(hostTagsHits, "dismiss")
	hitHostTagsScroll  = tree.ID(hostTagsHits, "scroll")
	hostTagsScope      = tree.ID(hostTagsHits, "scope")
	hitHostTagOptions  = tree.Sub(hostTagsHits, tree.Name("option"))
	hitHostTagRemove   = tree.Sub(hostTagsHits, tree.Name("remove"))
)

type hostTagPicker struct {
	control    components.MultiSelectState
	serialized string
	ready      bool
	popup      ui.Rect
}

func validHostTag(value string) bool {
	tags, ok := parseHostTags(value)
	return ok && len(tags) == 1 && tags[0] == strings.TrimSpace(value) && !strings.ContainsAny(value, ",\r\n")
}

func (state *State) hostTagPicker() *hostTagPicker {
	p := &state.editor.tagPicker
	value := state.editor.value(fieldTags)
	if !p.ready {
		p.control.AllowCreate, p.control.FoldValues, p.control.Limit = true, true, maxHostTags
		p.control.ValidateCreate = validHostTag
		options := []components.MultiSelectOption{{Value: "开发"}, {Value: "测试"}, {Value: "生产"}}
		for _, host := range state.hosts {
			tags, _ := parseHostTags(host.Tags)
			for _, tag := range tags {
				if !slices.ContainsFunc(options, func(o components.MultiSelectOption) bool { return strings.EqualFold(o.Value, tag) }) {
					options = append(options, components.MultiSelectOption{Value: tag})
				}
			}
		}
		p.control.SetOptions(options)
		p.ready = true
		p.serialized = "\x00"
	}
	if p.serialized != value {
		tags, _ := parseHostTags(value)
		p.control.SetValues(tags)
		p.serialized = value
	}
	return p
}

func (state *State) syncHostTags() {
	p := &state.editor.tagPicker
	value := strings.Join(p.control.Values(), ", ")
	if value != p.serialized {
		state.editor.tags = text.Editor{}
		state.editor.tags.Insert(value)
		p.serialized = value
	}
	if p.control.Query.String() == "" && state.dialogError == state.messages.ErrHostTagsInvalid {
		state.dialogError = ""
	}
}

// Saving a host also commits a valid query that has not yet been confirmed.
func (state *State) commitHostTagQuery() bool {
	p := state.hostTagPicker()
	query := strings.TrimSpace(p.control.Query.String())
	if query == "" {
		return true
	}
	if !validHostTag(query) {
		state.dialogError = state.messages.ErrHostTagsInvalid
		return false
	}
	values := append([]string(nil), p.control.Values()...)
	values = append(values, query)
	tags, ok := normalizeHostTags(values)
	if !ok {
		state.dialogError = state.messages.ErrHostTagsInvalid
		return false
	}
	p.control.SetValues(tags)
	p.control.Query = text.Editor{}
	state.syncHostTags()
	return true
}

func (state *State) clickHostTags(id ui.ID) bool {
	p := state.hostTagPicker()
	switch {
	case id == hitDialogTags:
		p.control.Open = true
	case id == hitHostTagsTrigger:
		p.control.Open = !p.control.Open
	case id == hitHostTagsDismiss:
		p.control.Open = false
	case id.Scope() == hitHostTagOptions:
		p.control.Choose(item(id))
		state.syncHostTags()
	case id.Scope() == hitHostTagRemove:
		p.control.Remove(item(id))
		state.syncHostTags()
	default:
		return false
	}
	return true
}

func (state *State) hostTagsKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalNewHost {
		return ui.EventResult{}, false
	}
	p := state.hostTagPicker()
	if event.Target != hitDialogTags && (!p.control.Open || event.Key != ui.KeyEscape && event.Key != ui.KeyTab) {
		return ui.EventResult{}, false
	}
	if event.Key == ui.KeyEnter && !event.Release && !event.Repeat && event.Modifiers == 0 {
		if _, composing := p.control.Query.Composition(); !composing {
			if query := strings.TrimSpace(p.control.Query.String()); query != "" && !validHostTag(query) {
				state.dialogError = state.messages.ErrHostTagsInvalid
				return ui.EventResult{Handled: true, Repaint: true}, true
			}
		}
	}
	result := p.control.Key(event)
	state.syncHostTags()
	return result, result.Handled || result.Repaint
}

func (state *State) paintHostTags(canvas *ui.Canvas, bounds ui.Rect) {
	p := state.hostTagPicker()
	input := components.BeginField(canvas, state.Theme(), bounds).Paint(state.messages.FieldTags, "", "")
	query := strings.TrimSpace(p.control.Query.String())
	layout := components.PaintMultiSelect(canvas, state.Theme(), input, &p.control, components.MultiSelectOptions{
		InputID: hitDialogTags, TriggerID: hitHostTagsTrigger, DismissID: hitHostTagsDismiss, Scope: hostTagsScope, ScrollID: hitHostTagsScroll,
		OptionIDs: hitHostTagOptions, RemoveIDs: hitHostTagRemove, Label: state.messages.FieldTags,
		Placeholder: state.messages.FieldTagsPlaceholder, EmptyLabel: state.messages.HostTagsEmpty, CreatePrefix: state.messages.HostTagsCreatePrefix,
		Viewport: ui.Rect{Width: state.viewport.Width, Height: state.viewport.Height}, Layer: dialogLayer + 2,
		Invalid: query != "" && !validHostTag(query),
	})
	p.popup = layout.Popup
}
