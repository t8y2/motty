package workbench

import (
	"encoding/json"
	"errors"

	"motty/internal/configstore"
)

func (saved *savedToolbars) specified() bool {
	return saved != nil && (saved.Window != nil || saved.Terminal != nil || saved.SFTP != nil || saved.Dock != nil || saved.Position != "" || saved.Size != "")
}

// Promote a legacy customization before windows take their save bases. Two
// windows must not independently create the same setting on an unrelated save.
// Unknown local fields stay local; only the recognized preferences are shared.
func migrateLocalToolbars(store *configstore.Store, entries []configstore.Entry, local []byte) (bool, error) {
	for _, entry := range entries {
		if entry.Kind == kindSetting && entry.ID == "toolbars" {
			return false, nil
		}
	}
	var legacy struct {
		Toolbars *savedToolbars `json:"toolbars"`
		Position string         `json:"terminal_toolbar_position"`
		Size     string         `json:"terminal_toolbar_size"`
	}
	if len(local) == 0 || json.Unmarshal(local, &legacy) != nil {
		return false, nil // the ordinary reader reports malformed local data
	}
	position, size := terminalToolbarPositionFromString(legacy.Position), terminalToolbarSizeFromString(legacy.Size)
	if !legacy.Toolbars.specified() && position == terminalToolbarRight && size == terminalToolbarSmall {
		return false, nil // untouched defaults must not outrank a cloud layout
	}
	if _, _, err := joinState(entries, local); err != nil {
		return false, nil // do not migrate a configuration the reader will reject
	}
	if legacy.Toolbars == nil {
		legacy.Toolbars = &savedToolbars{}
	}
	if legacy.Toolbars.Position == "" {
		legacy.Toolbars.Position = position.String()
	}
	if legacy.Toolbars.Size == "" {
		legacy.Toolbars.Size = size.String()
	}
	data, err := json.Marshal(legacy.Toolbars)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(recordPayload{Data: data})
	if err != nil {
		return false, err
	}
	key := configstore.Key{Kind: kindSetting, ID: "toolbars"}
	_, err = store.Commit(configstore.Change{
		Writer: configWriter(), Put: []configstore.Entry{{Kind: key.Kind, ID: key.ID, Payload: payload}},
		Expected: map[configstore.Key][]byte{key: nil},
	})
	if errors.Is(err, configstore.ErrConflict) {
		return true, nil // another window migrated it first; read its version
	}
	return err == nil, err
}
