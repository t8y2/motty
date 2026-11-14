package workbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"motty/internal/ai/store"
	"motty/internal/buildinfo"
	"motty/internal/configstore"
	layoutcore "motty/internal/workspace"
)

// The configuration lives in a configstore as records: one per host, per
// snippet, per setting and so on. This file turns a savedState into those
// records and back. A record is what two devices compare when they sync, so its
// key must name the same thing on every device:
//
//   - a host is its catalog id, which is made from where it connects, so two
//     devices that each added the same server end up with one host;
//   - a snippet, a highlight, a forward, a proxy route, a bookmark and a group are
//     what identifies them (a name, a pattern, the ports and the host);
//   - an autofill rule has a random id of its own;
//   - a setting is its key in the state.
//
// Everything the user did not choose for all their devices (the window, the open
// tabs, the language and look, the fonts, what this computer does at start) is
// kept as one value of this device and never synced.

// configFormat is the minimum reader/writer compatibility epoch, NOT the
// version of the program that last saved. Since epoch 23, optional object fields
// survive older writers. Only incompatible semantics or structures raise it.
// Arrays are atomic: extending their element schema requires a new epoch if
// older clients must still be able to edit them.
//
// Version 3 was assigned independently by two lines of work: one added the
// synchronized SFTP path input, the other the AI provider authentication style.
// Both payloads share the same number, so the merged build writes 4; otherwise
// either older format-3 build would accept the other's records and drop the
// field it does not know when it saves them.
//
// The AI providers mirrored into the state file are guarded by this number and
// by the ai.db schema, not by stateFileVersion: that constant is written but
// never checked, and a build that cannot open a newer ai.db omits the mirror
// until the next build rebuilds it from the database.
// Version 5 preserves device-local toolbar layouts: an older full snapshot
// writer would otherwise discard the visibility and ordering preferences.
// Version 6 preserves the device-local shell executable choice.
// Version 7 preserves local folder favorites and named paired SFTP profiles.
// Version 8 preserves independent monitor visibility and panel width.
// Version 9 keeps a host's password and one-time code secret under the host, not
// under its login: an older build would look for them where they no longer are.
// Version 9 was also used by monitoring builds for device-local panel definitions.
// Version 10 preserves both changes so neither older build can overwrite them.
// Version 11 preserves the device-local session-log capture switch: the field
// lives in the local state object, and an older build's first (full) save would
// rewrite that object from its typed struct and silently turn capture back on.
// Version 12 preserves the device-local default editor for the same reason: an
// older build's full save would drop the choice and the menu item with it.
// Version 13 preserves the device-local network proxy choice.
// Version 14 preserves the device-local tab orientation and auto-hide choice.
// Version 15 preserves the device-local vertical tab grouping switch.
// Version 16 preserves the interface font.
// Version 17 preserves the device-local terminal toolbar position.
// Version 18 preserves the device-local interface text size.
// Version 19 preserves the SFTP transfer layout and custom editor path: an
// older build's full save would otherwise drop these device-local fields.
// Version 20 preserves the host group explicit, pinned and order metadata.
// Version 21 preserves toolbar dock zones and mixed-context ordering.
// Version 22 preserves the device-local docked toolbar size.
// Version 23 requires lossless transactional field patches from every writer.
// Workspace templates travel as shared records; the layout they restore is a
// device-local object, which epoch 23 preserves without a version of its own.
// Version 24 preserves the SFTP location profile synchronization flag. Profiles
// are array records, so an older writer would otherwise drop the new field.
// Version 25 preserves an AI provider's models URL, on the same terms: providers
// are array records too.
// Version 26 adds the saved passwords and the hosts' reference to one: an older
// writer would drop the records it does not know.
// Version 27 preserves the latency readout's docked position and order: older
// writers would drop the new toolbar key and silently move it back.
// Version 28 preserves the device-local interface text size as a pixel number:
// an older full save would drop the field and reset the size.
const configFormat = 28

const (
	kindSetting             = "setting"
	kindWorkspaceTemplate   = "workspace-template"
	localKey                = "state"
	developmentWorkspaceKey = "development_workspace"
)

// savedWorkspace is the restore state that differs between release and
// development builds. Workspace identity travels with its deck; all other
// settings remain shared in local state.
type savedWorkspace struct {
	NoteSidebars     *savedNoteSidebars     `json:"note_sidebars,omitempty"`
	Workspace        string                 `json:"workspace,omitempty"`
	Sessions         []savedSession         `json:"sessions,omitempty"`
	ActiveSession    int                    `json:"active_session,omitempty"`
	Deck             *layoutcore.DeckDTO    `json:"deck,omitempty"`
	WorkspaceLayouts []savedWorkspaceLayout `json:"workspace_layouts,omitzero"`
}

func clearSavedWorkspace(saved *savedState) {
	saved.Workspace = ""
	saved.Sessions = nil
	saved.ActiveSession = 0
	saved.Deck = nil
	saved.WorkspaceLayouts = nil
	saved.NoteSidebars = nil
}

