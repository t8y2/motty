package workbench

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"motty/internal/configstore"
	"motty/internal/monitoring"
)

// Every field of the saved state is either shared between devices or belongs to
// this one. A field added without deciding which would silently be one or the
// other, so each must be named here or in the record tables.
func TestEverySavedStateFieldIsSharedOrKeptOnThisDevice(t *testing.T) {
	ownDevice := map[string]bool{
		"monitor_panels": true, "monitor_active": true,
		"language": true, "theme": true, "terminal_font_size": true, "terminal_font_face": true, "terminal_font_family": true, "terminal_font_fallbacks": true,
		"restore_connect": true, "share_connections": true, "update_check": true, "close_action": true, "update_skipped": true, "ai_panel_width": true,
		"workspace": true, "active_session": true, "shortcuts": true, "identity_files": true, "agent_hosts": true, "sessions": true, "deck": true, "sync": true,
		"host_view": true, "background": true, "snippet_shortcuts": true,
		"sftp_local_bookmarks": true,
		"sftp_columns":         true, "sftp_download_directory": true, "default_editor": true,
		"toolbars":    true,
		"local_shell": true, "ssh_agent": true, "ssh_agent_path": true,
		"window_x": true, "window_y": true, "window_width": true, "window_height": true,
		// The session journal switch belongs to this computer: another device
		// keeps its own choice, and an older build must not silently drop it.
		"session_logs_off": true,
	}
	entity := map[string]bool{}
	for _, field := range entityFields {
		entity[field.field] = true
	}
	kind := reflect.TypeOf(savedState{})
	for index := 0; index < kind.NumField(); index++ {
		name, _, _ := strings.Cut(kind.Field(index).Tag.Get("json"), ",")
		classes := 0
		for _, in := range []bool{ownDevice[name], entity[name], syncedSettings[name], stateVersionFields[name]} {
			if in {
				classes++
			}
		}
		if classes != 1 {
			t.Errorf("saved state field %q is in %d of: this device, a list of records, a shared setting, a version; it must be in exactly one", name, classes)
		}
	}
}

func richState() savedState {
	return savedState{
		Version: stateFileVersion, HostCatalogVersion: hostCatalogSchemaVersion, ShortcutVersion: shortcutSchemaVersion,
		Language: "zh-CN", Theme: "dark", LocalShell: "/opt/local-shell/bin/shell", TerminalFontSize: 15, TerminalScrollback: 5000, TerminalPadding: "roomy",
		SFTPParallel: 2, SFTPTerminalFollow: true, CommandHistory: true,
		Hosts: []savedHost{
			{ID: "host_b", Label: "Beta", Address: "b.example.com", User: "me", Port: 22, Group: "prod"},
			{ID: "host_a", Label: "Alpha", Address: "a.example.com", User: "me", Port: 2222, Group: "dev"},
		},
		HostGroups:    []savedHostGroup{{Name: "prod", Collapsed: true}, {Name: "dev"}},
		Snippets:      []savedSnippet{{Name: "update", Command: "apt update"}, {Name: "update", Command: "brew update"}, {Name: "zzz", Command: "ls"}},
		Highlights:    []savedHighlight{{Pattern: "ERROR", Color: "red"}, {Pattern: "warn", CaseSensitive: true, Color: "amber"}},
		Forwards:      []savedForward{{Label: "db", HostAddress: "b.example.com", HostUser: "me", HostPort: 22, LocalPort: 5432, DestinationHost: "localhost", DestinationPort: 5432}},
		Proxies:       []savedProxy{{TargetAddress: "a.example.com", TargetUser: "me", TargetPort: 2222, JumpAddress: "b.example.com", JumpUser: "me", JumpPort: 22}},
		Autofill:      []savedAutofill{{ID: "r1", Name: "sudo", Pattern: "password", Send: "vault"}},
		SFTPBookmarks: []savedBookmark{{Host: "host_a", Path: "/var/log"}},
		Shortcuts:     []savedShortcut{{Action: "new-tab", Key: "t", Modifiers: 1}},
		OutboundProxy: &savedOutboundProxy{Kind: "socks5", Address: "127.0.0.1:1080"},
		IdentityFiles: []string{"/Users/me/.ssh/id_ed25519"},
		Workspace:     "settings",
	}
}

func jsonOf(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAStateComesBackFromItsRecordsAsItWas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	want := richState()
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("%+v, %v", report, err)
	}
	if jsonOf(t, got) != jsonOf(t, want) {
		t.Fatalf("the state changed on the way through the store:\n got %s\nwant %s", jsonOf(t, got), jsonOf(t, want))
	}
}

