package configstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrConflict means a stale edit would discard data it did not read. The whole
// transaction is rolled back; the caller must reload before retrying the edit.
var ErrConflict = errors.New("config store: configuration changed; reload before saving")

// JSONChange contains only the fields understood by its caller, before and
// after an edit. Objects are patched recursively. Arrays are atomic values:
// editing one requires having read all of its elements without loss.
type JSONChange struct {
	Before, After []byte
}

// Apply also lets a caller advance its own baseline without pretending it has
// read concurrent edits made by another window.
func (change JSONChange) Apply(current []byte) ([]byte, error) {
	return patchJSON(current, change.Before, change.After)
}

type PatchEntry struct {
	Key
	JSONChange
}

func sameJSON(left, right []byte) bool {
	if bytes.Equal(left, right) {
		return true
	}
	var a, b bytes.Buffer
	if json.Compact(&a, left) != nil || json.Compact(&b, right) != nil {
		return false
	}
	if bytes.Equal(a.Bytes(), b.Bytes()) {
		return true
	}
	if l, ok := object(left); ok {
		r, ok := object(right)
		if !ok || len(l) != len(r) {
			return false
		}
		for key, value := range l {
			if !sameJSON(value, r[key]) {
				return false
			}
		}
		return true
	}
	var l, r []json.RawMessage
	if len(a.Bytes()) > 0 && a.Bytes()[0] == '[' && len(b.Bytes()) > 0 && b.Bytes()[0] == '[' &&
		json.Unmarshal(left, &l) == nil && json.Unmarshal(right, &r) == nil && len(l) == len(r) {
		for index := range l {
			if !sameJSON(l[index], r[index]) {
				return false
			}
		}
		return true
	}
	return false
}

func object(raw []byte) (map[string]json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false
	}
	var fields map[string]json.RawMessage
	err := json.Unmarshal(raw, &fields)
	return fields, err == nil
}

// patchJSON applies the difference, not the caller's entire snapshot, to the
// latest value inside the write transaction. A missing field differs from null.
func patchJSON(current, before, after []byte) ([]byte, error) {
	if sameJSON(before, after) {
		return current, nil
	}
	old, oldObject := object(before)
	next, nextObject := object(after)
	live, liveObject := object(current)
	if oldObject && len(old) == 0 && len(after) == 0 && liveObject {
		// An optional object containing only unknown fields projects as {}.
		// Omitting that empty projection is not an instruction to erase it.
		return current, nil
	}
	if oldObject && nextObject && liveObject {
		for key := range old {
			if _, exists := next[key]; !exists {
				value, err := patchJSON(live[key], old[key], nil)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				if value == nil {
					delete(live, key)
				} else {
					live[key] = value
				}
			}
		}
		for key, value := range next {
			patched, err := patchJSON(live[key], old[key], value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			if patched != nil {
				live[key] = patched
			}
		}
		return json.Marshal(live)
	}
	if sameJSON(current, after) {
		return current, nil
	}
	if !sameJSON(current, before) && !(len(before) == 0 && jsonZero(current)) {
		return nil, ErrConflict
	}
	return after, nil
}

func jsonZero(raw []byte) bool {
	switch string(bytes.TrimSpace(raw)) {
	case "null", "false", "0", `""`, "[]", "{}":
		return true
	}
	return false
}