func applySavedWorkspace(saved *savedState, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("development workspace must be a JSON object")
	}
	var workspace savedWorkspace
	if err := json.Unmarshal(trimmed, &workspace); err != nil {
		return fmt.Errorf("development workspace: %w", err)
	}
	if workspace.Deck != nil {
		if err := workspace.Deck.Validate(); err != nil {
			return fmt.Errorf("development workspace: %w", err)
		}
		for _, entry := range workspace.Deck.Workspaces {
			for _, pane := range entry.Panes {
				if pane.SessionID != 0 && (pane.SessionID < 1 || int(pane.SessionID) > len(workspace.Sessions)) {
					return fmt.Errorf("development workspace: invalid session reference %d", pane.SessionID)
				}
			}
		}
	}
	saved.Workspace = workspace.Workspace
	saved.Sessions = workspace.Sessions
	saved.ActiveSession = workspace.ActiveSession
	saved.Deck = workspace.Deck
	saved.WorkspaceLayouts = workspace.WorkspaceLayouts
	saved.NoteSidebars = workspace.NoteSidebars
	return nil
}

// recordPayload is what a record holds. Ord is the place of the item in its
// list, assigned when the item first appears and kept after, so removing an item
// does not rewrite the ones after it.
type recordPayload struct {
	Ord  int64           `json:"o,omitempty"`
	Data json.RawMessage `json:"d"`
}

// entityField is a list in the state that is stored a record per item.
type entityField struct {
	field string // the JSON key in the state
	kind  string // the record kind
	// single is a field that holds one object, not a list.
	single bool
	// keepWhenAbsent keeps the records of a field the state does not carry at
	// all (not one that is empty): the state may simply not have read it yet.
	keepWhenAbsent bool
	// key names an item from its fields.
	key func(fields map[string]json.RawMessage) string
}

var entityFields = []entityField{
	{field: "notes", kind: "note", key: func(f map[string]json.RawMessage) string { return fieldText(f, "id") }},
	{field: "note_folders", kind: "note-folder", key: func(f map[string]json.RawMessage) string { return fieldText(f, "id") }},
	{field: "hosts", kind: "host", key: func(fields map[string]json.RawMessage) string {
		if id := fieldText(fields, "id"); id != "" {
			return id
		}
		return joinKey("anonymous", fieldText(fields, "label"), fieldText(fields, "address"), fieldText(fields, "user"), fieldText(fields, "port"))
	}},
	{field: "host_groups", kind: "group", key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "name") }},
	{field: "workspace_templates", kind: kindWorkspaceTemplate, key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "forwards", kind: "forward", key: func(fields map[string]json.RawMessage) string {
		return joinKey(fieldText(fields, "mode"), fieldText(fields, "host_address"), fieldText(fields, "host_user"), fieldText(fields, "host_port"),
			fieldText(fields, "local_port"), fieldText(fields, "destination_host"), fieldText(fields, "destination_port"))
	}},
	{field: "proxies", kind: "proxy", key: proxyRecordKey},
	{field: "snippets", kind: "snippet", key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "name") }},
	{field: "highlights", kind: "highlight", key: func(fields map[string]json.RawMessage) string {
		return joinKey(fieldText(fields, "pattern"), fieldText(fields, "regex"), fieldText(fields, "case_sensitive"))
	}},
	{field: "autofill_rules", kind: "autofill", key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "credentials", kind: "credential", key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "sftp_bookmarks", kind: "bookmark", key: func(fields map[string]json.RawMessage) string {
		if name := fieldText(fields, "name"); name != "" {
			return joinKey("profile", fieldText(fields, "host"), fieldText(fields, "shared"), name)
		}
		return joinKey(fieldText(fields, "host"), fieldText(fields, "path"))
	}},
	{field: "ai_providers", kind: kindAIProvider, keepWhenAbsent: true, key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "ai_cli_providers", kind: kindAICLIProvider, keepWhenAbsent: true, key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "outbound_proxy", kind: "outbound-proxy", single: true, key: func(map[string]json.RawMessage) string { return "main" }},
}

// proxyRecordKey names a route by its target: where the target connects, which is
// what every build has always done. When the target host is not the first at its
// destination (its catalog ID is not the one that destination gives) the route
// is named by that ID instead, so two targets behind one bastion do not share a
// record.
func proxyRecordKey(fields map[string]json.RawMessage) string {
	user, address, port := fieldText(fields, "target_user"), fieldText(fields, "target_address"), fieldText(fields, "target_port")
	if id := fieldText(fields, "target_id"); id != "" {
		number, _ := strconv.ParseUint(port, 10, 16)
		if id != catalogIDFor(Host{Protocol: protocolSSH, Address: address, User: user, Port: uint16(number)}) {
			return joinKey("host", id)
		}
	}
	return joinKey(user, address, port)
}

// syncedSettings are the scalar settings every device shares.
var syncedSettings = map[string]bool{
	"host_order":                   true,
	"hidden_host_tags":             true,
	"highlight_builtin":            true,
	"terminalOption":               true,
	"terminal_scrollback":          true,
	"connection_timeout_seconds":   true,
	"connection_keepalive_seconds": true,
	"connection_keepalive_off":     true,
	"connection_reconnect":         true,
	"terminal_clipboard":           true,
	"terminal_cursor_steady":       true,
	"terminal_cursor_thickness":    true,
	"terminal_copy_on_select":      true,
	"terminal_word_separators":     true,
	"terminal_right_click_paste":   true,
	"terminal_tab_title":           true, // read legacy shared preference
	"tab_title_mode":               true,
	"terminal_show_latency":        true,
	"connection_stats":             true,
	"terminal_bell_off":            true,
	"terminal_link_plain_click":    true,
	"terminal_input_click_off":     true,
	"terminal_scheme":              true,
	"terminal_scheme_dark":         true,
	"terminal_scheme_light":        true,
	"terminal_padding":             true,
	"terminal_gutter":              true,
	"terminal_intense":             true,
	"terminal_line_height":         true,
	"terminal_notifications":       true,
	"terminal_paste_image":         true,
	"terminal_password_hint":       true,
	"terminal_close_confirm":       true,
	"sftp_parallel":                true,
	"sftp_keep_times":              true,
	"sftp_follow":                  true,
	"sftp_terminal_follow":         true,
	"sftp_path_input":              true,
	"sftp_panel_auto_open":         true,
	"sftp_panel_side":              true,
	"command_history":              true,
	"command_completion_mode":      true,
	"command_suggest_off":          true,
	"command_inline_suggest_off":   true,
	"command_accept_hint_off":      true,
}

