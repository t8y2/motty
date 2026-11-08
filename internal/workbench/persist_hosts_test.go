package workbench

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"motty/internal/configstore"
	"motty/internal/sshconn"
)

// stateWithHosts is a first-run state holding count saved hosts, with the
// catalog already written to disk.
func stateWithHosts(t testing.TB, count int) *State {
	t.Helper()
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	state := newTestState(t, options)
	for index := 0; index < count; index++ {
		host := Host{
			ID: state.nextHostID, Label: fmt.Sprintf("host %02d", index),
			Address: fmt.Sprintf("h%02d.example.com", index), User: "me", Port: uint16(22 + index),
		}
		state.nextHostID++
		host.finish()
		state.hosts = append(state.hosts, host)
	}
	state.save()
	return state
}

func hostEntryWithOrd(t testing.TB, id, label string, ord int64) configstore.Entry {
	t.Helper()
	raw, err := json.Marshal(savedHost{ID: id, Label: label})
	if err != nil {
		t.Fatal(err)
	}
	return configstore.Entry{Kind: kindHost, ID: id, Payload: recordPayloadJSON(t, ord, raw)}
}

// cloneSavedHost copies the parts savedHost shares with its source, so a test
// can change one leaf without touching the original.
func cloneSavedHost(host savedHost) savedHost {
	clone := host
	clone.Tags = slices.Clone(host.Tags)
	if host.Proxy != nil {
		proxy := *host.Proxy
		clone.Proxy = &proxy
	}
	return clone
}

// TestSameSavedHostNoticesEveryField walks savedHost's fields so a field added
// later cannot quietly escape the comparison the host cache relies on.
func TestSameSavedHostNoticesEveryField(t *testing.T) {
	base := savedHost{
		Color: "#3b82f6", ID: "host_1", Label: "web", Group: "prod", Tags: []string{"a", "b"}, Icon: "server",
		Address: "web.example.com", User: "deploy", Port: 2222, System: "linux",
		savedTransport: savedTransport{
			Protocol: "vnc", Line: "115200 8N1", Flow: "rtscts",
			Proxy:      &savedOutboundProxy{Kind: "socks5", Address: "proxy.example.com:1080", User: "pu", Bypass: "localhost"},
			Algorithms: "compatible", AlgorithmList: "a,b", Authentication: "key", Identity: "SHA256:abc",
			ForwardX11: true, AgentForward: true, NoHistory: true, Transfer: "sftp", ViaSSH: true, Clipboard: true,
		},
	}
	if !sameSavedHost(base, cloneSavedHost(base)) {
		t.Fatal("an unchanged host compared as changed")
	}
	if sameSavedHost(base, savedHost{ID: base.ID}) && base.Label != "" {
		t.Fatal("a host with only its id compared as unchanged")
	}

	var leaves []struct {
		indexes []int
		name    string
	}
	var gather func(value reflect.Value, indexes []int, name string)
	gather = func(value reflect.Value, indexes []int, name string) {
		if value.Kind() == reflect.Struct {
			for index := 0; index < value.NumField(); index++ {
				child := append(append([]int(nil), indexes...), index)
				gather(value.Field(index), child, name+"."+value.Type().Field(index).Name)
			}
			return
		}
		leaves = append(leaves, struct {
			indexes []int
			name    string
		}{indexes, name})
	}
	gather(reflect.ValueOf(base), nil, "savedHost")

	for _, leaf := range leaves {
		other := cloneSavedHost(base)
		target := reflect.ValueOf(&other).Elem()
		for _, index := range leaf.indexes[:len(leaf.indexes)-1] {
			target = target.Field(index)
		}
		last := target.Field(leaf.indexes[len(leaf.indexes)-1])
		switch last.Kind() {
		case reflect.String:
			last.SetString(last.String() + "-other")
		case reflect.Uint16:
			last.SetUint(last.Uint() + 1)
		case reflect.Bool:
			last.SetBool(!last.Bool())
		case reflect.Slice:
			changed := reflect.MakeSlice(last.Type(), last.Len()+1, last.Len()+1)
			reflect.Copy(changed, last)
			last.Set(changed)
		case reflect.Ptr:
			last.Set(reflect.ValueOf(&savedOutboundProxy{Kind: "other"}))
		default:
			t.Fatalf("%s: unsupported field kind %s", leaf.name, last.Kind())
		}
		if sameSavedHost(base, other) {
			t.Fatalf("%s changed but sameSavedHost did not notice", leaf.name)
		}
	}

	withoutProxy := cloneSavedHost(base)
	withoutProxy.Proxy = nil
	if sameSavedHost(base, withoutProxy) {
		t.Fatal("a dropped proxy compared as unchanged")
	}
}

