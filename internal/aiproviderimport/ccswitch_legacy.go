package aiproviderimport

import (
	"encoding/json"
	"fmt"
)

// cc-switch before v3.7 kept providers in ~/.cc-switch/config.json. The v2
// shape flattens one manager per application at the top level and uses
// camelCase field names; a v1 file held one Claude manager directly.
type legacyManager struct {
	Providers map[string]json.RawMessage `json:"providers"`
	Current   string                     `json:"current"`
}

type legacyProvider struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	SettingsConfig json.RawMessage `json:"settingsConfig"`
	Meta           json.RawMessage `json:"meta"`
}

// ParseCCSwitchLegacyFile reads the legacy JSON file from disk.
func ParseCCSwitchLegacyFile(path string) ([]Candidate, error) {
	data, err := readFileBounded(path, MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	return ParseCCSwitchLegacy(data)
}

// ParseCCSwitchLegacy parses a legacy cc-switch config.json document.
func ParseCCSwitchLegacy(data []byte) ([]Candidate, error) {
	root, err := jsonObject(data)
	if err != nil || root == nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	apps := root
	if raw, ok := root["apps"]; ok && hasObject(raw) {
		apps, _ = jsonObject(raw)
	}

	var out []Candidate
	// v1 was a single Claude manager at the top level.
	if raw, ok := apps["providers"]; ok && firstJSONByte(raw) == '{' {
		wrapped, _ := json.Marshal(map[string]json.RawMessage{"providers": raw})
		candidates, err := legacyManagerCandidates("claude", wrapped)
		if err != nil {
			return nil, err
		}
		out = append(out, candidates...)
	}
	for _, app := range []string{"claude", "codex", "opencode", "gemini"} {
		raw, ok := apps[app]
		if !ok || !hasObject(raw) {
			continue
		}
		candidates, err := legacyManagerCandidates(app, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, candidates...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no providers", ErrMalformed)
	}
	return out, nil
}

func legacyManagerCandidates(app string, raw json.RawMessage) ([]Candidate, error) {
	var manager legacyManager
	if err := json.Unmarshal(raw, &manager); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if len(manager.Providers) > MaxProviders {
		return nil, fmt.Errorf("%w: %d providers", ErrTooLarge, len(manager.Providers))
	}
	ids := sortedKeys(manager.Providers)
	out := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		raw := manager.Providers[id]
		var provider legacyProvider
		if err := json.Unmarshal(raw, &provider); err != nil {
			// One mistyped entry must not discard its siblings. Keep it as an
			// explained, unselectable row like the magpie reader does.
			object, _ := jsonObject(raw)
			name := CleanName(firstNonEmpty(stringField(object, "name"), stringField(object, "id"), id))
			out = append(out, Candidate{Source: SourceCCSwitch, Name: name, SkipReason: ReasonMalformed})
			continue
		}
		if provider.ID == "" {
			provider.ID = id
		}
		out = append(out, ccSwitchCandidate(app, provider.ID, provider.Name, provider.SettingsConfig, provider.Meta, nil))
	}
	return out, nil
}
