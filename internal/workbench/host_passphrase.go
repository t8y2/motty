package workbench

import "encoding/json"

type hostPassphrase struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

func hostPassphraseSecretName(catalogID string) string {
	return hostSecretName(catalogID, "key-passphrase")
}

func encodeHostPassphrase(path, value string) string {
	encoded, _ := json.Marshal(hostPassphrase{Path: path, Value: value})
	return string(encoded)
}

func decodeHostPassphrase(encoded, path string) (string, bool) {
	var saved hostPassphrase
	if err := json.Unmarshal([]byte(encoded), &saved); err != nil || path == "" || saved.Path != path || saved.Value == "" {
		return "", false
	}
	return saved.Value, true
}

func savedPassphraseName(reader passwordReader, owner, path string) string {
	if reader != nil && owner != "" && path != "" {
		name := hostPassphraseSecretName(owner)
		if encoded, found, err := reader.Get(name); err == nil && found {
			if _, matches := decodeHostPassphrase(encoded, path); matches {
				return name
			}
		}
	}
	return passphraseSecretName(path)
}

func savedPassphrase(reader passwordReader, owner, path string) (name, value string, found bool, err error) {
	if reader == nil {
		return "", "", false, nil
	}
	name = savedPassphraseName(reader, owner, path)
	value, found, err = reader.Get(name)
	if name == hostPassphraseSecretName(owner) && err == nil && found {
		value, found = decodeHostPassphrase(value, path)
	}
	return name, value, found, err
}
