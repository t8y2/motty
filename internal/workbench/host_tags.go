package workbench

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"motty/internal/i18n"
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
	popup      gfx.Rect
}

func validHostTag(value string) bool {
	tags, ok := parseHostTags(value)
	return ok && len(tags) == 1 && tags[0] == strings.TrimSpace(value) && !strings.ContainsAny(value, ",\r\n")
}

func (state *State) hostTagPicker() *hostTagPicker {
	p := &state.editor.tagPicker
	value := state.editor.value(fieldTags)
	if !p.ready {
		p.control.AllowCreate, p.control.MatchCaseInsensitive, p.control.Limit = true, true, maxHostTags
		p.control.SetValidateCreate(validHostTag)
		options := []components.MultiSelectOption{{Value: "开发"}, {Value: "测试"}, {Value: "生产"}}
		if state.language == i18n.TraditionalChinese {
			options = []components.MultiSelectOption{{Value: "開發"}, {Value: "測試"}, {Value: "生產"}}
		}
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

// pendingHostTags splits a query that lists several tags, so text copied from
// an old comma-separated field, a README or a shell command still saves as
// tags. Empty entries, as a trailing comma leaves, are ignored.
func pendingHostTags(query string) ([]string, bool) {
	var tags []string
	for _, part := range strings.Split(query, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !validHostTag(part) {
			return nil, false
		}
		tags = append(tags, part)
	}
	return tags, true
}

// Saving a host also commits a valid query that has not yet been confirmed.
func (state *State) commitHostTagQuery() bool {
	p := state.hostTagPicker()
	query := strings.TrimSpace(p.control.Query.String())
	if query == "" {
		return true
	}
	pending, ok := pendingHostTags(query)
	if !ok {
		state.dialogError = state.messages.ErrHostTagsInvalid
		return false
	}
	values := append([]string(nil), p.control.Values()...)
	values = append(values, pending...)
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

func (state *State) clickHostTags(id gfx.ID) bool {
	p := state.hostTagPicker()
	switch {
	case id == hitDialogTags:
		p.control.Open = true
		state.editor.groupPicker.control.Open = false
	case id == hitHostTagsTrigger:
		p.control.Open = !p.control.Open
		state.editor.groupPicker.control.Open = false
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

func (state *State) hostTagsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNewHost {
		return gfx.EventResult{}, false
	}
	p := state.hostTagPicker()
	if event.Target != hitDialogTags && (!p.control.Open || event.Key != gfx.KeyEscape && event.Key != gfx.KeyTab) {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEnter && !event.Release && !event.Repeat && event.Modifiers == 0 {
		if _, composing := p.control.Query.Composition(); !composing {
			if query := strings.TrimSpace(p.control.Query.String()); query != "" {
				if _, ok := pendingHostTags(query); !ok {
					state.dialogError = state.messages.ErrHostTagsInvalid
					return gfx.EventResult{Handled: true, Repaint: true}, true
				}
				// A comma-separated query is several tags, and the component's
				// create row is disabled for it, so commit the list here.
				if strings.Contains(query, ",") {
					state.commitHostTagQuery()
					return gfx.EventResult{Handled: true, Repaint: true}, true
				}
			}
		}
	}
	result := p.control.Key(event)
	state.syncHostTags()
	return result, result.Handled || result.Repaint
}

func (state *State) paintHostTags(canvas *gfx.Canvas, bounds gfx.Rect) {
	p := state.hostTagPicker()
	input := components.BeginField(canvas, state.Theme(), bounds).For(hitDialogTags).Paint(state.messages.FieldTags, "", "")
	query := strings.TrimSpace(p.control.Query.String())
	layout := components.PaintMultiSelect(canvas, state.Theme(), input, &p.control, components.MultiSelectOptions{
		InputID: hitDialogTags, TriggerID: hitHostTagsTrigger, DismissID: hitHostTagsDismiss, Scope: hostTagsScope, ScrollID: hitHostTagsScroll,
		OptionIDs: hitHostTagOptions, RemoveIDs: hitHostTagRemove, Label: state.messages.FieldTags,
		Placeholder: state.messages.FieldTagsPlaceholder, EmptyLabel: state.messages.HostTagsEmpty, CreatePrefix: state.messages.HostTagsCreatePrefix,
		// The popup may cover the dialog footer, while the input stays clipped to
		// the scrollable form. Viva bounds the floating list to this window.
		Viewport: gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}, Layer: dialogLayer + 2,
		Invalid: query != "" && !validHostTag(query),
	})
	p.popup = layout.Popup
	// The form's tab ring is built from registered stops while it is clipped.
	// Each chip is a focusable remove button painted by the component, so take
	// its bounds from the hits it just declared and keep it in that order.
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitHostTagRemove {
			state.rememberHostControl(hit.ID, hit.Bounds)
		}
	}
}
