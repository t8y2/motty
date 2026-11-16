package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"

	"motty/internal/i18n"
)

// The Host tags group of Settings lists every tag the hosts use and the built-in
// suggestions, so a tag that is no longer wanted can be renamed or taken off
// every host at once instead of host by host. A built-in suggestion that is
// deleted is remembered by its key, the same in every language, and stays out of
// the host form's list until it is restored.

var hostTagSettingsHits = tree.Feature("host-tag-settings")

var (
	hitHostTagRenameList  = tree.Sub(hostTagSettingsHits, tree.Name("rename")) // + tag index
	hitHostTagDeleteList  = tree.Sub(hostTagSettingsHits, tree.Name("delete")) // + tag index
	hitHostTagRestoreHits = tree.ID(hostTagSettingsHits, "restore")
)

// builtinHostTag is a tag the host form suggests before any host has one.
type builtinHostTag struct {
	key                     string
	simplified, traditional string
}

var builtinHostTags = [...]builtinHostTag{
	{key: "dev", simplified: "开发", traditional: "開發"},
	{key: "test", simplified: "测试", traditional: "測試"},
	{key: "prod", simplified: "生产", traditional: "生產"},
}

func (tag builtinHostTag) name(language i18n.Language) string {
	if language == i18n.TraditionalChinese {
		return tag.traditional
	}
	return tag.simplified
}

// normalizeHiddenHostTags keeps the known built-in keys, once each, in their
// own order, so a saved list from another build reads the same everywhere.
func normalizeHiddenHostTags(keys []string) []string {
	var kept []string
	for _, tag := range builtinHostTags {
		if slices.Contains(keys, tag.key) {
			kept = append(kept, tag.key)
		}
	}
	return kept
}

// hostTagEntry is one tag of the inventory: its name as shown, how many hosts
// carry it, and the key of the built-in suggestion it is, if it is one.
type hostTagEntry struct {
	name    string
	hosts   int
	builtin string
}

// hostTagInventory lists the built-in suggestions still offered, then every
// other tag in the order the hosts first use it. Tags differing only in case are
// one tag, as they are in the host form.
func (state *State) hostTagInventory() []hostTagEntry {
	var entries []hostTagEntry
	find := func(name string) int {
		return slices.IndexFunc(entries, func(entry hostTagEntry) bool { return strings.EqualFold(entry.name, name) })
	}
	for _, tag := range builtinHostTags {
		if !slices.Contains(state.hiddenHostTags, tag.key) {
			entries = append(entries, hostTagEntry{name: tag.name(state.language), builtin: tag.key})
		}
	}
	for _, host := range state.hosts {
		tags, _ := parseHostTags(host.Tags)
		for _, tag := range tags {
			if index := find(tag); index >= 0 {
				entries[index].hosts++
				continue
			}
			entries = append(entries, hostTagEntry{name: tag, hosts: 1, builtin: state.builtinHostTagKey(tag)})
		}
	}
	return entries
}

// builtinHostTagKey is the key of the built-in suggestion named name in this
// language, or "".
func (state *State) builtinHostTagKey(name string) string {
	for _, tag := range builtinHostTags {
		if strings.EqualFold(tag.name(state.language), name) {
			return tag.key
		}
	}
	return ""
}

// hideBuiltinHostTag stops suggesting the built-in tag with that key.
func (state *State) hideBuiltinHostTag(key string) {
	if key != "" && !slices.Contains(state.hiddenHostTags, key) {
		state.hiddenHostTags = normalizeHiddenHostTags(append(state.hiddenHostTags, key))
	}
}

// retagHosts gives every host carrying the tag name the tag to instead, or takes
// it off when to is empty, and reports how many hosts changed. A host that
// already has to keeps one of it.
func (state *State) retagHosts(name, to string) int {
	count := 0
	for index := range state.hosts {
		host := &state.hosts[index]
		tags, _ := parseHostTags(host.Tags)
		at := slices.IndexFunc(tags, func(tag string) bool { return strings.EqualFold(tag, name) })
		if at < 0 {
			continue
		}
		if to == "" {
			tags = slices.Delete(tags, at, at+1)
		} else {
			tags[at] = to
		}
		if normalized, ok := normalizeHostTags(tags); ok {
			tags = normalized
		}
		setHostTags(host, tags)
		count++
	}
	return count
}

