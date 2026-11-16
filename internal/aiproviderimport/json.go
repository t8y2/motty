package aiproviderimport

import (
	"encoding/json"
	"strings"
)

func jsonObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	return object, nil
}

func objectField(object map[string]json.RawMessage, key string) map[string]json.RawMessage {
	raw, ok := object[key]
	if !ok {
		return nil
	}
	nested, _ := jsonObject(raw)
	return nested
}

func stringField(object map[string]json.RawMessage, key string) string {
	raw, ok := object[key]
	if !ok {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func boolField(object map[string]json.RawMessage, key string) bool {
	raw, ok := object[key]
	if !ok {
		return false
	}
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.EqualFold(strings.TrimSpace(text), "true")
	}
	return false
}

func hasObject(raw []byte) bool {
	object, err := jsonObject(raw)
	return err == nil && object != nil
}

// cleanModelMessages decodes and bounds a magpie model array in one pass. It
// reads the raw messages directly instead of materialising an intermediate
// []string, which matters when a small hostile file repeats many model names.
func cleanModelMessages(raw []json.RawMessage) []string {
	limit := min(len(raw), MaxModels)
	out := make([]string, 0, limit)
	seen := make(map[string]bool, limit)
	for _, item := range raw {
		var name string
		if json.Unmarshal(item, &name) != nil {
			continue
		}
		name = CleanModel(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= MaxModels {
			break
		}
	}
	return out
}
