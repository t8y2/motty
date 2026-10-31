package workbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"motty/internal/buildinfo"
	"motty/internal/configstore"
)

// savedFieldChange names one top-level field of savedState that differs
// between two snapshots, and carries its current value for the incremental
// writer.
type savedFieldChange struct {
	name  string
	value any
}

// changedSavedFields lists the top-level fields that differ between two
// snapshots. Comparing field by field tells a one-field save from a mixed one
// and lets the incremental writer encode only what changed.
func changedSavedFields(previous, current savedState) []savedFieldChange {
	before, after := reflect.ValueOf(previous), reflect.ValueOf(current)
	kind := before.Type()
	var changes []savedFieldChange
	for index := 0; index < kind.NumField(); index++ {
		if reflect.DeepEqual(before.Field(index).Interface(), after.Field(index).Interface()) {
			continue
		}
		name, _, _ := strings.Cut(kind.Field(index).Tag.Get("json"), ",")
		changes = append(changes, savedFieldChange{name: name, value: after.Field(index).Interface()})
	}
	return changes
}

// writeChangedFields stores several changed top-level fields in one commit.
// Every record and device value that did not change is reused from base; only
// the changed lists, settings and device keys are encoded. The host group list
// is split the same way as the full encoder: records carry no collapsed flag,
// and the collapsed names go into this device's own JSON. handled is false only
// for a state version field.
func writeChangedFields(store *configstore.Store, base *configBase, defaults configDefaults, changes []savedFieldChange) (shared, handled bool, err error) {
	entries := make([]configstore.Entry, 0, len(base.payloads)+len(changes))
	for key, payload := range base.payloads {
		entries = append(entries, configstore.Entry{Kind: key.Kind, ID: key.ID, Payload: payload})
	}
	releaseLocal := map[string]json.RawMessage{}
	if len(base.local) > 0 {
		if err := json.Unmarshal(base.local, &releaseLocal); err != nil {
			return false, true, err
		}
	}
	developmentLocal := map[string]json.RawMessage{}
	if buildinfo.Development && len(base.development) > 0 {
		if err := json.Unmarshal(base.development, &developmentLocal); err != nil {
			return false, true, err
		}
	}
	releaseChanged, developmentChanged := false, false
	nextOrd := base.maxOrd
	for _, change := range changes {
		if change.name == "host_groups" {
			entity, _ := entityByField(change.name)
			groups := change.value.([]savedHostGroup)
			collapsed := make([]string, 0, len(groups))
			stripped := make([]savedHostGroup, len(groups))
			for index, group := range groups {
				if group.Collapsed {
					collapsed = append(collapsed, group.Name)
				}
				group.Collapsed = false // a device's own view, not the shared list
				stripped[index] = group
			}
			entries = withoutKind(entries, entity.kind)
			entries, nextOrd, err = appendEntityEntries(entries, base, defaults, entity, stripped, nextOrd)
			if err != nil {
				return false, true, err
			}
			changed, err := setLocalField(releaseLocal, collapsedGroupsKey, collapsed)
			if err != nil {
				return false, true, err
			}
			releaseChanged = releaseChanged || changed
			continue
		}
		if change.name == "ai_providers" {
			// A nil provider list means "not read yet", which splitState keeps as
			// the records already in base.
			value := reflect.ValueOf(change.value)
			if value.Kind() == reflect.Ptr && value.IsNil() {
				continue
			}
		}
		entity, isEntity := entityByField(change.name)
		switch {
		case isEntity:
			entries = withoutKind(entries, entity.kind)
			entries, nextOrd, err = appendEntityEntries(entries, base, defaults, entity, change.value, nextOrd)
			if err != nil {
				return false, true, err
			}
		case syncedSettings[change.name]:
			entries = withoutSetting(entries, change.name)
			entries, err = appendSettingEntry(entries, base, defaults, change.name, change.value)
			if err != nil {
				return false, true, err
			}
		case stateVersionFields[change.name]:
			return false, false, nil
		default:
			target := releaseLocal
			developmentField := buildinfo.Development && isWorkspaceField(change.name)
			if developmentField {
				target = developmentLocal
			}
			changed, err := setLocalField(target, change.name, change.value)
			if err != nil {
				return false, true, err
			}
			if developmentField {
				developmentChanged = developmentChanged || changed
			} else {
				releaseChanged = releaseChanged || changed
			}
		}
	}
	local := localWrite{state: base.local}
	if releaseChanged {
		local.state, err = json.Marshal(releaseLocal)
		if err != nil {
			return false, true, err
		}
	}
	if buildinfo.Development {
		local.includeDevelopment = true
		local.development = base.development
		if developmentChanged {
			local.development, err = json.Marshal(developmentLocal)
			if err != nil {
				return false, true, err
			}
		}
	}
	sortEntries(entries)
	shared, err = writeEntries(store, base, entries, local)
	if err == nil {
		base.maxOrd = nextOrd
	}
	return shared, true, err
}

