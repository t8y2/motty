package configstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONChangePreservesUnknownFieldsAndConcurrentEdits(t *testing.T) {
	tests := []struct {
		name, current, before, after, want string
		conflict                           bool
	}{
		{"nested", `{"name":"old","proxy":{"port":1,"future":true},"future":42}`,
			`{"name":"old","proxy":{"port":1}}`, `{"name":"new","proxy":{"port":2}}`,
			`{"name":"new","proxy":{"port":2,"future":true},"future":42}`, false},
		{"disjoint", `{"a":1,"b":3}`, `{"a":1,"b":2}`, `{"a":4,"b":2}`, `{"a":4,"b":3}`, false},
		{"clear", `{"a":"old","future":42}`, `{"a":"old"}`, `{}`, `{"future":42}`, false},
		{"omitted zero", `{"a":0,"future":42}`, `{}`, `{"a":2}`, `{"a":2,"future":42}`, false},
		{"opaque empty projection", `{"object":{"future":42}}`, `{"object":{}}`, `{}`, `{"object":{"future":42}}`, false},
		{"null", `{"a":1,"future":42}`, `{"a":1}`, `{"a":null}`, `{"a":null,"future":42}`, false},
		{"conflict", `{"a":3}`, `{"a":1}`, `{"a":2}`, "", true},
		{"unchanged array", `{"list":[{"id":1,"future":2}],"a":1}`, `{"list":[{"id":1}],"a":1}`,
			`{"list":[{"id":1}],"a":2}`, `{"list":[{"id":1,"future":2}],"a":2}`, false},
		{"opaque array edit", `{"list":[{"id":1,"future":2}]}`, `{"list":[{"id":1}]}`, `{"list":[{"id":2}]}`, "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := (JSONChange{Before: []byte(test.before), After: []byte(test.after)}).Apply([]byte(test.current))
			if test.conflict {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("want conflict, got %s, %v", got, err)
				}
			} else if err != nil || !sameJSON(got, []byte(test.want)) {
				t.Fatalf("got %s, %v; want %s", got, err, test.want)
			}
		})
	}
}

func TestPatchTransactionPreservesDataAndRollsBackConflicts(t *testing.T) {
	store := open(t)
	if _, err := store.Commit(Change{
		Put:   []Entry{put("host", "h", `{"label":"old","future":true}`)},
		Local: map[string][]byte{"state": []byte(`{"theme":"dark","future":42}`)},
	}); err != nil {
		t.Fatal(err)
	}
	change := Change{
		Patch: []PatchEntry{{Key: Key{"host", "h"}, JSONChange: JSONChange{
			Before: []byte(`{"label":"old"}`), After: []byte(`{"label":"new"}`),
		}}},
		LocalPatch: map[string]JSONChange{"state": {
			Before: []byte(`{"theme":"light"}`), After: []byte(`{"theme":"system"}`),
		}},
	}
	if _, err := store.Commit(change); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict: %v", err)
	}
	entries, _, err := store.Load()
	if err != nil || !sameJSON(entries[0].Payload, []byte(`{"label":"old","future":true}`)) {
		t.Fatalf("partial write: %+v, %v", entries, err)
	}
	change.LocalPatch["state"] = JSONChange{Before: []byte(`{"theme":"dark"}`), After: []byte(`{}`)}
	if _, err := store.Commit(change); err != nil {
		t.Fatal(err)
	}
	entries, local, err := store.Load()
	if err != nil || !sameJSON(entries[0].Payload, []byte(`{"label":"new","future":true}`)) ||
		!sameJSON(local["state"], []byte(`{"future":42}`)) {
		t.Fatalf("lossy edit: %+v %s, %v", entries, local["state"], err)
	}
	if written, err := store.Commit(change); err != nil || written != 0 {
		t.Fatalf("idempotent retry wrote %d: %v", written, err)
	}
}

func TestOpenWriterRejectsLaterIncompatibleMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	old, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	newer, err := Open(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer newer.Close()
	before, err := newer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() error{
		func() error {
			_, err := old.Commit(Change{Local: map[string][]byte{"state": []byte("{}")}})
			return err
		},
		func() error { _, err := old.Merge(nil); return err },
		func() error { _, err := old.Prune(time.Hour); return err },
		func() error { _, err := old.NextStamp(); return err },
	} {
		if err := operation(); !errors.Is(err, ErrTooNew) {
			t.Fatalf("old writer was not blocked: %v", err)
		}
	}
	after, err := newer.Snapshot()
	if err != nil || string(after) != string(before) {
		t.Fatalf("blocked writes changed database: %v", err)
	}
}

func TestSnapshotReopensWithOpaqueLocalAndSharedData(t *testing.T) {
	store := open(t)
	if _, err := store.Commit(Change{Put: []Entry{put("future", "f", `{"v":1}`)},
		Local: map[string][]byte{"future-local": []byte(`{"v":2}`)}}); err != nil {
		t.Fatal(err)
	}
	image, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	entries, local, err := ReadOnly(path, 1)
	if err != nil || len(entries) != 1 || string(local["future-local"]) != `{"v":2}` {
		t.Fatalf("snapshot lost data: %+v %+v %v", entries, local, err)
	}
}

func BenchmarkJSONChangeNested(b *testing.B) {
	change := JSONChange{Before: []byte(`{"proxy":{"port":1}}`), After: []byte(`{"proxy":{"port":2}}`)}
	current := []byte(`{"proxy":{"port":1,"future":true},"future":42}`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := change.Apply(current); err != nil {
			b.Fatal(err)
		}
	}
}
