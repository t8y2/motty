package workbench

import (
	"testing"

	"motty/internal/configstore"
)

// saveReleaseState writes the layout a release build uses, even when the test
// binary itself has the dev tag. It lets cross-build persistence tests start
// from a real release-shaped store without calling build-specific New.
func saveReleaseState(t testing.TB, path string, saved savedState) {
	t.Helper()
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, local, _, err := splitState(saved, newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{
		Put:   entries,
		Local: map[string][]byte{localKey: local},
	}); err != nil {
		t.Fatal(err)
	}
}

func loadReleaseState(t testing.TB, path string) savedState {
	t.Helper()
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, local, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	saved, _, err := joinState(entries, local[localKey])
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func localConfigValue(t testing.TB, path, key string) []byte {
	t.Helper()
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, local, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	return local[key]
}

func setLocalConfigValue(t testing.TB, path, key string, value []byte) {
	t.Helper()
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Commit(configstore.Change{Local: map[string][]byte{key: value}}); err != nil {
		t.Fatal(err)
	}
}