func entityByField(name string) (entityField, bool) {
	for _, entity := range entityFields {
		if entity.field == name {
			return entity, true
		}
	}
	return entityField{}, false
}

// appendEntityEntries appends the current records of one changed list to
// entries. It uses the same id, ordering, default and delete rules as
// splitState.
func appendEntityEntries(entries []configstore.Entry, base *configBase, defaults configDefaults, entity entityField, value any, nextOrd int64) ([]configstore.Entry, int64, error) {
	// The host list is saved one record at a time: an edit of one host must not
	// re-encode the whole catalog. The generic path below is kept for lists
	// whose keys are derived from their content and for duplicate or anonymous
	// host ids, whose record keys depend on the order.
	if entity.kind == kindHost {
		if hosts, ok := value.([]savedHost); ok && uniqueHostIDs(hosts) {
			return appendHostEntries(entries, base, defaults, hosts, nextOrd)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nextOrd, err
	}
	var items []json.RawMessage
	if entity.single {
		if !bytes.Equal(raw, []byte("null")) {
			items = []json.RawMessage{raw}
		}
	} else if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nextOrd, err
	}
	used := map[string]bool{}
	for _, item := range items {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(item, &fields); err != nil {
			return nil, nextOrd, err
		}
		id := boundKey(entity.key(fields))
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
			nextOrd++
			ord = nextOrd
		}
		payload, err := json.Marshal(recordPayload{Ord: ord, Data: item})
		if err != nil {
			return nil, nextOrd, err
		}
		entries = append(entries, configstore.Entry{Kind: entity.kind, ID: id, Payload: payload})
	}
	return entries, nextOrd, nil
}

const kindHost = "host"

// sameSavedHost reports whether two saved hosts hold the same state. It compares
// field by field instead of reflect.DeepEqual, which allocates an interface box
// and a visited map per call, three hundred times per save on a large catalog.
func sameSavedHost(left, right savedHost) bool {
	return left.Color == right.Color && left.ID == right.ID && left.Label == right.Label &&
		left.Group == right.Group && slices.Equal(left.Tags, right.Tags) && left.Icon == right.Icon &&
		left.Address == right.Address && left.User == right.User && left.Port == right.Port &&
		left.System == right.System &&
		left.Protocol == right.Protocol && left.Line == right.Line && left.Flow == right.Flow &&
		sameSavedProxy(left.Proxy, right.Proxy) &&
		left.Algorithms == right.Algorithms && left.AlgorithmList == right.AlgorithmList &&
		left.Authentication == right.Authentication && left.Identity == right.Identity &&
		left.ForwardX11 == right.ForwardX11 && left.AgentForward == right.AgentForward &&
		left.NoHistory == right.NoHistory && left.InitScript == right.InitScript && left.Transfer == right.Transfer &&
		left.ViaSSH == right.ViaSSH && left.Clipboard == right.Clipboard && left.Console == right.Console
}

