package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/tools/winpkg"
	"motty/internal/datadir"
)

func TestTheArchiveHoldsTheProgramBesideThePortableFolder(t *testing.T) {
	stamp := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	var first, second bytes.Buffer
	if err := write(&first, strings.NewReader("MZ program"), stamp, nil); err != nil {
		t.Fatal(err)
	}
	if err := write(&second, strings.NewReader("MZ program"), stamp, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("the same program and time gave different archives")
	}
	reader, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range reader.File {
		names = append(names, entry.Name)
	}
	if want := []string{"motty.exe", datadir.PortableName + "/README.txt"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries %v, want %v", names, want)
	}
}

func TestTheArchiveCarriesExtraFilesInDestinationOrder(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"conpty.dll": "dll bytes", "OpenConsole.exe": "console bytes"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	var archive bytes.Buffer
	extras := []winpkg.ExtraFile{
		{Source: filepath.Join(dir, "OpenConsole.exe"), Destination: "conpty/x64/x64/OpenConsole.exe"},
		{Source: filepath.Join(dir, "conpty.dll"), Destination: "conpty/x64/conpty.dll"},
	}
	if err := write(&archive, strings.NewReader("MZ program"), stamp, extras); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	bodies := map[string]string{}
	for _, entry := range reader.File {
		names = append(names, entry.Name)
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(file)
		file.Close()
		bodies[entry.Name] = string(data)
	}
	want := []string{"motty.exe", datadir.PortableName + "/README.txt", "conpty/x64/x64/OpenConsole.exe", "conpty/x64/conpty.dll"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries %v, want %v", names, want)
	}
	if bodies["conpty/x64/x64/OpenConsole.exe"] != "console bytes" || bodies["conpty/x64/conpty.dll"] != "dll bytes" {
		t.Errorf("extra bodies = %v", bodies)
	}
}
