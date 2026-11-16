package workbench

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestImportingXshellArchivePreservesChineseNamesAndGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.xts")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("Xshell/NAS存储/管理主机.xsh"))
	if err != nil {
		t.Fatal(err)
	}
	member, err := writer.CreateHeader(&zip.FileHeader{Name: string(encoded), NonUTF8: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("[CONNECTION]\nHost=nas.example.test\nProtocol=SSH\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportXshell)
	if state.modal != modalHostImportResult || len(state.hosts) != 1 {
		t.Fatalf("modal = %d, hosts = %+v", state.modal, state.hosts)
	}
	if host := state.hosts[0]; host.Label != "管理主机" || host.Group != "Xshell/NAS存储" || host.Address != "nas.example.test" {
		t.Fatalf("imported host = %+v", host)
	}
	state.Click(hitHostImportClose)
	_, texts := paintedTexts(state)
	if !contains(texts, "管理主机") || !contains(texts, "NAS存储") {
		t.Fatalf("imported Chinese names are not visible: %q", texts)
	}
}
