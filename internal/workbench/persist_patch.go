package workbench

import (
	"encoding/json"
	"fmt"

	"motty/internal/buildinfo"
	"motty/internal/configstore"
	"motty/internal/update"
)

func configWriter() string { return buildinfo.ProductName + " " + update.Current }

// projectState strips unknown fields through the typed DTO, without creating
// absent fields from its defaults. Raw data stays in SQLite; this projection
// only describes the fields a save is allowed to change.
func projectState(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return []byte("{}"), nil
	}
	var saved savedState
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		return nil, err
	}
	var original, projected map[string]json.RawMessage
	if err := json.Unmarshal(raw, &original); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &projected); err != nil {
		return nil, err
	}
	for key := range projected {
		if _, present := original[key]; !present {
			delete(projected, key)
		}
	}
	if value, present := original[collapsedGroupsKey]; present {
		projected[collapsedGroupsKey] = value
	}
	for _, key := range []string{linkedNotesKey, linkedNoteFoldersKey} {
		if value, present := original[key]; present {
			projected[key] = value
		}
	}
	return json.Marshal(projected)
}

func projectRecord(key configstore.Key, raw []byte) ([]byte, error) {
	var payload recordPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	name, single := key.ID, true
	if key.Kind != kindSetting {
		found := false
		for _, entity := range entityFields {
			if entity.kind == key.Kind {
				name, single, found = entity.field, entity.single, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("cannot edit unknown record kind %q", key.Kind)
		}
	}
	data := payload.Data
	if !single {
		data = append(append([]byte{'['}, data...), ']')
	}
	wrapped, err := json.Marshal(map[string]json.RawMessage{name: data})
	if err != nil {
		return nil, err
	}
	projected, err := projectState(wrapped)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(projected, &fields); err != nil {
		return nil, err
	}
	data = fields[name]
	if !single {
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		if len(items) != 1 {
			return nil, fmt.Errorf("invalid %s record", key.Kind)
		}
		data = items[0]
	}
	if len(data) == 0 {
		// Zero-valued scalar settings are omitted by savedState.
		data = payload.Data
	}
	payload.Data = data
	return json.Marshal(payload)
}

func localPatch(before, after []byte) (configstore.JSONChange, error) {
	before, err := projectLocalPatch(before)
	if err != nil {
		return configstore.JSONChange{}, err
	}
	after, err = projectLocalPatch(after)
	return configstore.JSONChange{Before: before, After: after}, err
}

// Recent connection times use a monotonic merge after the ordinary field patch.
func projectLocalPatch(raw []byte) ([]byte, error) {
	projected, err := projectState(raw)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(projected, &fields); err != nil {
		return nil, err
	}
	delete(fields, "recent_connections")
	return json.Marshal(fields)
}
