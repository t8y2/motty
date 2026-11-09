package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/backup"
	"motty/internal/buildinfo"
	"motty/internal/configstore"
	"motty/internal/vault"
)

// DevelopmentSource names the release profile offered to a new development
// profile. Keys is injectable; nil reads the release system store with its
// existing file-key fallback, never the development namespace.
type DevelopmentSource struct {
	Directory string
	Keys      vault.Keystore
}

const developmentImportKey = "development_import"

var (
	developmentImportHits = tree.Feature("development-import")
	hitDevelopmentImport  = tree.ID(developmentImportHits, "import")
	hitDevelopmentEmpty   = tree.ID(developmentImportHits, "empty")
	hitDevelopmentClose   = tree.ID(developmentImportHits, "close")
)

type developmentImport struct {
	offered         bool
	passwords       bool
	recordsImported bool
	hosts           int
	err             string
	// body declares the dialog's body: its checkbox writes passwords itself.
	body *ui.Page
}

func developmentImportPhase(local map[string][]byte) (string, error) {
	raw := local[developmentImportKey]
	if len(raw) == 0 {
		return "", nil
	}
	var phase string
	if err := json.Unmarshal(raw, &phase); err != nil {
		return "", fmt.Errorf("development import: %w", err)
	}
	switch phase {
	case "records", "done", "empty":
		return phase, nil
	default:
		return "", fmt.Errorf("development import: unknown progress %q", phase)
	}
}

func (state *State) offerDevelopmentImport() {
	source := state.options.DevelopmentSource
	if !buildinfo.Development || source == nil || state.config == nil || state.saveBlocked ||
		state.modal != modalNone || state.developmentImport.offered {
		return
	}
	entries, local, err := state.config.Load()
	if err != nil {
		state.showDevelopmentImport(0, false, err)
		return
	}
	phase, err := developmentImportPhase(local)
	if err != nil {
		state.showDevelopmentImport(0, false, err)
		return
	}
	if phase == "done" || phase == "empty" {
		return
	}
	if phase == "records" {
		state.showDevelopmentImport(len(state.hosts), true, nil)
		return
	}
	if len(entries) != 0 || len(state.sessions) != 0 || state.sync.configured() ||
		state.secrets.vault != nil && state.secrets.vault.Len() > 0 {
		return
	}
	path := filepath.Join(source.Directory, "config.db")
	entries, _, err = configstore.ReadOnly(path, configFormat)
	if errors.Is(err, os.ErrNotExist) || err == nil && len(entries) == 0 {
		return
	}
	hosts := 0
	for _, entry := range entries {
		if entry.Kind == kindHost {
			hosts++
		}
	}
	state.showDevelopmentImport(hosts, false, err)
}

func (state *State) showDevelopmentImport(hosts int, records bool, err error) {
	state.developmentImport = developmentImport{offered: true, passwords: true, hosts: hosts, recordsImported: records}
	if err != nil {
		state.developmentImport.err = state.vaultErrorText(err)
	}
	state.modal = modalDevelopmentImport
}

