package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"motty/internal/protection"
)

func TestRecoverProfileBackupUsesANewDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "recovered")
	file := protection.RecoveryFile{Name: "config.db.bak", Data: []byte("saved copy"), ProtectionJSON: []byte("wrapped key control"), ProfileKey: []byte("plaintext profile key")}
	if err := writeRecoveredFile(target, file); err != nil {
		t.Fatal(err)
	}
	for filename, want := range map[string]string{"config.db.bak": "saved copy", "protection.json": "wrapped key control"} {
		got, err := os.ReadFile(filepath.Join(target, filename))
		if err != nil || !bytes.Equal(got, []byte(want)) {
			t.Fatalf("incorrect %s: %v", filename, err)
		}
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected key material in recovery directory: %v", entries)
	}
	file.Data = []byte("replacement")
	if err := writeRecoveredFile(target, file); err == nil {
		t.Fatal("reused an existing directory")
	}
	got, err := os.ReadFile(filepath.Join(target, file.Name))
	if err != nil || string(got) != "saved copy" {
		t.Fatal("overwrote existing recovery data")
	}
}
