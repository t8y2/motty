package configstore

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "config.db"), 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func put(kind, id, payload string) Entry { return Entry{Kind: kind, ID: id, Payload: []byte(payload)} }

func TestStoringTheSameStateTwiceWritesNothingTheSecondTime(t *testing.T) {
	store := open(t)
	change := Change{Put: []Entry{put("host", "a", `{"label":"one"}`), put("host", "b", `{"label":"two"}`)}}
	if written, err := store.Commit(change); err != nil || written != 2 {
		t.Fatalf("first commit wrote %d, %v", written, err)
	}
	before, _ := store.Records()
	if written, err := store.Commit(change); err != nil || written != 0 {
		t.Fatalf("second commit wrote %d, %v", written, err)
	}
	after, _ := store.Records()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("an unchanged state moved a stamp:\n%v\n%v", before, after)
	}
	change.Put[0] = put("host", "a", `{"label":"changed"}`)
	if written, _ := store.Commit(change); written != 1 {
		t.Fatalf("one changed record wrote %d", written)
	}
	changed, _ := store.Records()
	if changed[0].Stamp <= before[0].Stamp || changed[1].Stamp != before[1].Stamp {
		t.Fatalf("stamps %v -> %v", before, changed)
	}
}

func TestADeletedRecordLeavesATombstoneAndIsNotLoaded(t *testing.T) {
	store := open(t)
	store.Commit(Change{Put: []Entry{put("host", "a", `1`), put("host", "b", `2`)}})
	if written, _ := store.Commit(Change{Delete: []Key{{"host", "a"}, {"host", "missing"}}}); written != 1 {
		t.Fatalf("deleting one record and one that never existed wrote %d", written)
	}
	if written, _ := store.Commit(Change{Delete: []Key{{"host", "a"}}}); written != 0 {
		t.Fatalf("deleting a deleted record wrote %d", written)
	}
	entries, _, _ := store.Load()
	if len(entries) != 1 || entries[0].ID != "b" {
		t.Fatalf("loaded %v", entries)
	}
	records, _ := store.Records()
	if len(records) != 2 || !records[0].Deleted || records[0].Payload != nil {
		t.Fatalf("records %v", records)
	}
	store.Commit(Change{Put: []Entry{put("host", "a", `3`)}})
	entries, _, _ = store.Load()
	if len(entries) != 2 {
		t.Fatalf("a record put again after its deletion was not loaded: %v", entries)
	}
}

func TestLocalValuesStayOutOfTheRecords(t *testing.T) {
	store := open(t)
	store.Commit(Change{Local: map[string][]byte{"deck": []byte(`{"x":1}`), "gone": []byte("v")}})
	store.Commit(Change{Local: map[string][]byte{"gone": nil}})
	_, local, _ := store.Load()
	if len(local) != 1 || string(local["deck"]) != `{"x":1}` {
		t.Fatalf("local %v", local)
	}
	if records, _ := store.Records(); len(records) != 0 {
		t.Fatalf("local values became records: %v", records)
	}
}

func TestStampsKeepRisingAcrossRestartsAndABackwardsClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.UnixMilli(2_000_000_000_000) }
	store.Commit(Change{Put: []Entry{put("k", "1", `1`)}})
	first, _ := store.Records()
	device := store.Device()
	store.Close()

	store, err = Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.Device() != device {
		t.Fatalf("the device id changed: %s -> %s", device, store.Device())
	}
	store.now = func() time.Time { return time.UnixMilli(1_000_000_000_000) } // the clock went back
	store.Commit(Change{Put: []Entry{put("k", "1", `2`)}})
	second, _ := store.Records()
	if second[0].Stamp <= first[0].Stamp {
		t.Fatalf("a later edit has an earlier stamp: %s then %s", first[0].Stamp, second[0].Stamp)
	}
}

func TestOpeningRefusesWhatIsNotOrCannotBeUnderstood(t *testing.T) {
	dir := t.TempDir()
	notDatabase := filepath.Join(dir, "text.db")
	os.WriteFile(notDatabase, []byte(strings.Repeat("this is not a database ", 20)), 0o600)
	if _, err := Open(notDatabase, 1); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a text file: %v", err)
	}

	path := filepath.Join(dir, "config.db")
	store, err := Open(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err := Open(path, 1); !errors.Is(err, ErrTooNew) {
		t.Fatalf("payloads written by a newer format: %v", err)
	}

	data, _ := os.ReadFile(path)
	binary.BigEndian.PutUint32(data[60:64], schemaVersion+1)
	newer := filepath.Join(dir, "newer.db")
	os.WriteFile(newer, data, 0o600)
	if _, err := Open(newer, 2); !errors.Is(err, ErrTooNew) {
		t.Fatalf("a newer schema: %v", err)
	}
}

func TestTheFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits to check")
	}
	path := filepath.Join(t.TempDir(), "sub", "config.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file %v, %v", info, err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory %v, %v", info, err)
	}
}

func pair(t *testing.T) (a, b *Store) {
	a, b = open(t), open(t)
	a.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	b.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	return a, b
}

func TestTheLaterEditWinsWhicheverWayTheyAreMerged(t *testing.T) {
	a, b := pair(t)
	a.Commit(Change{Put: []Entry{put("host", "x", `"from a"`)}})
	b.now = func() time.Time { return time.UnixMilli(1_700_000_005_000) }
	b.Commit(Change{Put: []Entry{put("host", "x", `"from b"`)}})

	fromA, _ := a.Records()
	fromB, _ := b.Records()
	if result, err := a.Merge(fromB); err != nil || len(result.Applied) != 1 {
		t.Fatalf("a took %d from b, %v", len(result.Applied), err)
	}
	if result, _ := b.Merge(fromA); len(result.Applied) != 0 {
		t.Fatalf("b took an older version: %v", result.Applied)
	}
	entries, _, _ := a.Load()
	if string(entries[0].Payload) != `"from b"` {
		t.Fatalf("a holds %s", entries[0].Payload)
	}
}

func TestADeletionBeatsAnOlderEditAndAnEditBeatsAnOlderDeletion(t *testing.T) {
	a, b := pair(t)
	a.Commit(Change{Put: []Entry{put("snippet", "s", `1`)}})
	records, _ := a.Records()
	b.Merge(records)

	b.now = func() time.Time { return time.UnixMilli(1_700_000_010_000) }
	b.Commit(Change{Delete: []Key{{"snippet", "s"}}})
	a.now = func() time.Time { return time.UnixMilli(1_700_000_005_000) }
	a.Commit(Change{Put: []Entry{put("snippet", "s", `2`)}}) // older than the deletion

	fromB, _ := b.Records()
	a.Merge(fromB)
	if entries, _, _ := a.Load(); len(entries) != 0 {
		t.Fatalf("the later deletion did not win: %v", entries)
	}

	a.now = func() time.Time { return time.UnixMilli(1_700_000_020_000) }
	a.Commit(Change{Put: []Entry{put("snippet", "s", `3`)}}) // after the deletion: the record is back
	fromA, _ := a.Records()
	b.Merge(fromA)
	if entries, _, _ := b.Load(); len(entries) != 1 || string(entries[0].Payload) != `3` {
		t.Fatalf("an edit after the deletion did not bring the record back: %v", entries)
	}
}

func TestAStampFromTheFarFutureIsRefusedAndOneNearIsFollowed(t *testing.T) {
	a, b := pair(t)
	b.now = func() time.Time { return a.now().Add(MaxFuture + time.Hour) }
	b.Commit(Change{Put: []Entry{put("k", "far", `1`)}})
	records, _ := b.Records()
	if result, _ := a.Merge(records); result.Refused != 1 || len(result.Applied) != 0 {
		t.Fatalf("a stamp from the far future was taken: %+v", result)
	}

	b.now = func() time.Time { return a.now().Add(time.Hour) }
	b.Commit(Change{Put: []Entry{put("k", "near", `1`)}})
	records, _ = b.Records()
	a.Merge(records)
	a.Commit(Change{Put: []Entry{put("k", "near", `2`)}})
	merged, _ := a.Records()
	for _, record := range merged {
		if record.ID == "near" && string(record.Payload) != `2` {
			t.Fatalf("an edit made after seeing a stamp ahead of this clock lost to it: %v", record)
		}
	}
}

func TestMalformedReceivedRecordsAreCountedAndIgnored(t *testing.T) {
	a := open(t)
	good := makeStamp(a.now().UnixMilli(), 0, "abcdef01")
	result, err := a.Merge([]Record{
		{Kind: "", ID: "x", Stamp: good, Payload: []byte(`1`)},
		{Kind: "k", ID: "x", Stamp: "nonsense", Payload: []byte(`1`)},
		{Kind: "k", ID: "y", Stamp: good, Payload: []byte(`{not json`)},
		{Kind: "k", ID: "z", Stamp: good},
		{Kind: "k", ID: "ok", Stamp: good, Payload: []byte(`1`)},
	})
	if err != nil || result.Refused != 4 || len(result.Applied) != 1 {
		t.Fatalf("%+v, %v", result, err)
	}
}