// The source is opened independently, without a stamper. Export consequently
// reads old secrets without stamping or rewriting them.
func (source DevelopmentSource) read(passwords bool) (backup.Document, error) {
	entries, _, err := configstore.ReadOnly(filepath.Join(source.Directory, "config.db"), configFormat)
	if err != nil {
		return backup.Document{}, err
	}
	document := backup.Document{Format: configFormat}
	for _, entry := range entries {
		document.Records = append(document.Records, configstore.Record{Kind: entry.Kind, ID: entry.ID, Payload: entry.Payload})
	}
	if _, refused := holdableRecords(document.Records); refused != 0 {
		return backup.Document{}, backup.ErrNotBackup
	}
	known, err := os.ReadFile(filepath.Join(source.Directory, "known_hosts"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return backup.Document{}, err
	}
	document.KnownHosts = string(known)
	if !passwords {
		return document, nil
	}
	path := filepath.Join(source.Directory, "vault.json")
	keys := source.Keys
	if keys == nil {
		keys = vault.FallbackKeystore{
			System: systemKeystore{release: true},
			File:   vault.FileKeystore{Path: filepath.Join(source.Directory, "vault.key")},
		}
	}
	opened, err := vault.Open(path, importReadOnlyKeys{keys})
	if err != nil {
		return backup.Document{}, err
	}
	if opened.Len() == 0 {
		return document, nil
	}
	secrets, err := opened.Export()
	if err != nil {
		return backup.Document{}, err
	}
	for _, secret := range secrets {
		if !secret.Deleted && !excludedFromBackup(secret.Name) {
			document.Secrets = append(document.Secrets, secret)
		}
	}
	return document, nil
}

type importReadOnlyKeys struct{ source vault.Keystore }

func (keys importReadOnlyKeys) Load(service, account string) ([]byte, error) {
	return keys.source.Load(service, account)
}
func (importReadOnlyKeys) Store(string, string, []byte) error {
	return errors.New("development import: source keys are read-only")
}
func (importReadOnlyKeys) Delete(string, string) error {
	return errors.New("development import: source keys are read-only")
}

func (state *State) importDevelopmentData() error {
	source := state.options.DevelopmentSource
	if source == nil || !buildinfo.Development || state.config == nil {
		return errors.New("development import: no source or destination")
	}
	current, local, err := state.config.Load()
	if err != nil {
		return err
	}
	phase, err := developmentImportPhase(local)
	if err != nil {
		return err
	}
	if phase == "done" || phase == "empty" {
		return configstore.ErrConflict
	}
	state.developmentImport.recordsImported = phase == "records"
	if phase == "" && (len(current) != 0 || len(state.sessions) != 0 || state.sync.configured() ||
		state.secrets.vault != nil && state.secrets.vault.Len() != 0) {
		return errors.New(state.messages.DevelopmentImportNotEmpty)
	}
	document, err := source.read(state.developmentImport.passwords)
	if err != nil {
		return err
	}
	if phase == "" {
		change := configstore.Change{
			Writer: configWriter(), Expected: map[configstore.Key][]byte{},
			ExpectedLocal: map[string][]byte{developmentImportKey: nil},
			Local:         map[string][]byte{developmentImportKey: []byte(`"records"`)},
		}
		for _, record := range document.Records {
			change.Put = append(change.Put, configstore.Entry{Kind: record.Kind, ID: record.ID, Payload: record.Payload})
			change.Expected[configstore.Key{Kind: record.Kind, ID: record.ID}] = nil
		}
		if _, err := state.config.Commit(change); err != nil {
			return err
		}
		state.developmentImport.recordsImported = true
		if err := state.reloadShared(); err != nil {
			return err
		}
		state.reloadOtherWindows()
	}
	if err := state.mergeKnownHosts(document.KnownHosts); err != nil {
		return err
	}
	if len(document.Secrets) != 0 {
		opened := state.secrets.vault
		if opened == nil {
			return vault.ErrUnavailable
		}
		existing, err := opened.Export()
		if err != nil {
			return err
		}
		have := make(map[string]bool, len(existing))
		for _, secret := range existing {
			have[secret.Name] = true
		}
		var changes []vault.Change
		for _, secret := range document.Secrets {
			if !have[secret.Name] {
				changes = append(changes, vault.Change{Name: secret.Name, Value: secret.Value})
			}
		}
		if err := opened.Apply(changes); err != nil {
			return err
		}
	}
	_, err = state.config.Commit(configstore.Change{
		Writer: configWriter(),
		Local:  map[string][]byte{developmentImportKey: []byte(`"done"`)},
	})
	return err
}

func (state *State) clickDevelopmentImport(id gfx.ID) {
	switch id {
	case hitDevelopmentClose:
		state.closeModal()
	case hitDevelopmentEmpty:
		if _, err := state.config.Commit(configstore.Change{
			Local: map[string][]byte{developmentImportKey: []byte(`"empty"`)},
		}); err != nil {
			state.developmentImport.err = err.Error()
			return
		}
		state.closeModal()
	case hitDevelopmentImport:
		if err := state.importDevelopmentData(); err != nil {
			state.developmentImport.err = state.vaultErrorText(err)
			return
		}
		state.closeModal()
		state.showNextPrompt()
	}
}

func (state *State) paintDevelopmentImport(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, view := state.messages, &state.developmentImport
	body := fmt.Sprintf(m.DevelopmentImportBodyFmt, view.hosts)
	skip := m.DevelopmentImportEmpty
	if view.recordsImported {
		body, skip = m.DevelopmentImportResume, m.DevelopmentImportKeep
	}
	const width float32 = 600
	bodyWidth := width - 2*(metrics.PadSurface+metrics.GapItem)
	lines := wrappedLines(canvas, body, metrics.FontSizeM, bodyWidth)
	errorText := ""
	errorLines := 0
	if view.err != "" {
		errorText = fmt.Sprintf(m.DevelopmentImportFailedFmt, view.err)
		errorLines = wrappedLines(canvas, errorText, metrics.FontSizeM, bodyWidth)
	}
	height := float32(lines+errorLines)*stateNoticeLine + 40
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitDevelopmentClose, CloseID: hitDevelopmentClose,
		InitialFocusID: hitDevelopmentImport, ContentSize: gfx.Size{Width: width}, FitBody: true, BodyHeight: height,
	})
	layout := dialog.Begin(m.DevelopmentImportTitle, "")
	if view.body == nil {
		view.body = ui.NewPage(ui.PageConfig{Tree: &tree, Root: "development-import", Theme: &state.theme})
	}
	view.body.Draw(canvas, layout.Body, func(c ui.C) {
		ui.Column(c).Fill().Children(func() {
			ui.Paragraph(c, body, lines).FontSize(metrics.FontSizeM).LineHeight(stateNoticeLine)
			ui.Checkbox(c, &view.passwords, m.DevelopmentImportPasswords).Color(state.pal.text)
			if errorLines != 0 {
				ui.Paragraph(c, errorText, errorLines).FontSize(metrics.FontSizeM).LineHeight(stateNoticeLine)
			}
		})
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitDevelopmentEmpty, Label: skip, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitDevelopmentImport, Label: m.DevelopmentImportAction},
	)
	dialog.End()
}