func TestReorderingHostsRepairsDuplicateOrdinals(t *testing.T) {
	saved, base, err := joinState([]configstore.Entry{
		hostEntryWithOrd(t, "a", "A", 10),
		hostEntryWithOrd(t, "b", "B", 10),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved.Hosts[0], saved.Hosts[1] = saved.Hosts[1], saved.Hosts[0]

	entries, nextOrd, err := appendHostEntries(nil, base, nil, saved.Hosts, base.maxOrd)
	if err != nil {
		t.Fatal(err)
	}
	if nextOrd <= 10 {
		t.Fatalf("next ordinal = %d, want repaired range above duplicate 10", nextOrd)
	}
	reloaded, _, err := joinState(entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{reloaded.Hosts[0].ID, reloaded.Hosts[1].ID}; !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("reloaded order = %v", got)
	}
}

func TestFullSavePersistsReorderedHosts(t *testing.T) {
	saved, base, err := joinState([]configstore.Entry{
		hostEntryWithOrd(t, "a", "A", 10),
		hostEntryWithOrd(t, "b", "B", 20),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved.Hosts[0], saved.Hosts[1] = saved.Hosts[1], saved.Hosts[0]

	entries, _, _, err := splitState(saved, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := joinState(entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{reloaded.Hosts[0].ID, reloaded.Hosts[1].ID}; !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("reloaded order = %v", got)
	}
}

// TestSavingOneHostKeepsEveryOtherEncodedRecord proves the host cache: only the
// edited host's record is encoded again, the others keep the bytes already in
// the store.
func TestSavingOneHostKeepsEveryOtherEncodedRecord(t *testing.T) {
	state := stateWithHosts(t, 20)
	// The first save after the load encodes the whole catalog; the next one
	// fills the per-host cache, which is what this test then measures.
	state.hosts[0].Label += " warm"
	state.save()
	pointers := func() map[string]uintptr {
		out := map[string]uintptr{}
		for key, payload := range state.configBase.payloads {
			if key.Kind == kindHost {
				out[key.ID] = reflect.ValueOf(payload).Pointer()
			}
		}
		return out
	}
	before := pointers()
	edited := state.hosts[3].CatalogID
	state.hosts[3].Label = "renamed"
	state.save()
	after := pointers()
	if len(after) != len(before) {
		t.Fatalf("record count changed from %d to %d", len(before), len(after))
	}
	for id, pointer := range before {
		if id == edited {
			if after[id] == pointer {
				t.Fatalf("the edited host %s was not re-encoded", id)
			}
			continue
		}
		if after[id] != pointer {
			t.Fatalf("host %s was re-encoded although it did not change", id)
		}
	}
}

// TestSavingHostsMatchesAReopenedStore runs a sequence of host edits through the
// incremental writer and checks the store a second window would read.
func TestSavingHostsMatchesAReopenedStore(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	state := newTestState(t, options)
	for index := 0; index < 12; index++ {
		host := Host{ID: state.nextHostID, Label: fmt.Sprintf("host %02d", index), Address: fmt.Sprintf("h%d.example.com", index), User: "me", Port: 22}
		state.nextHostID++
		host.finish()
		state.hosts = append(state.hosts, host)
	}
	state.save()

	addHost := func(label, address string) {
		host := Host{ID: state.nextHostID, Label: label, Address: address, User: "me", Port: 22}
		state.nextHostID++
		host.finish()
		state.hosts = append(state.hosts, host)
	}
	steps := []struct {
		name  string
		apply func()
	}{
		{"rename", func() { state.hosts[2].Label = "renamed" }},
		{"tags", func() { state.hosts[4].Tags = "web, prod" }},
		{"proxy", func() {
			state.hosts[5].Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "proxy.example.com:1080", user: "pu", bypass: "localhost"}
		}},
		{"add", func() { addHost("added", "added.example.com") }},
		{"delete", func() { state.hosts = append(state.hosts[:1], state.hosts[2:]...) }},
		{"port", func() { state.hosts[0].Port = 2222 }},
		{"two at once", func() {
			state.hosts[1].Label = "first"
			state.hosts[2].User = "other"
		}},
		{"setting", func() { state.terminalScrollback = 10000 }},
	}
	for _, step := range steps {
		step.apply()
		state.save()

		saved, _, err := loadState(options.StatePath)
		if err != nil {
			t.Fatalf("%s: reopen: %v", step.name, err)
		}
		if len(saved.Hosts) != len(state.hosts) {
			t.Fatalf("%s: store has %d hosts, state has %d", step.name, len(saved.Hosts), len(state.hosts))
		}
		want := map[string]string{}
		for _, host := range state.snapshot().Hosts {
			raw, err := json.Marshal(host)
			if err != nil {
				t.Fatal(err)
			}
			want[host.ID] = string(raw)
		}
		for _, host := range saved.Hosts {
			raw, err := json.Marshal(host)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != want[host.ID] {
				t.Fatalf("%s: host %s read back as %s, want %s", step.name, host.ID, raw, want[host.ID])
			}
		}
	}
	if saved, _, err := loadState(options.StatePath); err != nil || saved.TerminalScrollback != 10000 {
		t.Fatalf("setting did not reach the store: %v %+v", err, saved.TerminalScrollback)
	}
}

// TestAFailedHostSaveIsWrittenByTheRetry builds the host records without
// committing them (a failed commit leaves the base unchanged) and checks the
// retry still writes the edit rather than the record the base still holds.
func TestAFailedHostSaveIsWrittenByTheRetry(t *testing.T) {
	state := stateWithHosts(t, 3)
	state.hosts[0].Label += " warm"
	state.save()
	state.hosts[1].Label = "edited"
	snapshot := state.snapshot()
	entity, ok := entityByField("hosts")
	if !ok {
		t.Fatal("no hosts entity")
	}
	base := state.configBase
	nextOrd := base.maxOrd
	if _, _, err := appendEntityEntries(nil, base, state.configDefaults, entity, snapshot.Hosts, nextOrd); err != nil {
		t.Fatal(err)
	}
	// The first attempt was not committed; the base still describes the old
	// records. The retry must not take the stale payload from it.
	retry, _, err := appendEntityEntries(nil, base, state.configDefaults, entity, snapshot.Hosts, nextOrd)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range retry {
		if entry.ID != state.hosts[1].CatalogID {
			continue
		}
		var payload recordPayload
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(payload.Data), "edited") {
			t.Fatalf("retry wrote %s, the edit is missing", payload.Data)
		}
		return
	}
	t.Fatalf("retry has no record for %s", state.hosts[1].CatalogID)
}

// TestDuplicateHostIDsStillUseTheGenericEncoder checks the fallback that keeps
// the record keys of a damaged catalog working.
func TestReorderingHostsPersistsBySwappingTheirOrdinals(t *testing.T) {
	state := stateWithHosts(t, 4)
	state.hosts[0], state.hosts[2] = state.hosts[2], state.hosts[0]
	state.save()

	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"host 02", "host 01", "host 00", "host 03"}
	got := make([]string, len(saved.Hosts))
	for index := range saved.Hosts {
		got[index] = saved.Hosts[index].Label
	}
	if !slices.Equal(got, want) {
		t.Fatalf("reopened order = %v, want %v", got, want)
	}
}

func TestDuplicateHostIDsStillUseTheGenericEncoder(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	state := newTestState(t, options)
	for index := 0; index < 2; index++ {
		host := Host{ID: state.nextHostID, Label: fmt.Sprintf("host %d", index), Address: "same.example.com", User: "me", Port: 22}
		state.nextHostID++
		host.finish()
		state.hosts = append(state.hosts, host)
	}
	state.save()
	// Force a duplicate catalog id, as a damaged or imported state may hold.
	state.hosts[1].CatalogID = state.hosts[0].CatalogID
	state.hosts[1].Label = "duplicate"
	state.save()
	store, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, _ := store.Records()
	seen := map[string]bool{}
	for _, record := range records {
		if record.Kind != kindHost || record.Deleted {
			continue
		}
		seen[record.ID] = true
	}
	if !seen[state.hosts[0].CatalogID] || !seen[state.hosts[0].CatalogID+"#2"] {
		t.Fatalf("duplicate host records = %v", seen)
	}
}

// TestAcceptLocalKeepsKnownOrdinalsOfNonHostRecords guards the ordinal this
// base already knows for a record kind whose payload carries none: decoding
// would read a zero and lose the position the store had.
func TestAcceptLocalKeepsKnownOrdinalsOfNonHostRecords(t *testing.T) {
	key := configstore.Key{Kind: "snippet", ID: "one"}
	base := newConfigBase()
	base.ords[key] = 7
	settings := configstore.Key{Kind: kindSetting, ID: "terminalOption"}
	base.ords[settings] = 3

	data, err := json.Marshal("snippet value")
	if err != nil {
		t.Fatal(err)
	}
	base.acceptLocal([]configstore.Entry{
		// No Ord: exactly what appendSettingEntry and the generic path write for
		// kinds that have no position of their own.
		{Kind: kindSetting, ID: "terminalOption", Payload: recordPayloadJSON(t, 0, data)},
	}, localWrite{})
	if got := base.ords[settings]; got != 3 {
		t.Fatalf("known ordinal of a setting became %d", got)
	}
}

// TestAHostOrdinalStillFollowsTheRecord proves the relaxation above stays
// narrow: a host's position is the one the save just wrote.
func TestAHostOrdinalStillFollowsTheRecord(t *testing.T) {
	key := configstore.Key{Kind: kindHost, ID: "a"}
	base := newConfigBase()
	base.ords[key] = 2
	raw, err := json.Marshal(savedHost{ID: "a", Label: "A"})
	if err != nil {
		t.Fatal(err)
	}
	base.acceptLocal([]configstore.Entry{{Kind: kindHost, ID: "a", Payload: recordPayloadJSON(t, 9, raw)}}, localWrite{})
	if got := base.ords[key]; got != 9 {
		t.Fatalf("host ordinal = %d, want the written position", got)
	}
}

// TestAnUnchangedHostOrderKeepsEveryOrdinal proves a save that moves nothing
// leaves the catalog positions alone, so a caller that hands over a slice in a
// derived order cannot rewrite the order the user chose.
func TestAnUnchangedHostOrderKeepsEveryOrdinal(t *testing.T) {
	saved, base, err := joinState([]configstore.Entry{
		hostEntryWithOrd(t, "a", "A", 10),
		hostEntryWithOrd(t, "b", "B", 20),
		hostEntryWithOrd(t, "c", "C", 30),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A middle host edited: the slice order still matches the ordinals.
	saved.Hosts[1].Label = "B edited"
	entries, _, err := appendHostEntries(nil, base, nil, saved.Hosts, base.maxOrd)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"a": 10, "b": 20, "c": 30}
	got := map[string]int64{}
	for _, entry := range entries {
		var payload recordPayload
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		got[entry.ID] = payload.Ord
	}
	for id, ord := range want {
		if got[id] != ord {
			t.Fatalf("ordinals = %v, want %v", got, want)
		}
	}
}