func TestOldTombstonesArePrunedButRecentOnesAndLiveRecordsStay(t *testing.T) {
	store := open(t)
	day := 24 * time.Hour
	base := time.UnixMilli(1_700_000_000_000)
	store.now = func() time.Time { return base }
	store.Commit(Change{Put: []Entry{put("k", "old", `1`), put("k", "recent", `1`), put("k", "live", `1`)}})
	store.Commit(Change{Delete: []Key{{"k", "old"}}})
	store.now = func() time.Time { return base.Add(100 * day) }
	store.Commit(Change{Delete: []Key{{"k", "recent"}}})
	store.now = func() time.Time { return base.Add(101 * day) }
	if count, err := store.Prune(90 * day); err != nil || count != 1 {
		t.Fatalf("pruned %d, %v", count, err)
	}
	records, _ := store.Records()
	var ids []string
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	if !reflect.DeepEqual(ids, []string{"live", "recent"}) {
		t.Fatalf("left %v", ids)
	}
}

func TestNewerListsWhatTheOtherSideLacks(t *testing.T) {
	a, b := pair(t)
	a.Commit(Change{Put: []Entry{put("k", "1", `1`), put("k", "2", `1`)}})
	records, _ := a.Records()
	b.Merge(records)
	a.now = func() time.Time { return time.UnixMilli(1_700_000_001_000) }
	a.Commit(Change{Put: []Entry{put("k", "2", `2`), put("k", "3", `1`)}})
	have, _ := b.Records()
	missing, _ := a.Newer(have)
	if len(missing) != 2 || missing[0].ID != "2" || missing[1].ID != "3" {
		t.Fatalf("missing %v", missing)
	}
	if none, _ := b.Newer(have); len(none) != 0 {
		t.Fatalf("a store with nothing newer reported %v", none)
	}
}

// Whatever the devices do and however their records are passed around, once
// they have all heard from each other they hold the same records.
func TestDevicesThatExchangeEverythingEndUpWithTheSameRecords(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		random := rand.New(rand.NewSource(seed))
		stores := []*Store{open(t), open(t), open(t)}
		for _, store := range stores {
			offset := time.Duration(random.Intn(5000)-2500) * time.Millisecond // clocks that disagree
			store.now = func() time.Time { return time.UnixMilli(1_700_000_000_000).Add(offset) }
		}
		for step := 0; step < 60; step++ {
			store := stores[random.Intn(len(stores))]
			id := fmt.Sprint(random.Intn(6))
			switch random.Intn(4) {
			case 0:
				store.Commit(Change{Delete: []Key{{"host", id}}})
			case 1, 2:
				store.Commit(Change{Put: []Entry{put("host", id, fmt.Sprintf(`"%d-%d"`, seed, step))}})
			default:
				from, to := stores[random.Intn(len(stores))], stores[random.Intn(len(stores))]
				records, _ := from.Records()
				to.Merge(records)
			}
			for _, each := range stores {
				each.now = func(old func() time.Time) func() time.Time {
					return func() time.Time { return old().Add(time.Second) }
				}(each.now)
			}
		}
		for round := 0; round < 2; round++ {
			for _, from := range stores {
				records, _ := from.Records()
				for _, to := range stores {
					to.Merge(records)
				}
			}
		}
		want, _ := stores[0].Records()
		for _, store := range stores[1:] {
			got, _ := store.Records()
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("seed %d: devices disagree:\n%v\n%v", seed, want, got)
			}
		}
	}
}

func TestAStampForAnotherKindOfVersionOrdersWithTheRecords(t *testing.T) {
	a, b := pair(t)
	first, err := a.NextStamp()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := a.NextStamp()
	if second <= first {
		t.Fatalf("%s then %s", first, second)
	}
	// A stamp seen from elsewhere, ahead of this clock, is followed.
	b.now = func() time.Time { return a.now().Add(time.Hour) }
	ahead, _ := b.NextStamp()
	if !a.Acceptable(ahead) {
		t.Fatal("a stamp an hour ahead was refused")
	}
	a.Observe(ahead)
	after, _ := a.NextStamp()
	if after <= ahead {
		t.Fatalf("a stamp issued after seeing %s is %s", ahead, after)
	}
	far := makeStamp(a.now().Add(MaxFuture+time.Hour).UnixMilli(), 0, "abcdef01")
	if a.Acceptable(far) || a.Acceptable("junk") {
		t.Fatal("a stamp from the far future or a malformed one was accepted")
	}
	a.Observe(far)
	if later, _ := a.NextStamp(); later >= far {
		t.Fatal("observing a refused stamp moved the clock")
	}
}

