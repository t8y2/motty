package workbench

import (
	"encoding/json"
	layoutcore "motty/internal/workspace"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"motty/internal/configstore"
)

func TestWorkspaceTemplatesRoundTripInStableOrder(t *testing.T) {
	path := testConfigPath(t)
	production := mustWorkspaceTemplate(t, "Production", []string{"host_b", "host_a"}, 2)
	database := mustWorkspaceTemplate(t, "Database", []string{"host_c"}, 1)
	want := savedState{WorkspaceTemplates: []WorkspaceTemplate{database, production}}
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if !reflect.DeepEqual(got.WorkspaceTemplates, want.WorkspaceTemplates) {
		t.Fatalf("templates = %+v, want %+v", got.WorkspaceTemplates, want.WorkspaceTemplates)
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, err := store.Records()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, record := range records {
		if record.Kind == kindWorkspaceTemplate && !record.Deleted {
			ids = append(ids, record.ID)
		}
	}
	if len(ids) != 2 || !slices.Contains(ids, production.ID) || !slices.Contains(ids, database.ID) {
		t.Fatalf("records = %v", ids)
	}
}

func TestAMalformedWorkspaceTemplateDoesNotBlockTheOthers(t *testing.T) {
	path := testConfigPath(t)
	valid := mustWorkspaceTemplate(t, "Good", []string{"host_a"}, 1)
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	invalidID, err := newWorkspaceTemplateID()
	if err != nil {
		t.Fatal(err)
	}
	var invalid WorkspaceTemplate
	invalid = WorkspaceTemplate{ID: invalidID, Name: "Bad", HostIDs: []string{"host_a", "host_a"}, Layout: layoutcore.GridSpec(1)}
	bad, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{Put: []configstore.Entry{
		{Kind: kindWorkspaceTemplate, ID: valid.ID, Payload: recordPayloadJSON(t, 1, raw)},
		{Kind: kindWorkspaceTemplate, ID: invalid.ID, Payload: recordPayloadJSON(t, 2, bad)},
	}}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if !reflect.DeepEqual(got.WorkspaceTemplates, []WorkspaceTemplate{valid}) {
		t.Fatalf("templates = %+v", got.WorkspaceTemplates)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the malformed record moved the database aside")
	}
}

func TestLoadedWorkspaceTemplateKeepsItsNormalizedValues(t *testing.T) {
	_, options := newWorld(t)
	valid := mustWorkspaceTemplate(t, "Good", []string{"host_a"}, 1)
	valid.Name = " Padded "
	valid.HostIDs = []string{" host_a "}
	if err := saveState(options.StatePath, savedState{Hosts: sampleHosts, WorkspaceTemplates: []WorkspaceTemplate{valid}}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.templates) != 1 || state.templates[0].Name != "Padded" ||
		!slices.Equal(state.templates[0].HostIDs, []string{"host_a"}) {
		var validation error
		if len(state.templates) == 1 {
			validation = state.templates[0].Validate()
		}
		t.Fatalf("restored template = %#v, validation=%v", state.templates, validation)
	}
}

func TestAnOlderBuildKeepsWorkspaceTemplateRecords(t *testing.T) {
	path := testConfigPath(t)
	template := mustWorkspaceTemplate(t, "Production", []string{"host_a"}, 1)
	if err := saveState(path, savedState{WorkspaceTemplates: []WorkspaceTemplate{template}}); err != nil {
		t.Fatal(err)
	}
	entries, local, err := configstore.ReadOnly(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	base := newConfigBase()
	var saved savedState
	for _, entry := range entries {
		if entry.Kind != kindWorkspaceTemplate {
			continue
		}
		base.payloads[configstore.Key{Kind: entry.Kind, ID: entry.ID}] = entry.Payload
	}
	saved.Version, saved.HostCatalogVersion, saved.ShortcutVersion = stateFileVersion, hostCatalogSchemaVersion, shortcutSchemaVersion
	if _, _, _, err := splitState(saved, base, nil); err == nil {
		base.local = local[localKey]
		store, err := configstore.Open(path, configFormat)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Commit(configstore.Change{Put: []configstore.Entry{
			{Kind: kindSetting, ID: "terminal_scrollback", Payload: []byte(`{"d":10000}`)},
		}}); err != nil {
			store.Close()
			t.Fatal(err)
		}
		store.Close()
	}
	// A full save from a state that never decoded the new field must not delete
	// the record. This is the older build's behavior through the unknown-kind
	// retention path.
	got, _, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.WorkspaceTemplates) != 1 || got.WorkspaceTemplates[0].ID != template.ID {
		t.Fatalf("templates after older save = %+v", got.WorkspaceTemplates)
	}
}

func mustWorkspaceTemplate(t testing.TB, name string, hosts []string, columns uint8) WorkspaceTemplate {
	t.Helper()
	template, err := NewWorkspaceTemplate(name, hosts, columnsSpec(columns))
	if err != nil {
		t.Fatal(err)
	}
	return template
}

func testConfigPath(t testing.TB) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.db")
}

func recordPayloadJSON(t testing.TB, ord int64, data []byte) []byte {
	t.Helper()
	payload, err := json.Marshal(recordPayload{Ord: ord, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