func sameSavedProxy(left, right *savedOutboundProxy) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// uniqueHostIDs reports whether every host has a non-empty id of its own, which
// is what lets appendHostEntries key its cache by id.
func uniqueHostIDs(hosts []savedHost) bool {
	if len(hosts) < 2 {
		return len(hosts) == 0 || hosts[0].ID != ""
	}
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if host.ID == "" {
			return false
		}
		if _, duplicate := seen[host.ID]; duplicate {
			return false
		}
		seen[host.ID] = struct{}{}
	}
	return true
}

// appendHostEntries appends the host records, reusing the record of every host
// that did not change since the last save. It applies the same id, ordinal,
// default and skip rules as the generic appendEntityEntries path.
func appendHostEntries(entries []configstore.Entry, base *configBase, defaults configDefaults, hosts []savedHost, nextOrd int64) ([]configstore.Entry, int64, error) {
	if base.hostRecords == nil {
		base.hostRecords = make(map[string]hostRecord, len(hosts))
	}
	for _, host := range hosts {
		key := configstore.Key{Kind: kindHost, ID: host.ID}
		ord, known := base.ords[key]
		if record, cached := base.hostRecords[host.ID]; cached && known && record.ord == ord && sameSavedHost(record.host, host) {
			entries = append(entries, configstore.Entry{Kind: kindHost, ID: host.ID, Payload: record.payload})
			continue
		}
		raw, err := json.Marshal(host)
		if err != nil {
			return nil, nextOrd, err
		}
		if _, held := base.payloads[key]; !held && defaults[key] == string(raw) {
			continue
		}
		if !known {
			nextOrd++
			ord = nextOrd
		}
		payload, err := json.Marshal(recordPayload{Ord: ord, Data: raw})
		if err != nil {
			return nil, nextOrd, err
		}
		entries = append(entries, configstore.Entry{Kind: kindHost, ID: host.ID, Payload: payload})
		base.hostRecords[host.ID] = hostRecord{host: host, ord: ord, payload: payload}
	}
	return entries, nextOrd, nil
}

// appendSettingEntry appends one changed shared setting. A setting left at its
// zero value has no record, exactly as the omitted field in splitState's JSON
// has none, so no entry is added and the base's old record is deleted.
func appendSettingEntry(entries []configstore.Entry, base *configBase, defaults configDefaults, name string, value any) ([]configstore.Entry, error) {
	omitted := reflect.ValueOf(value).IsZero()
	if v := reflect.ValueOf(value); v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
		omitted = v.Len() == 0
	}
	if omitted {
		return entries, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	key := configstore.Key{Kind: kindSetting, ID: name}
	if _, held := base.payloads[key]; !held && defaults[key] == string(data) {
		return entries, nil
	}
	payload, err := json.Marshal(recordPayload{Data: data})
	if err != nil {
		return nil, err
	}
	return append(entries, configstore.Entry{Kind: key.Kind, ID: key.ID, Payload: payload}), nil
}

// setLocalField updates or drops one key of this device's own JSON object. It
// reports whether the object changed.
func setLocalField(local map[string]json.RawMessage, name string, value any) (bool, error) {
	omitted := reflect.ValueOf(value).IsZero()
	if v := reflect.ValueOf(value); v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
		omitted = v.Len() == 0
	}
	if omitted {
		if _, present := local[name]; !present {
			return false, nil
		}
		delete(local, name)
		return true, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	if bytes.Equal(local[name], data) {
		return false, nil
	}
	local[name] = data
	return true, nil
}

func withoutKind(entries []configstore.Entry, kind string) []configstore.Entry {
	kept := entries[:0]
	for _, entry := range entries {
		if entry.Kind != kind {
			kept = append(kept, entry)
		}
	}
	return kept
}

func withoutSetting(entries []configstore.Entry, name string) []configstore.Entry {
	kept := entries[:0]
	for _, entry := range entries {
		if entry.Kind != kindSetting || entry.ID != name {
			kept = append(kept, entry)
		}
	}
	return kept
}

func sortEntries(entries []configstore.Entry) {
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].Kind != entries[right].Kind {
			return entries[left].Kind < entries[right].Kind
		}
		return entries[left].ID < entries[right].ID
	})
}