func TestFormatNineUpgradePreservesHostsAndMonitoringAndBlocksOlderWriters(t *testing.T) {
	for _, panels := range []bool{false, true} {
		t.Run(map[bool]string{false: "upstream", true: "monitoring"}[panels], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.db")
			want := richState()
			if panels {
				want.MonitorPanels = []monitoring.Panel{{ID: "panel", Name: "Production", Hosts: []string{"host_a"}, Interval: 5, Retention: 60}}
				want.MonitorActive = "panel"
			}
			entries, local, err := splitState(want, newConfigBase(), nil)
			if err != nil {
				t.Fatal(err)
			}
			old, err := configstore.Open(path, 9)
			if err != nil {
				t.Fatal(err)
			}
			_, err = old.Commit(configstore.Change{Put: entries, Local: map[string][]byte{localKey: local}})
			if err := errors.Join(err, old.Close()); err != nil {
				t.Fatal(err)
			}
			got, report, err := loadState(path)
			if err != nil || report.outcome != loadOK {
				t.Fatalf("upgrade: %+v, %v", report, err)
			}
			if !reflect.DeepEqual(got.Hosts, want.Hosts) || !reflect.DeepEqual(got.MonitorPanels, want.MonitorPanels) || got.MonitorActive != want.MonitorActive {
				t.Fatalf("upgrade changed hosts or monitoring: %+v", got)
			}
			if err := saveState(path, got); err != nil {
				t.Fatal(err)
			}
			if _, _, err := configstore.ReadOnly(path, 9); !errors.Is(err, configstore.ErrTooNew) {
				t.Fatalf("a format-9 build could overwrite the upgraded configuration: %v", err)
			}
		})
	}
}

func TestFormatTenUpgradeKeepsTheLocalSessionLogSwitchAndBlocksOlderWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	want := richState()
	want.SessionLogsOff = true
	entries, local, err := splitState(want, newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := configstore.Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Commit(configstore.Change{Put: entries, Local: map[string][]byte{localKey: local}})
	if err := errors.Join(err, old.Close()); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("upgrade: %+v, %v", report, err)
	}
	if !got.SessionLogsOff {
		t.Fatal("the upgrade lost the local session-log switch")
	}
	if err := saveState(path, got); err != nil {
		t.Fatal(err)
	}
	if _, _, err := configstore.ReadOnly(path, 10); !errors.Is(err, configstore.ErrTooNew) {
		t.Fatalf("a format-10 build could overwrite the upgraded configuration: %v", err)
	}
}

func TestSessionLogCaptureDefaultsOnForAStateThatPredatesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	// A format-10 state never wrote the switch: absent must mean capture on.
	want := richState()
	entries, local, err := splitState(want, newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(configstore.Change{Put: entries, Local: map[string][]byte{localKey: local}})
	if err := errors.Join(err, store.Close()); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if got.SessionLogsOff {
		t.Fatal("a state that never chose the switch disabled capture")
	}
}

func TestOnlyWhatIsSharedBecomesARecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	if err := saveState(path, richState()); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, _ := store.Records()
	for _, record := range records {
		text := string(record.Payload)
		for _, own := range []string{"zh-CN", "dark", "settings", "id_ed25519", "local-shell", `"collapsed"`} {
			if strings.Contains(text, own) {
				t.Errorf("%s %s carries %q, which belongs to this device", record.Kind, record.ID, own)
			}
		}
	}
}

func TestChangingOneSettingMovesOnlyItsRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	state := richState()
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	read := func() map[configstore.Key]configstore.Stamp {
		store, err := configstore.Open(path, configFormat)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		records, _ := store.Records()
		stamps := map[configstore.Key]configstore.Stamp{}
		for _, record := range records {
			stamps[configstore.Key{Kind: record.Kind, ID: record.ID}] = record.Stamp
		}
		return stamps
	}
	before := read()
	state.TerminalScrollback = 10000
	state.Language = "en" // this device's own: not a record
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	after := read()
	var moved []string
	for key, stamp := range after {
		if before[key] != stamp {
			moved = append(moved, key.Kind+"/"+key.ID)
		}
	}
	if !reflect.DeepEqual(moved, []string{"setting/terminal_scrollback"}) {
		t.Fatalf("records that moved: %v", moved)
	}
}

func TestDeletingAnItemRemovesItsRecordAndLeavesTheOthersWhereTheyWere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	state := richState()
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	state.Hosts = state.Hosts[1:] // Beta goes
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	got, _, _ := loadState(path)
	if len(got.Hosts) != 1 || got.Hosts[0].ID != "host_a" || len(got.Snippets) != 3 {
		t.Fatalf("hosts %+v, %d snippets", got.Hosts, len(got.Snippets))
	}
}

