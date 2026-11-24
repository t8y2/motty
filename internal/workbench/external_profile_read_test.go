package workbench

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"motty/internal/configstore"
	"motty/internal/protection"
)

// A reader in another process must consult the migration journal as well as
// the SQLite version. A cutover can legitimately leave an ordinary readable
// file while the journal still requires the user to finish recovery.
func TestExternalReadersRespectProfileProtection(t *testing.T) {
	world := newAgentWorld(t)
	saveAgentState(t, world, true)
	options := world.options.withDefaults()
	dir := filepath.Dir(options.StatePath)
	source := DevelopmentSource{Directory: dir, Keys: options.Keystore}

	// Exercise both APIs with a real ordinary file, while the journal cannot
	// be authenticated. Neither may begin writing to the destination.
	journal := filepath.Join(dir, "protection-migration.json")
	if err := os.WriteFile(journal, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	assertBlocked := func(want error) {
		t.Helper()
		var output bytes.Buffer
		if err := ServeAgentMCP(context.Background(), strings.NewReader(""), &output, options); !errors.Is(err, want) || output.Len() != 0 {
			t.Fatalf("standalone reader returned %v, wrote %d bytes", err, output.Len())
		}
		document, err := source.read(true)
		if !errors.Is(err, want) || len(document.Records) != 0 || len(document.Secrets) != 0 {
			t.Fatalf("release import returned data or the wrong error: %v", err)
		}
	}
	assertBlocked(protection.ErrCorrupt)
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	g, err := protection.Open(protection.Options{ConfigPath: options.StatePath, ConfigFormat: configFormat, Keystore: options.Keystore})
	if err != nil {
		t.Fatal(err)
	}
	const password = "test profile password"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	// An authenticated GUI in the same process must not confer its cached key
	// on the headless or release-import entry point.
	assertBlocked(configstore.ErrLocked)
	g.Lock()
	assertBlocked(configstore.ErrLocked)
	if err := g.Unlock(password); err != nil {
		t.Fatal(err)
	}
	if err := g.Disable(password); err != nil {
		t.Fatal(err)
	}
	document, err := source.read(false)
	if err != nil || len(document.Records) == 0 {
		t.Fatalf("ordinary import after disable: %v", err)
	}
	if err := ServeAgentMCP(context.Background(), strings.NewReader(""), io.Discard, options); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseReadDiscardsSnapshotWhenFirstMigrationTakesLease(t *testing.T) {
	dir := t.TempDir()
	source := DevelopmentSource{Directory: dir}
	config := filepath.Join(dir, "config.db")
	err := source.withOrdinaryProfile(func() error {
		lease, err := profileLease(config, true)
		if err != nil {
			return err
		}
		return lease.Close()
	})
	if !errors.Is(err, errProfileInUse) {
		t.Fatalf("read crossed the first migration: %v", err)
	}
	// Once created, the shared read lock excludes a second cutover.
	err = source.withOrdinaryProfile(func() error {
		lease, err := profileLease(config, true)
		if err == nil {
			lease.Close()
			t.Fatal("migration overlapped the snapshot")
		}
		if !errors.Is(err, errProfileInUse) {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