// renameHostTag renames the tag on every host. It returns the text of an error
// to show in the form, or "" when it is done.
func (state *State) renameHostTag(name, to string) string {
	to = strings.TrimSpace(to)
	if !validHostTag(to) {
		return state.messages.ErrHostTagsInvalid
	}
	if to == name {
		return ""
	}
	count := state.retagHosts(name, to)
	// The suggestion now has another name: offering the old one beside it
	// would bring back what was just renamed away.
	if !strings.EqualFold(name, to) {
		state.hideBuiltinHostTag(state.builtinHostTagKey(name))
	}
	if count > 0 {
		state.hostDataChanged()
	}
	state.status = fmt.Sprintf(state.messages.HostTags.StatusRenamedFmt, to, count)
	state.save()
	return ""
}

// deleteHostTag takes the tag off every host and stops suggesting it.
func (state *State) deleteHostTag(name string) {
	count := state.retagHosts(name, "")
	state.hideBuiltinHostTag(state.builtinHostTagKey(name))
	if count > 0 {
		state.hostDataChanged()
	}
	state.status = fmt.Sprintf(state.messages.HostTags.StatusDeletedFmt, name)
	state.save()
}

func (state *State) restoreBuiltinHostTags() {
	if len(state.hiddenHostTags) == 0 {
		return
	}
	state.hiddenHostTags = nil
	state.status = state.messages.HostTags.StatusRestored
	state.save()
}

func (state *State) openRenameHostTagForm(name string) {
	m := state.messages.HostTags
	form := state.openForm(formDialog{
		title: m.RenameTitle, description: fmt.Sprintf(m.RenameFmt, name), submit: m.Save,
		run: func(state *State, form *formDialog) string {
			return state.renameHostTag(name, form.value(0))
		},
	}, formField{label: m.RenameLabel, placeholder: name})
	form.set(0, name)
}

func (state *State) askDeleteHostTag(entry hostTagEntry) {
	state.ruleDelete = ruleDelete{kind: ruleDeleteHostTag, name: entry.name, uses: entry.hosts}
	state.modal = modalDeleteRule
}

// clickHostTagSettings handles the group's buttons. An index is looked up again
// in the inventory as it is now, so a press always acts on the row it was on.
func (state *State) clickHostTagSettings(id gfx.ID) bool {
	switch {
	case id == hitHostTagRestoreHits:
		state.restoreBuiltinHostTags()
	case id.Scope() == hitHostTagRenameList, id.Scope() == hitHostTagDeleteList:
		entries := state.hostTagInventory()
		index := item(id)
		if index < 0 || index >= len(entries) {
			return true
		}
		if id.Scope() == hitHostTagRenameList {
			if entries[index].hosts > 0 {
				state.openRenameHostTagForm(entries[index].name)
			}
		} else {
			state.askDeleteHostTag(entries[index])
		}
	default:
		return false
	}
	return true
}

// paintHostTagSettings is the Host tags group of Settings.
func (state *State) paintHostTagSettings(page *settingsPage) {
	m := state.messages.HostTags
	entries := state.hostTagInventory()
	page.group(m.Group, func() {
		if len(entries) == 0 {
			page.actionRow(m.Empty, m.EmptyDescription, true)
		}
		for index, entry := range entries {
			uses := m.UsedByNone
			if entry.hosts > 0 {
				uses = fmt.Sprintf(m.UsedByFmt, entry.hosts)
			}
			if entry.builtin != "" {
				uses = m.BuiltIn + " · " + uses
			}
			// A suggestion no host has has nothing to rename: the new name would
			// be on no host, and the suggestion would be gone.
			page.actionRow(entry.name, uses, false,
				components.ButtonAction{ID: hitHostTagRenameList.At(index), Label: m.Rename, Variant: components.ButtonOutline, Disabled: entry.hosts == 0},
				components.ButtonAction{ID: hitHostTagDeleteList.At(index), Label: m.Delete, Variant: components.ButtonOutline})
		}
		if len(state.hiddenHostTags) > 0 {
			names := make([]string, 0, len(state.hiddenHostTags))
			for _, tag := range builtinHostTags {
				if slices.Contains(state.hiddenHostTags, tag.key) {
					names = append(names, tag.name(state.language))
				}
			}
			page.actionRow(m.RestoreRow, fmt.Sprintf(m.RestoreFmt, strings.Join(names, ", ")), false,
				components.ButtonAction{ID: hitHostTagRestoreHits, Label: m.Restore, Variant: components.ButtonOutline})
		}
	})
}