// stateVersionFields are written back as this build's own on every load.
var stateVersionFields = map[string]bool{"version": true, "host_catalog_version": true, "shortcut_version": true}

// windowStateFields are the saved fields one window owns: the tabs it has open,
// the deck they fill, the workspace it is in, and where it sits on screen. They
// live in that window's own local value rather than in this device's document,
// so two windows of one process never write each other's tabs.
var windowStateFields = map[string]bool{
	"sessions": true, "active_session": true, "deck": true, "note_sidebars": true,
	"workspace": true, "workspace_layouts": true,
	"window_x": true, "window_y": true, "window_width": true, "window_height": true,
}

// windowKey names the local value holding one window's own state. A development
// build keeps its windows apart from the ones an installed copy remembers, the
// way the rest of its data is kept apart.
func windowKey(id int, development bool) string {
	if development {
		return fmt.Sprintf("dev-window-%d", id)
	}
	return fmt.Sprintf("window-%d", id)
}

const collapsedGroupsKey = "collapsed_groups"

// linkedNotesKey is the device-local list of notes whose text lives in a file.
// They are notes in the state, but not records: nothing about them is shared. A
// build that does not know the field leaves it in the store, as epoch 23 requires
// of every device-local field, so it needs no format version of its own.
const linkedNotesKey = "linked_notes"

// linkedNoteFoldersKey is the same for the folders that mirror a directory.
const linkedNoteFoldersKey = "linked_note_folders"

// linkedLocalKey names the device-local field holding the linked items of a
// record kind, or "" for a kind that has none.
func linkedLocalKey(kind string) string {
	switch kind {
	case "note":
		return linkedNotesKey
	case "note-folder":
		return linkedNoteFoldersKey
	}
	return ""
}