// Two windows share one store. A window that has not heard of what the other
// added must not delete it when it saves, nor write over what the other changed.
func TestAWindowThatIsBehindDoesNotUndoTheOthersWork(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	first := newTestState(t, options)
	second := newTestState(t, options)

	first.hosts = append(first.hosts, Host{ID: 1, Label: "Added by first", Address: "first.example.com", User: "me", Port: 22})
	first.hosts[0].finish()
	first.save()

	second.terminalScrollback = 10000 // a different change, made without seeing the first
	second.save()

	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Hosts) != 1 || saved.Hosts[0].Label != "Added by first" {
		t.Fatalf("the second window's save lost the first's host: %+v", saved.Hosts)
	}
	if saved.TerminalScrollback != 10000 {
		t.Fatalf("the second window's own change was not saved: %d", saved.TerminalScrollback)
	}
}

// A computer that has only ever run on its defaults has changed nothing, so it
// has nothing to claim; if it did, its defaults would outrank the settings of the
// computers that were set up before it.
func TestAFreshComputerWritesNoRecordsForItsDefaults(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	state := newTestState(t, options)
	state.save()
	records := func() []configstore.Record {
		store, err := configstore.Open(options.StatePath, configFormat)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		all, _ := store.Records()
		return all
	}
	if got := records(); len(got) != 0 {
		t.Fatalf("a computer on its defaults wrote %d records: %v", len(got), got)
	}

	state.terminalScrollback = 10000
	state.save()
	got := records()
	if len(got) != 1 || got[0].Kind != kindSetting || got[0].ID != "terminal_scrollback" {
		t.Fatalf("one changed setting made %v", got)
	}

	// Putting it back is a change too, and has to reach the other computers.
	state.terminalScrollback = scrollbackLines
	state.save()
	got = records()
	if len(got) != 1 || got[0].Deleted || !strings.Contains(string(got[0].Payload), "5000") {
		t.Fatalf("a setting put back to its default is %v", got)
	}
}

func benchHostsState(b *testing.B, count int) *State {
	b.Helper()
	options := freshOptions(b)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	state := newTestState(b, options)
	for index := 0; index < count; index++ {
		host := Host{ID: state.nextHostID, Label: "host " + strings.Repeat("x", index%7), Address: "h" + string(rune('a'+index%26)) + ".example.com", User: "me", Port: uint16(22 + index)}
		state.nextHostID++
		host.finish()
		state.hosts = append(state.hosts, host)
	}
	state.save()
	return state
}

// Saving happens on the window's thread after nearly every change, so what it
// costs when nothing shared changed (most saves) and when one host did matters.
func BenchmarkSaveNothingShared(b *testing.B) {
	state := benchHostsState(b, 300)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.save()
	}
}

func BenchmarkSaveOneHostChanged(b *testing.B) {
	state := benchHostsState(b, 300)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.hosts[10].Label = "renamed " + strings.Repeat("y", i%5)
		state.save()
	}
}

func TestChangedSavedFieldsRecognizesOneFieldChanges(t *testing.T) {
	previous := richState()
	current := richState()
	current.Hosts[1].Label = "renamed"
	changes := changedSavedFields(previous, current)
	if len(changes) != 1 || changes[0].name != "hosts" {
		t.Fatalf("host change = %+v", changes)
	}
	current = richState()
	current.Language = "en"
	changes = changedSavedFields(previous, current)
	if len(changes) != 1 || changes[0].name != "language" {
		t.Fatalf("setting change = %+v", changes)
	}
	current = richState()
	current.HostGroups[0].Collapsed = false
	changes = changedSavedFields(previous, current)
	if len(changes) != 1 || changes[0].name != "host_groups" {
		t.Fatalf("group view change = %+v", changes)
	}
	current = richState()
	current.Hosts[1].Label = "renamed"
	current.Language = "en"
	if changes = changedSavedFields(previous, current); len(changes) != 2 {
		t.Fatalf("mixed change = %+v", changes)
	}
}

// Saving one setting is a single-field change; it should not walk the whole
// state. The host benchmark next to it covers the other common single-field
// change.
func BenchmarkSaveOneSettingChanged(b *testing.B) {
	state := benchHostsState(b, 300)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		state.connectionTimeout = time.Duration(10+index%2) * time.Second
		state.save()
	}
}

// Moving a device-only value (the assistant panel width) should only rewrite
// this device's own JSON object, not the shared records.
func BenchmarkSaveOneDeviceValue(b *testing.B) {
	state := benchHostsState(b, 300)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		state.ai.panelWidth = 300 + float32(index%2)
		state.save()
	}
}
