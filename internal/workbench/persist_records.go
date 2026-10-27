package workbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"motty/internal/ai/store"
	"motty/internal/configstore"
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

// configFormat is the version of the record payloads this build writes. A change
// that an older build could not read back unchanged (a new field it would drop
// when it saved the record, a value it would reject) raises it; a build refuses a
// store or a synced file with a higher number than its own, so that it never
// saves over data it only half understands.
const configFormat = 3

const (
	kindSetting = "setting"
	localKey    = "state"
)

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
	{field: "hosts", kind: "host", key: func(fields map[string]json.RawMessage) string {
		if id := fieldText(fields, "id"); id != "" {
			return id
		}
		return joinKey("anonymous", fieldText(fields, "label"), fieldText(fields, "address"), fieldText(fields, "user"), fieldText(fields, "port"))
	}},
	{field: "host_groups", kind: "group", key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "name") }},
	{field: "forwards", kind: "forward", key: func(fields map[string]json.RawMessage) string {
		return joinKey(fieldText(fields, "mode"), fieldText(fields, "host_address"), fieldText(fields, "host_user"), fieldText(fields, "host_port"),
			fieldText(fields, "local_port"), fieldText(fields, "destination_host"), fieldText(fields, "destination_port"))
	}},
	{field: "proxies", kind: "proxy", key: func(fields map[string]json.RawMessage) string {
		return joinKey(fieldText(fields, "target_user"), fieldText(fields, "target_address"), fieldText(fields, "target_port"))
	}},
	{field: "snippets", kind: "snippet", key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "name") }},
	{field: "highlights", kind: "highlight", key: func(fields map[string]json.RawMessage) string {
		return joinKey(fieldText(fields, "pattern"), fieldText(fields, "regex"), fieldText(fields, "case_sensitive"))
	}},
	{field: "autofill_rules", kind: "autofill", key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "sftp_bookmarks", kind: "bookmark", key: func(fields map[string]json.RawMessage) string {
		return joinKey(fieldText(fields, "host"), fieldText(fields, "path"))
	}},
	{field: "ai_providers", kind: kindAIProvider, keepWhenAbsent: true, key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "ai_cli_providers", kind: kindAICLIProvider, keepWhenAbsent: true, key: func(fields map[string]json.RawMessage) string { return fieldText(fields, "id") }},
	{field: "outbound_proxy", kind: "outbound-proxy", single: true, key: func(map[string]json.RawMessage) string { return "main" }},
}

// syncedSettings are the scalar settings every device shares.
var syncedSettings = map[string]bool{
	"highlight_builtin":            true,
	"terminalOption":               true,
	"terminal_scrollback":          true,
	"connection_timeout_seconds":   true,
	"connection_keepalive_seconds": true,
	"connection_reconnect":         true,
	"terminal_clipboard":           true,
	"terminal_cursor_steady":       true,
	"terminal_scheme":              true,
	"terminal_padding":             true,
	"terminal_gutter":              true,
	"terminal_notifications":       true,
	"sftp_parallel":                true,
	"sftp_keep_times":              true,
	"sftp_follow":                  true,
	"sftp_path_input":              true,
	"command_history":              true,
	"command_suggest_off":          true,
}

// stateVersionFields are written back as this build's own on every load.
var stateVersionFields = map[string]bool{"version": true, "host_catalog_version": true, "shortcut_version": true}

const collapsedGroupsKey = "collapsed_groups"

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
	entries, _, err := splitState(pristine, newConfigBase(), nil)
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

// splitState turns a state into records and this device's own values. Items new
// since the base are numbered after the ones it knows. Records that only hold
// their defaults, and that the base has never had, are left out.
func splitState(saved savedState, base *configBase, defaults configDefaults) (entries []configstore.Entry, local []byte, err error) {
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
		return nil, nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(whole, &fields); err != nil {
		return nil, nil, err
	}
	entityKeys := make(map[string]bool, len(entityFields))
	for _, entity := range entityFields {
		entityKeys[entity.field] = true
	}
	own := map[string]json.RawMessage{}
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
				return nil, nil, err
			}
			entries = append(entries, configstore.Entry{Kind: kindSetting, ID: name, Payload: payload})
		default:
			own[name] = value
		}
	}
	if len(collapsed) > 0 {
		own[collapsedGroupsKey], _ = json.Marshal(collapsed)
	}

	for _, entity := range entityFields {
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
			return nil, nil, fmt.Errorf("%s: %w", entity.field, err)
		}
		used := map[string]bool{}
		for _, item := range items {
			var itemFields map[string]json.RawMessage
			if err := json.Unmarshal(item, &itemFields); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", entity.field, err)
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
				return nil, nil, err
			}
			entries = append(entries, configstore.Entry{Kind: entity.kind, ID: id, Payload: payload})
		}
	}
	local, err = json.Marshal(own)
	if err != nil {
		return nil, nil, err
	}
	return entries, local, nil
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

// accept records that the store now holds entries and local.
func (base *configBase) accept(entries []configstore.Entry, local []byte) {
	base.payloads = make(map[configstore.Key][]byte, len(entries))
	base.ords = make(map[configstore.Key]int64, len(entries))
	for _, entry := range entries {
		key := configstore.Key{Kind: entry.Kind, ID: entry.ID}
		base.payloads[key] = entry.Payload
		var payload recordPayload
		if json.Unmarshal(entry.Payload, &payload) == nil {
			base.ords[key] = payload.Ord
		}
	}
	base.local = local
}

// writeConfig stores a state through a store and updates the base. It says
// whether a shared record changed, which is what other computers will want.
func writeConfig(store *configstore.Store, base *configBase, defaults configDefaults, saved savedState) (shared bool, err error) {
	entries, local, err := splitState(saved, base, defaults)
	if err != nil {
		return false, err
	}
	return writeEntries(store, base, entries, local)
}

// writeEntries commits the difference between entries and base, then makes the
// base describe them. It is the second half of a save, shared by the full
// encoder and the hosts-only shortcut.
func writeEntries(store *configstore.Store, base *configBase, entries []configstore.Entry, local []byte) (shared bool, err error) {
	puts, deletes := base.diff(entries)
	change := configstore.Change{Put: puts, Delete: deletes}
	if !bytes.Equal(local, base.local) {
		change.Local = map[string][]byte{localKey: local}
	}
	if len(puts) == 0 && len(deletes) == 0 && change.Local == nil {
		return false, nil
	}
	if _, err := store.Commit(change); err != nil {
		return false, err
	}
	base.accept(entries, local)
	return len(puts) > 0 || len(deletes) > 0, nil
}

// openConfig opens the store at path and reads the state in it. A file that is
// not a store is set aside and a new one is started; one from a newer build is
// left alone and reported, with no store.
func openConfig(path string) (store *configstore.Store, saved savedState, base *configBase, report loadReport, err error) {
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
	return store, saved, base, loadReport{}, nil
}
