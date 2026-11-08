//go:build !dev

package workbench

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"motty/internal/configstore"
)

func TestReleaseRestoreIgnoresDevelopmentWorkspace(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	if err := saveState(options.StatePath, savedState{
		HostCatalogVersion: hostCatalogSchemaVersion,
		ShortcutVersion:    shortcutSchemaVersion,
		Hosts: []savedHost{{
			ID: "release-host", Label: "Release Host", Address: "release.example.com", User: "me", Port: 22,
		}},
		Workspace:     "terminal",
		ActiveSession: 1,
		Sessions: []savedSession{{
			Label: "Release Session", Address: "release.example.com", User: "me", Port: 22, SavedHost: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{Local: map[string][]byte{
		"development_workspace": []byte(`{"workspace":"settings","sessions":[]}`),
	}}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	store.Close()

	state := newTestState(t, options)
	if len(state.sessions) != 1 || state.sessions[0].Title != "Release Session" || state.activeSection != sectionTerminal {
		t.Fatalf("release workspace = sessions %+v, section %d", state.sessions, state.activeSection)
	}
}

func TestReleaseIncrementalSavePreservesDamagedDevelopmentWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	saveReleaseState(t, path, savedState{
		Version:            stateFileVersion,
		HostCatalogVersion: hostCatalogSchemaVersion,
		ShortcutVersion:    shortcutSchemaVersion,
		Hosts: []savedHost{{
			ID: "release-host", Label: "Release Host", Address: "release.example.com", User: "me", Port: 22,
		}},
	})
	damaged := []byte(`{"sessions":[`)
	setLocalConfigValue(t, path, developmentWorkspaceKey, damaged)

	store, _, base, _, err := openConfig(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, handled, err := writeChangedFields(store, base, nil, []savedFieldChange{{
		name: "terminal_font_size", value: 17,
	}}); err != nil || !handled {
		t.Fatalf("release incremental save: handled=%t err=%v", handled, err)
	}

	if got := localConfigValue(t, path, developmentWorkspaceKey); string(got) != string(damaged) {
		t.Fatalf("release save changed the development workspace: %s", got)
	}
	var release map[string]json.RawMessage
	if err := json.Unmarshal(localConfigValue(t, path, localKey), &release); err != nil {
		t.Fatal(err)
	}
	if string(release["terminal_font_size"]) != "17" {
		t.Fatalf("release setting was not saved: %s", release["terminal_font_size"])
	}
}