func TestWindowsOfOneProcessShareAStoreAndItClosesWithTheLast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	first, err := OpenShared(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenShared(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("two holders got two stores, so two clocks")
	}
	if _, err := OpenShared(path, 2); err == nil {
		t.Fatal("a store opened with one format was handed out for another")
	}
	a, _ := first.NextStamp()
	b, _ := second.NextStamp()
	if a == b {
		t.Fatalf("two stamps are equal: %s", a)
	}
	first.Close()
	if _, err := second.NextStamp(); err != nil {
		t.Fatalf("the store closed while a holder was left: %v", err)
	}
	second.Close()
	if _, err := second.NextStamp(); err == nil {
		t.Fatal("the store stayed open after its last holder closed")
	}
	again, err := OpenShared(path, 1)
	if err != nil || again == second {
		t.Fatalf("a closed store was handed out again: %v", err)
	}
	again.Close()
}

func TestADataDirectoryWithQueryCharactersInItsNameStillWorks(t *testing.T) {
	name := "a#b?c%d"
	if runtime.GOOS == "windows" {
		name = "a#b%d" // a question mark cannot be in a Windows file name
	}
	path := filepath.Join(t.TempDir(), name, "config.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	store.Commit(Change{Put: []Entry{put("host", "a", `1`)}})
	store.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database is not where it was asked for: %v", err)
	}
	again, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if entries, _, _ := again.Load(); len(entries) != 1 {
		t.Fatalf("loaded %v", entries)
	}
}

func TestReadOnlyReadsWhatTheApplicationSavedAndChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	store.Commit(Change{
		Put:   []Entry{put("host", "a", `{"label":"one"}`), put("host", "b", `{"label":"two"}`)},
		Local: map[string][]byte{"state": []byte(`{"x":1}`)},
	})
	store.Commit(Change{Delete: []Key{{"host", "b"}}})
	store.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	entries, local, err := ReadOnly(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "a" || string(local["state"]) != `{"x":1}` {
		t.Fatalf("read %v and %v", entries, local)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("reading changed the file")
	}
	if matches, _ := filepath.Glob(path + "*"); len(matches) != 1 {
		t.Fatalf("reading left side files: %v", matches)
	}
}

func TestReadOnlyDoesNotCreateAFileAndRefusesWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "none.db")
	if _, _, err := ReadOnly(missing, 1); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a missing file gave %v", err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("reading created the file")
	}

	notStore := filepath.Join(dir, "text.db")
	os.WriteFile(notStore, []byte(strings.Repeat("this is not a database ", 10)), 0o600)
	if _, _, err := ReadOnly(notStore, 1); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a file that is not a store gave %v", err)
	}

	newer := filepath.Join(dir, "newer.db")
	store, err := Open(newer, 2)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, _, err := ReadOnly(newer, 1); !errors.Is(err, ErrTooNew) {
		t.Fatalf("payloads newer than this build gave %v", err)
	}
}

func TestReadOnlySeesWhatAnotherProcessCommittedWhileItWasOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Commit(Change{Put: []Entry{put("host", "a", `1`)}})
	if entries, _, err := ReadOnly(path, 1); err != nil || len(entries) != 1 {
		t.Fatalf("first read %v, %v", entries, err)
	}
	store.Commit(Change{Put: []Entry{put("host", "b", `2`)}})
	if entries, _, err := ReadOnly(path, 1); err != nil || len(entries) != 2 {
		t.Fatalf("second read %v, %v", entries, err)
	}
}

func TestExpectedKindsRejectsPhantomMembersAtomically(t *testing.T) {
	store := open(t)
	if _, err := store.Commit(Change{Put: []Entry{put("note-folder", "f", `{"name":"folder"}`)}}); err != nil {
		t.Fatal(err)
	}
	entries, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	folders := []Entry{}
	for _, e := range entries {
		if e.Kind == "note-folder" {
			folders = append(folders, e)
		}
	}
	// A new member appeared after an empty-directory check.
	if _, err = store.Commit(Change{Put: []Entry{put("note", "child", `{"folder":"f"}`)}}); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Records()
	_, err = store.Commit(Change{Delete: []Key{{Kind: "note-folder", ID: "f"}}, ExpectedKinds: map[string][]Entry{"note": {}, "note-folder": folders}, Local: map[string][]byte{"probe": []byte(`"must not write"`)}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("phantom insert was not rejected: %v", err)
	}
	after, _ := store.Records()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed transaction changed records")
	}
	_, local, _ := store.Load()
	if local["probe"] != nil {
		t.Fatal("failed transaction changed local values")
	}
}