func fieldText(fields map[string]json.RawMessage, name string) string {
	raw := fields[name]
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

// joinKey makes an item's key from the parts that identify it, shortened to what
// a record id may hold.
func joinKey(parts ...string) string { return boundKey(strings.Join(parts, "|")) }

// boundKey shortens a key to what a record id may hold, keeping it recognisable
// and, because of the hash, still different from every other long key.
func boundKey(key string) string {
	if len(key) <= 200 && !strings.ContainsRune(key, 0) {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	if len(key) > 100 {
		key = key[:100]
	}
	key = strings.ToValidUTF8(strings.ReplaceAll(key, "\x00", ""), "")
	return key + "~" + hex.EncodeToString(sum[:8])
}

// recordParses says whether a record's data is something this build can read: a
// known setting or list item of the right shape. A record of a kind or setting
// this build does not know is left alone, neither read nor deleted.
func recordParses(kind, id string, data json.RawMessage) (known, ok bool) {
	if kind == kindAIProvider || kind == kindAICLIProvider {
		var provider savedAIProvider
		if json.Unmarshal(data, &provider) != nil {
			return true, false
		}
		// Future provider subtypes are opaque too: importing and then omitting
		// them from a snapshot would otherwise turn a save into a deletion.
		if !store.ValidProviderKind(provider.Kind) || store.IsCLIKind(provider.Kind) != (kind == kindAICLIProvider) {
			return false, true
		}
	}
	var body []byte
	if kind == kindSetting {
		if !syncedSettings[id] {
			return false, true
		}
		name, _ := json.Marshal(id)
		body = append(append(append([]byte{'{'}, name...), ':'), data...)
		body = append(body, '}')
	} else {
		found := false
		for _, entity := range entityFields {
			if entity.kind != kind {
				continue
			}
			found = true
			if entity.single {
				body = []byte(`{"` + entity.field + `":` + string(data) + `}`)
			} else {
				body = []byte(`{"` + entity.field + `":[` + string(data) + `]}`)
			}
		}
		if !found {
			return false, true
		}
	}
	var probe savedState
	return true, json.Unmarshal(body, &probe) == nil
}

// holdableRecords are the received records this build can keep: tombstones, ones
// of kinds it does not know (they are stored and passed on), and known ones that
// have the shape it expects. The rest are counted as refused.
func holdableRecords(received []configstore.Record) (kept []configstore.Record, refused int) {
	for _, record := range received {
		if !record.Deleted {
			var payload recordPayload
			if err := json.Unmarshal(record.Payload, &payload); err != nil || len(payload.Data) == 0 {
				refused++
				continue
			}
			if known, ok := recordParses(record.Kind, record.ID, payload.Data); known && !ok {
				refused++
				continue
			}
		}
		kept = append(kept, record)
	}
	return kept, refused
}

// configBase is what this window last read from or wrote to the store. Saving
// writes only what differs from it, and deletes only what it once held: a window
// that has not heard of a record another one added must not delete it.
type configBase struct {
	payloads map[configstore.Key][]byte
	ords     map[configstore.Key]int64
	maxOrd   int64
	local    []byte
	// hostRecords is what the last save wrote for each host. The host list is
	// the one large list in the state, and a save that edits a single host must
	// not re-encode every other host; see appendHostEntries.
	hostRecords map[string]hostRecord
	// development is the raw local development_workspace value. Release
	// builds neither read nor write it.
	development        []byte
	developmentDamaged bool
	// windowName and window are this window's own local value: the tabs it has
	// open, the deck they fill and where it sits. Read when the window starts and
	// written by this window only, so no other window's save can replace it. An
	// empty name is a state built without a window (a test, or a save that writes
	// one document), which keeps its tabs in the device document as before.
	windowName string
	window     []byte
	// stored says the last write put something in the store: records that
	// changed, or this device's own values, which the store counts separately.
	// False says the save found the store already holding what this window has,
	// so the other windows have nothing new to read. writeEntries sets it, and a
	// save reads it right after the write returns.
	stored bool
}

// hostRecord remembers the state a host was last encoded from, its ordinal, and
// the record payload that encoding produced. A host whose state and ordinal are
// unchanged reuses the payload; holding it here rather than reading the store
// again also makes a retry after a failed commit write the edit, not the record
// the failed save left behind.
type hostRecord struct {
	host    savedHost
	ord     int64
	payload []byte
}

func newConfigBase() *configBase {
	return &configBase{payloads: map[configstore.Key][]byte{}, ords: map[configstore.Key]int64{}}
}

// configDefaults are the data of the records a state with nothing in it would
// have. A record that holds exactly its default, and that this window has never
// had, is not written: a computer that has only ever used the defaults has not
// changed anything, and must not outrank the settings of one that has.
type configDefaults map[configstore.Key]string

// defaultsOf is the defaults a pristine state's records stand for.
func defaultsOf(pristine savedState) configDefaults {
	entries, _, _, err := splitState(pristine, newConfigBase(), nil)
	if err != nil {
		return nil
	}
	defaults := make(configDefaults, len(entries))
	for _, entry := range entries {
		var payload recordPayload
		if json.Unmarshal(entry.Payload, &payload) == nil {
			defaults[configstore.Key{Kind: entry.Kind, ID: entry.ID}] = string(payload.Data)
		}
	}
	return defaults
}

// splitState turns a state into records, this device's own values, and the value
// of the window that wrote it. Items new since the base are numbered after the
// ones it knows. Records that only hold their defaults, and that the base has
// never had, are left out. A base without a window name keeps the window fields
// in the device document, which is what a save that names no window writes.
func splitState(saved savedState, base *configBase, defaults configDefaults) (entries []configstore.Entry, local, window []byte, err error) {
	var collapsed []string
	groups := make([]savedHostGroup, len(saved.HostGroups))
	for index, group := range saved.HostGroups {
		if group.Collapsed {
			collapsed = append(collapsed, group.Name)
		}
		group.Collapsed = false // a device's own view, not the shared list
		groups[index] = group
	}
	saved.HostGroups = groups

	whole, err := json.Marshal(saved)
	if err != nil {
		return nil, nil, nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(whole, &fields); err != nil {
		return nil, nil, nil, err
	}
	entityKeys := make(map[string]bool, len(entityFields))
	for _, entity := range entityFields {
		entityKeys[entity.field] = true
	}
	own := map[string]json.RawMessage{}
	mine := map[string]json.RawMessage{}
	for name, value := range fields {
		switch {
		case entityKeys[name], stateVersionFields[name]:
		case syncedSettings[name]:
			key := configstore.Key{Kind: kindSetting, ID: name}
			if _, held := base.payloads[key]; !held && defaults[key] == string(value) {
				continue
			}
			payload, err := json.Marshal(recordPayload{Data: value})
			if err != nil {
				return nil, nil, nil, err
			}
			entries = append(entries, configstore.Entry{Kind: kindSetting, ID: name, Payload: payload})
		case windowStateFields[name] && base.windowName != "":
			mine[name] = value
		default:
			own[name] = value
		}
	}
	if len(collapsed) > 0 {
		own[collapsedGroupsKey], _ = json.Marshal(collapsed)
	}

	for _, entity := range entityFields {
		if entity.kind == kindHost && uniqueHostIDs(saved.Hosts) {
			entries, base.maxOrd, err = appendHostEntries(entries, base, defaults, saved.Hosts, base.maxOrd)
			if err != nil {
				return nil, nil, nil, err
			}
			continue
		}
		raw, present := fields[entity.field]
		if !present && entity.keepWhenAbsent {
			for key, payload := range base.payloads {
				if key.Kind == entity.kind {
					entries = append(entries, configstore.Entry{Kind: key.Kind, ID: key.ID, Payload: payload})
				}
			}
			continue
		}
		if !present || bytes.Equal(raw, []byte("null")) {
			continue
		}
		var items []json.RawMessage
		if entity.single {
			items = []json.RawMessage{raw}
		} else if err := json.Unmarshal(raw, &items); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", entity.field, err)
		}
		used := map[string]bool{}
		var linked []json.RawMessage
		for _, item := range items {
			var itemFields map[string]json.RawMessage
			if err := json.Unmarshal(item, &itemFields); err != nil {
				return nil, nil, nil, fmt.Errorf("%s: %w", entity.field, err)
			}
			if linkedLocalKey(entity.kind) != "" && isLinkedNote(itemFields) {
				linked = append(linked, item)
				continue
			}
			id := boundKey(entity.key(itemFields))
			if id == "" {
				continue // nothing names it, so nothing can be synced about it
			}
			unique := id
			for number := 2; used[unique]; number++ {
				unique = fmt.Sprintf("%s#%d", id, number)
			}
			id = unique
			used[id] = true
			key := configstore.Key{Kind: entity.kind, ID: id}
			if _, held := base.payloads[key]; !held && defaults[key] == string(item) {
				continue
			}
			ord, known := base.ords[key]
			if !known {
				base.maxOrd++
				ord = base.maxOrd
			}
			payload, err := json.Marshal(recordPayload{Ord: ord, Data: item})
			if err != nil {
				return nil, nil, nil, err
			}
			entries = append(entries, configstore.Entry{Kind: entity.kind, ID: id, Payload: payload})
		}
		if len(linked) > 0 {
			own[linkedLocalKey(entity.kind)], _ = json.Marshal(linked)
		}
	}
	local, err = json.Marshal(own)
	if err != nil {
		return nil, nil, nil, err
	}
	if base.windowName == "" {
		return entries, local, nil, nil
	}
	window, err = json.Marshal(mine)
	if err != nil {
		return nil, nil, nil, err
	}
	return entries, local, window, nil
}

// isLinkedNote reports whether a note's fields name the file it lives in.
func isLinkedNote(fields map[string]json.RawMessage) bool {
	raw := fields["link"]
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null"))
}

// linkedSavedFolders is the folders of a list that mirror a directory.
func linkedSavedFolders(folders []savedNoteFolder) []savedNoteFolder {
	var linked []savedNoteFolder
	for _, folder := range folders {
		if folder.Link != nil {
			linked = append(linked, folder)
		}
	}
	return linked
}

// linkedSavedNotes is the notes of a list that live in a file.
func linkedSavedNotes(notes []savedNote) []savedNote {
	var linked []savedNote
	for _, note := range notes {
		if note.Link != nil {
			linked = append(linked, note)
		}
	}
	return linked
}

// mergeLinkedNotes appends the device's linked notes to the shared ones. A note
// the shared records also hold by id stays the shared one: that is a note that was
// unlinked, and the stale local entry must not shadow it.
func mergeLinkedNotes(shared, linked json.RawMessage) json.RawMessage {
	var held, local []json.RawMessage
	if json.Unmarshal(shared, &held) != nil && len(shared) > 0 && !bytes.Equal(shared, []byte("null")) {
		return shared
	}
	if json.Unmarshal(linked, &local) != nil {
		return shared
	}
	ids := make(map[string]bool, len(held))
	for _, item := range held {
		var fields map[string]json.RawMessage
		if json.Unmarshal(item, &fields) == nil {
			ids[fieldText(fields, "id")] = true
		}
	}
	for _, item := range local {
		var fields map[string]json.RawMessage
		if json.Unmarshal(item, &fields) == nil && !ids[fieldText(fields, "id")] {
			held = append(held, item)
		}
	}
	merged, err := json.Marshal(held)
	if err != nil {
		return shared
	}
	return merged
}

// joinState builds a state from records and this device's own values, and the
// base that describes them.
func joinState(entries []configstore.Entry, local []byte) (savedState, *configBase, error) {
	base := newConfigBase()
	fields := map[string]json.RawMessage{}
	if len(local) > 0 {
		if err := json.Unmarshal(local, &fields); err != nil {
			return savedState{}, nil, fmt.Errorf("device values: %w", err)
		}
	}
	base.local = local

	type item struct {
		ord  int64
		id   string
		data json.RawMessage
	}
	lists := map[string][]item{}
	for _, entry := range entries {
		var payload recordPayload
		if json.Unmarshal(entry.Payload, &payload) != nil || len(payload.Data) == 0 {
			continue // a record this build cannot read is left out, not fatal
		}
		if known, ok := recordParses(entry.Kind, entry.ID, payload.Data); !known || !ok {
			// One this build does not know or cannot read stays in the store as
			// it is: not part of the state, so never taken for deleted.
			continue
		}
		if entry.Kind == kindWorkspaceTemplate {
			var template WorkspaceTemplate
			if json.Unmarshal(payload.Data, &template) != nil || template.Validate() != nil || template.ID != entry.ID {
				continue
			}
		}
		key := configstore.Key{Kind: entry.Kind, ID: entry.ID}
		base.payloads[key] = entry.Payload
		base.ords[key] = payload.Ord
		base.maxOrd = max(base.maxOrd, payload.Ord)
		if entry.Kind == kindSetting {
			if syncedSettings[entry.ID] {
				fields[entry.ID] = payload.Data
			}
			continue
		}
		lists[entry.Kind] = append(lists[entry.Kind], item{payload.Ord, entry.ID, payload.Data})
	}
	for _, entity := range entityFields {
		items := lists[entity.kind]
		if len(items) == 0 {
			continue
		}
		sort.Slice(items, func(left, right int) bool {
			if items[left].ord != items[right].ord {
				return items[left].ord < items[right].ord
			}
			return items[left].id < items[right].id
		})
		if entity.single {
			fields[entity.field] = items[0].data
			continue
		}
		list := make([]json.RawMessage, len(items))
		for index, each := range items {
			list[index] = each.data
		}
		fields[entity.field], _ = json.Marshal(list)
	}
	if raw, ok := fields[linkedNotesKey]; ok {
		fields["notes"] = mergeLinkedNotes(fields["notes"], raw)
		delete(fields, linkedNotesKey)
	}
	if raw, ok := fields[linkedNoteFoldersKey]; ok {
		fields["note_folders"] = mergeLinkedNotes(fields["note_folders"], raw)
		delete(fields, linkedNoteFoldersKey)
	}
	var collapsed []string
	if raw, ok := fields[collapsedGroupsKey]; ok {
		_ = json.Unmarshal(raw, &collapsed)
		delete(fields, collapsedGroupsKey)
	}
	whole, err := json.Marshal(fields)
	if err != nil {
		return savedState{}, nil, err
	}
	var saved savedState
	if err := json.Unmarshal(whole, &saved); err != nil {
		return savedState{}, nil, err
	}
	saved.Version, saved.HostCatalogVersion, saved.ShortcutVersion = stateFileVersion, hostCatalogSchemaVersion, shortcutSchemaVersion
	for index := range saved.HostGroups {
		for _, name := range collapsed {
			if saved.HostGroups[index].Name == name {
				saved.HostGroups[index].Collapsed = true
			}
		}
	}
	return saved, base, nil
}

// applyWindowState puts one window's own work back: what its record holds, and,
// for the first window only, what the device document still holds from before
// windows had records of their own. Every other window starts empty rather than
// opening another window's tabs.
func applyWindowState(saved savedState, window []byte, id int) savedState {
	held := map[string]json.RawMessage{}
	if len(window) > 0 {
		if err := json.Unmarshal(window, &held); err != nil {
			return saved
		}
	}
	if id != 1 && len(held) == 0 {
		clearSavedWorkspace(&saved)
		return saved
	}
	whole, err := json.Marshal(saved)
	if err != nil {
		return saved
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(whole, &fields); err != nil {
		return saved
	}
	for name := range windowStateFields {
		if value, ok := held[name]; ok {
			fields[name] = value
			continue
		}
		if id != 1 {
			delete(fields, name)
		}
	}
	whole, err = json.Marshal(fields)
	if err != nil {
		return saved
	}
	var merged savedState
	if err := json.Unmarshal(whole, &merged); err != nil {
		return saved
	}
	return merged
}

// diff is what a save must write: the records that are new or changed since the
// base, and the ones the base held that are gone.
func (base *configBase) diff(entries []configstore.Entry) (puts []configstore.Entry, deletes []configstore.Key) {
	now := make(map[configstore.Key]bool, len(entries))
	for _, entry := range entries {
		key := configstore.Key{Kind: entry.Kind, ID: entry.ID}
		now[key] = true
		if !bytes.Equal(base.payloads[key], entry.Payload) {
			puts = append(puts, entry)
		}
	}
	for key := range base.payloads {
		if !now[key] {
			deletes = append(deletes, key)
		}
	}
	sort.Slice(deletes, func(left, right int) bool {
		if deletes[left].Kind != deletes[right].Kind {
			return deletes[left].Kind < deletes[right].Kind
		}
		return deletes[left].ID < deletes[right].ID
	})
	return puts, deletes
}

// acceptLocal records that the store now holds entries and both local values.
func (base *configBase) acceptLocal(entries []configstore.Entry, local localWrite) {
	payloads := make(map[configstore.Key][]byte, len(entries))
	ords := make(map[configstore.Key]int64, len(entries))
	for _, entry := range entries {
		key := configstore.Key{Kind: entry.Kind, ID: entry.ID}
		payloads[key] = entry.Payload
		// A host's ordinal is the catalog position the user can change, so it has
		// to be read back from the record that was just written. Every other kind
		// keeps the ordinal this base already knows: their payloads do not carry a
		// meaningful one (recordPayload.Ord is omitted when it is zero, and synced
		// settings never set it), so decoding would replace a known ordinal with 0.
		var payload recordPayload
		decoded := json.Unmarshal(entry.Payload, &payload) == nil
		if ord, known := base.ords[key]; known && !(decoded && entry.Kind == kindHost) {
			ords[key] = ord
			continue
		}
		if decoded {
			ords[key] = payload.Ord
		}
	}
	base.payloads, base.ords = payloads, ords
	base.local = local.state
	if local.windowName != "" {
		base.window = local.window
	}
	if local.includeDevelopment {
		base.development = local.development
	}
}

// writeConfig stores a state through a store and updates the base. It says
// whether a shared record changed, which is what other computers will want.
func writeConfig(store *configstore.Store, base *configBase, defaults configDefaults, saved savedState) (shared bool, err error) {
	entries, local, window, err := splitState(saved, base, defaults)
	if err != nil {
		return false, err
	}
	localValues, err := routeLocalState(base, local)
	if err != nil {
		return false, err
	}
	localValues.windowName, localValues.window = base.windowName, window
	return writeEntries(store, base, entries, localValues)
}

// localWrite is the local values a save can replace: this device's document, the
// development workspace beside it, and the value of the window that is saving. A
// release save does not include the development key at all, so it cannot remove
// one it did not read.
type localWrite struct {
	state              []byte
	development        []byte
	includeDevelopment bool
	// windowName and window are the saving window's own value. An empty name is a
	// state with no window of its own, whose tabs stay in the device document.
	windowName string
	window     []byte
}

func isWorkspaceField(name string) bool {
	switch name {
	case "workspace", "sessions", "active_session", "deck", "workspace_layouts", "note_sidebars":
		return true
	default:
		return false
	}
}

func savedStateFieldNames() map[string]bool {
	kind := reflect.TypeOf(savedState{})
	names := make(map[string]bool, kind.NumField()+1)
	for index := 0; index < kind.NumField(); index++ {
		name, _, _ := strings.Cut(kind.Field(index).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names[name] = true
		}
	}
	names[collapsedGroupsKey] = true
	names[linkedNotesKey] = true
	names[linkedNoteFoldersKey] = true
	return names
}

func decodeLocalState(raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	return fields, nil
}

func discardKnownLocalState(release map[string]json.RawMessage) {
	known := savedStateFieldNames()
	for name := range release {
		if known[name] && !isWorkspaceField(name) {
			delete(release, name)
		}
	}
}

func splitLocalState(release, common map[string]json.RawMessage) map[string]json.RawMessage {
	development := map[string]json.RawMessage{}
	for name, value := range common {
		if isWorkspaceField(name) {
			development[name] = value
		} else {
			release[name] = value
		}
	}
	return development
}

// routeLocalState keeps the release workspace in state while a development
// build saves its own workspace in a separate local key.
func routeLocalState(base *configBase, local []byte) (localWrite, error) {
	if !buildinfo.Development {
		return localWrite{state: local}, nil
	}
	common, err := decodeLocalState(local)
	if err != nil {
		return localWrite{}, err
	}
	release := map[string]json.RawMessage{}
	if len(base.local) > 0 {
		release, err = decodeLocalState(base.local)
		if err != nil {
			return localWrite{}, err
		}
		discardKnownLocalState(release)
	}
	development := splitLocalState(release, common)
	releaseBytes, err := json.Marshal(release)
	if err != nil {
		return localWrite{}, err
	}
	developmentBytes, err := json.Marshal(development)
	if err != nil {
		return localWrite{}, err
	}
	return localWrite{state: releaseBytes, development: developmentBytes, includeDevelopment: true}, nil
}

// mergeRecentConnectionTimes keeps successes recorded by other windows even
// when this window writes an older local snapshot or changes another setting.
func mergeRecentConnectionTimes(current, next []byte) ([]byte, error) {
	var previous struct {
		RecentConnections map[string]time.Time `json:"recent_connections"`
	}
	if len(current) == 0 {
		return next, nil
	}
	if err := json.Unmarshal(current, &previous); err != nil {
		return nil, err
	}
	if len(previous.RecentConnections) == 0 {
		return next, nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(next, &values); err != nil {
		return nil, err
	}
	times := make(map[string]time.Time)
	if raw := values["recent_connections"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &times); err != nil {
			return nil, err
		}
	}
	if times == nil {
		times = make(map[string]time.Time)
	}
	for key, at := range previous.RecentConnections {
		if at.After(times[key]) {
			times[key] = at
		}
	}
	raw, err := json.Marshal(times)
	if err != nil {
		return nil, err
	}
	values["recent_connections"] = raw
	return json.Marshal(values)
}

// writeEntries commits the difference between entries and base, then makes the
// base describe them. It is the second half of a save, shared by the full
// encoder and the hosts-only shortcut.
func writeEntries(store *configstore.Store, base *configBase, entries []configstore.Entry, local localWrite) (shared bool, err error) {
	base.stored = false
	puts, deletes := base.diff(entries)
	change := configstore.Change{Writer: configWriter(), Delete: deletes, Expected: map[configstore.Key][]byte{}}
	patchedPayloads := make(map[configstore.Key][]byte, len(puts))
	for _, entry := range puts {
		key := configstore.Key{Kind: entry.Kind, ID: entry.ID}
		previous, held := base.payloads[key]
		if !held {
			change.Put = append(change.Put, entry)
			change.Expected[key] = nil
			continue
		}
		before, err := projectRecord(key, previous)
		if err != nil {
			return false, err
		}
		after, err := projectRecord(key, entry.Payload)
		if err != nil {
			return false, err
		}
		patch := configstore.JSONChange{Before: before, After: after}
		kept, err := patch.Apply(previous)
		if err != nil {
			return false, err
		}
		patchedPayloads[key] = kept
		change.Patch = append(change.Patch, configstore.PatchEntry{Key: key, JSONChange: patch})
	}
	for _, key := range deletes {
		change.Expected[key] = base.payloads[key]
	}
	if !bytes.Equal(local.state, base.local) || local.includeDevelopment && !bytes.Equal(local.development, base.development) {
		change.LocalPatch = map[string]configstore.JSONChange{}
		if !bytes.Equal(local.state, base.local) {
			patch, err := localPatch(base.local, local.state)
			if err != nil {
				return false, err
			}
			change.LocalPatch[localKey] = patch
			desired := local.state
			change.MergeLocal = map[string]func([]byte, []byte) ([]byte, error){
				localKey: func(current, next []byte) ([]byte, error) {
					merged, err := mergeRecentConnectionTimes(desired, next)
					if err == nil {
						local.state = merged
					}
					return merged, err
				},
			}
		}
		if local.includeDevelopment && !bytes.Equal(local.development, base.development) {
			if base.developmentDamaged {
				change.Local = map[string][]byte{developmentWorkspaceKey: local.development}
				change.ExpectedLocal = map[string][]byte{developmentWorkspaceKey: base.development}
			} else {
				patch, err := localPatch(base.development, local.development)
				if err != nil {
					return false, err
				}
				change.LocalPatch[developmentWorkspaceKey] = patch
			}
		}
	}
	if local.windowName != "" && !bytes.Equal(local.window, base.window) {
		if change.LocalPatch == nil {
			change.LocalPatch = map[string]configstore.JSONChange{}
		}
		patch, err := localPatch(base.window, local.window)
		if err != nil {
			return false, err
		}
		change.LocalPatch[local.windowName] = patch
	}
	if len(puts) == 0 && len(deletes) == 0 && change.LocalPatch == nil {
		return false, nil
	}
	for key, patch := range change.LocalPatch {
		previous := base.local
		switch key {
		case developmentWorkspaceKey:
			previous = base.development
		case local.windowName:
			previous = base.window
		}
		if len(previous) == 0 {
			previous = []byte("{}")
		}
		value, err := patch.Apply(previous)
		if err != nil {
			return false, err
		}
		switch key {
		case developmentWorkspaceKey:
			local.development = value
		case local.windowName:
			local.window = value
		default:
			local.state = value
		}
	}
	if err := guardNoteGraphChange(store, &change); err != nil {
		return false, err
	}
	written, err := store.Commit(change)
	if err != nil {
		return false, err
	}
	base.stored = written > 0 || change.LocalPatch != nil || len(change.Local) > 0
	for index := range entries {
		entry := &entries[index]
		key := configstore.Key{Kind: entry.Kind, ID: entry.ID}
		if payload, exists := patchedPayloads[key]; exists {
			entry.Payload = payload
			if cached, ok := base.hostRecords[entry.ID]; entry.Kind == kindHost && ok {
				cached.payload = payload
				base.hostRecords[entry.ID] = cached
			}
		}
	}
	base.acceptLocal(entries, local)
	base.developmentDamaged = false
	return len(puts) > 0 || len(deletes) > 0, nil
}

// openConfig opens the store at path and reads the state in it, including the
// record of window id. A file that is not a store is set aside and a new one is
// started; one from a newer build is left alone and reported, with no store.
func openConfig(path string, id int) (store *configstore.Store, saved savedState, base *configBase, report loadReport, err error) {
	fresh := savedState{Version: stateFileVersion, HostCatalogVersion: hostCatalogSchemaVersion, ShortcutVersion: shortcutSchemaVersion}
	store, err = configstore.OpenShared(path, configFormat)
	var tooNew *configstore.TooNewError
	switch {
	case errors.As(err, &tooNew):
		return nil, fresh, newConfigBase(), loadReport{outcome: loadTooNew, found: tooNew.Found, supported: tooNew.Supported}, nil
	case errors.Is(err, configstore.ErrCorrupt):
		kept, renameErr := setAside(path)
		if renameErr != nil {
			return nil, fresh, newConfigBase(), loadReport{outcome: loadUnreadable}, fmt.Errorf("keep unreadable %s: %w", path, renameErr)
		}
		store, err = configstore.OpenShared(path, configFormat)
		if err != nil {
			return nil, fresh, newConfigBase(), loadReport{outcome: loadUnreadable, kept: kept}, fmt.Errorf("open %s: %w", path, err)
		}
		return store, fresh, newConfigBase(), loadReport{outcome: loadUnreadable, kept: kept}, nil
	case err != nil:
		return nil, fresh, newConfigBase(), loadReport{}, fmt.Errorf("open %s: %w", path, err)
	}
	entries, local, err := store.Load()
	if err == nil {
		saved, base, err = joinState(entries, local[localKey])
		if err == nil {
			base.development = local[developmentWorkspaceKey]
			// The window reads its own value and nothing else writes it, so two
			// windows of this process never share their tabs.
			base.windowName = windowKey(id, buildinfo.Development)
			base.window = local[base.windowName]
			saved = applyWindowState(saved, base.window, id)
			if buildinfo.Development {
				// A development build used to keep its workspace in a document of
				// its own; that document is read once for a window that has no
				// record yet, and a damaged one is reported the same either way.
				// The workspace is cleared first, so a record that does not read
				// leaves the window empty instead of half restored.
				clearSavedWorkspace(&saved)
				if len(base.window) > 0 {
					report.workspaceErr = applySavedWorkspace(&saved, base.window)
				} else if raw, ok := local[developmentWorkspaceKey]; ok {
					report.workspaceErr = applySavedWorkspace(&saved, raw)
				}
				base.developmentDamaged = report.workspaceErr != nil
			}
		}
	}
	if err != nil {
		// The records were read but make no state: as damaged as an unreadable file.
		store.Close()
		kept, renameErr := setAside(path)
		if renameErr != nil {
			return nil, fresh, newConfigBase(), loadReport{outcome: loadUnreadable}, fmt.Errorf("keep unreadable %s: %w", path, renameErr)
		}
		if store, err = configstore.OpenShared(path, configFormat); err != nil {
			return nil, fresh, newConfigBase(), loadReport{outcome: loadUnreadable, kept: kept}, fmt.Errorf("open %s: %w", path, err)
		}
		return store, fresh, newConfigBase(), loadReport{outcome: loadUnreadable, kept: kept}, nil
	}
	return store, saved, base, report, nil
}
