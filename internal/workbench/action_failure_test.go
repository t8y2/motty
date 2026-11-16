package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"motty/internal/i18n"
)

func TestFileFailuresUsePlainMessagesAndKeepDiagnostics(t *testing.T) {
	var diagnostics bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&diagnostics)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, test := range []struct {
			name string
			err  error
			save bool
		}{
			{"missing read", fs.ErrNotExist, false},
			{"denied read", fs.ErrPermission, false},
			{"unknown read", errors.New("json: unexpected end of input"), false},
			{"missing save folder", fs.ErrNotExist, true},
			{"denied save", fs.ErrPermission, true},
			{"unknown save", errors.New("CreateFileW HRESULT 0x80004005"), true},
		} {
			t.Run(language.String()+"/"+test.name, func(t *testing.T) {
				_, o := newWorld(t)
				s := newTestState(t, o)
				s.setLanguage(language)
				m := s.messages.ActionFeedback
				err := fmt.Errorf("operation failed: %w", &fs.PathError{Op: "native-operation", Path: "private-path-must-not-be-shown", Err: test.err})
				diagnostics.Reset()
				want := m.ImportHostCatalogFailed
				if test.save {
					want = m.SaveFailed
					s.showFileSaveFailure("Save", err)
				} else {
					s.showFileReadFailure("Import", err, want)
				}
				switch {
				case errors.Is(err, fs.ErrNotExist) && test.save:
					want = m.SaveFolderMissing
				case errors.Is(err, fs.ErrNotExist):
					want = m.FileMissing
				case errors.Is(err, fs.ErrPermission) && test.save:
					want = m.SaveDenied
				case errors.Is(err, fs.ErrPermission):
					want = m.ReadDenied
				}
				requireActionNotice(t, s, want, components.ToastError)
				if strings.Contains(s.status, "private-path") || strings.Contains(s.status, "HRESULT") || strings.Contains(s.status, "json:") {
					t.Fatalf("technical details leaked: %q", s.status)
				}
				if !strings.Contains(diagnostics.String(), err.Error()) {
					t.Fatal("raw error was lost from diagnostics")
				}
			})
		}
	}
}

func TestCatalogImportExplainsMalformedAndNewerFiles(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, newer := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/newer=%t", language.String(), newer), func(t *testing.T) {
				_, o := newWorld(t)
				path := filepath.Join(t.TempDir(), "hosts.json")
				data := "{broken-json"
				if newer {
					data = fmt.Sprintf(`{"format":%q,"version":%d}`, hostCatalogFormat, hostCatalogSchemaVersion+1)
				}
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
				o.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
				s := newTestState(t, o)
				s.setLanguage(language)
				before := len(s.hosts)
				s.importHostCatalog()
				want := s.messages.ActionFeedback.ImportHostCatalogFailed
				if newer {
					want = s.messages.ActionFeedback.ImportUpdateRequired
				}
				requireActionNotice(t, s, want, components.ToastError)
				if len(s.hosts) != before {
					t.Fatal("failed import changed saved hosts")
				}
			})
		}
	}
}
