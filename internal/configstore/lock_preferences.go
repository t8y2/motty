package configstore

import (
	"context"
	"database/sql"
	"encoding/json"
)

// These are the only profile-local preferences needed to paint the lock screen.
// Encoding a typed subset keeps hosts, groups, paths and future fields private.
type LockPreferences struct {
	Language          string `json:"language,omitempty"`
	Theme             string `json:"theme,omitempty"`
	Corners           string `json:"corners,omitempty"`
	IconColor         string `json:"icon_color,omitempty"`
	TextScale         string `json:"text_scale,omitempty"`
	InterfaceTextSize int    `json:"interface_text_size,omitempty"`
	InterfaceFont     string `json:"interface_font,omitempty"`
	InterfaceFontMain int    `json:"interface_font_main_weight,omitempty"`
	InterfaceFontBold int    `json:"interface_font_bold_weight,omitempty"`
}

const lockPreferencesMeta = "lock-screen-preferences"

func writeLockPreferences(ctx context.Context, tx *sql.Tx, key string, value []byte) error {
	if key != "state" {
		return nil
	}
	var prefs LockPreferences
	if len(value) > 0 {
		if err := json.Unmarshal(value, &prefs); err != nil {
			return err
		}
	}
	b, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	return setMeta(ctx, tx, lockPreferencesMeta, string(b))
}

func ReadLockPreferences(path string, format int) (LockPreferences, error) {
	var prefs LockPreferences
	ctx := context.Background()
	db, err := openReadOnly(ctx, path, format)
	if err != nil {
		return prefs, err
	}
	defer db.Close()
	b, err := metaValue(ctx, db, lockPreferencesMeta)
	if err != nil || b == "" {
		return prefs, err
	}
	err = json.Unmarshal([]byte(b), &prefs)
	return prefs, err
}
