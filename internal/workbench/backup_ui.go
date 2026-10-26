package workbench

import (
	"context"
	"errors"
	"fmt"
	"github.com/vivaui/viva/ui"
	"io/fs"
	"motty/internal/configstore"
	"os"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"motty/internal/backup"
	"motty/internal/cloudsync"
	"motty/internal/i18n"
	"motty/internal/vault"
	"motty/internal/webdav"
)

// Backup and sync. A backup is the hosts, settings, trusted host keys and saved
// passwords sealed in one file under a password the user chooses (package
// backup). It can be written to a file, or kept in the cloud (a WebDAV server,
// S3-compatible storage or a GitHub Gist, all behind cloudsync.Store) and
// fetched from another computer. Sessions, the open layout and the cloud login
// itself stay on the computer they belong to.

// backupUiHits is the place of the controls this file paints.
var backupUiHits = tree.Feature("backup-ui")

var (
	hitBackupExport      = tree.ID(backupUiHits, "backup-export")
	hitBackupRestore     = tree.ID(backupUiHits, "backup-restore")
	hitSyncConfigure     = tree.ID(backupUiHits, "sync-configure")
	hitSyncConfigureS3   = tree.ID(backupUiHits, "sync-configure-s3")
	hitSyncConfigureGist = tree.ID(backupUiHits, "sync-configure-gist")
	hitSyncNow           = tree.ID(backupUiHits, "sync-now")
	hitSyncAuto          = tree.ID(backupUiHits, "sync-auto")
)

const (
	webdavPasswordSecret = "webdav:password"
	backupPasswordSecret = "backup:password"
	defaultSyncPath      = "motty/backup.mottybak"
	syncTimeout          = 90 * time.Second
	beforeRestoreSuffix  = ".before-restore"
)

// savedSync is the cloud service in the state file: WebDAV by default, or the
// one Kind names. Its passwords and tokens are in the saved-passwords vault.
type savedSync struct {
	Kind     string     `json:"kind,omitempty"` // "" is WebDAV
	URL      string     `json:"url"`
	User     string     `json:"user,omitempty"`
	Path     string     `json:"path,omitempty"`
	S3       *savedS3   `json:"s3,omitempty"`
	Gist     *savedGist `json:"gist,omitempty"`
	ETag     string     `json:"etag,omitempty"`
	LastSync string     `json:"last_sync,omitempty"`
	// Manual is set when the user turned automatic syncing off.
	Manual bool `json:"manual,omitempty"`
}

// syncState is the cloud service and what is known of the file on it.
type syncState struct {
	kind syncKind
	// url, user and path are the WebDAV server.
	url, user, path string
	s3              s3Settings
	gist            gistSettings
	// etag identifies the version of the file this computer last uploaded or
	// downloaded, so an upload never replaces one it has not seen.
	etag string
	last time.Time
	// working is set while a request is on its way.
	working bool
	// manual is set when syncing only happens when the user asks for it.
	manual bool
	// err is why the last sync did not go through, in words, or "".
	err string
	// again is set when something changed while a sync was running.
	again bool
	// retries counts failed syncs in a row, which sets how long the next retry
	// waits.
	retries int
	// waitingForVault is set when a sync could not read its passwords because
	// the vault is locked; unlocking it syncs.
	waitingForVault bool
	// timer is the one pending start of a sync, after a change or a failure.
	timer *time.Timer

	rowText, runText string
	runFor           time.Time
	textLanguage     i18n.Language
}

func (state *State) restoreSync(saved *savedSync) {
	state.sync = syncState{}
	if saved == nil {
		return
	}
	next := syncState{etag: saved.ETag, manual: saved.Manual}
	switch saved.Kind {
	case savedKindS3:
		if saved.S3 == nil {
			return
		}
		next.kind, next.s3 = syncS3, saved.S3.settings()
	case savedKindGist:
		if saved.Gist == nil {
			return
		}
		next.kind, next.gist = syncGist, saved.Gist.settings()
	default:
		next.url, next.user, next.path = saved.URL, saved.User, saved.Path
	}
	if !next.configured() {
		return
	}
	state.sync = next
	if when, err := time.Parse(time.RFC3339, saved.LastSync); err == nil {
		state.sync.last = when
	}
}

func (state *State) savedSyncValue() *savedSync {
	view := &state.sync
	if !view.configured() {
		return nil
	}
	saved := &savedSync{ETag: view.etag, Manual: view.manual}
	switch view.kind {
	case syncS3:
		saved.Kind, saved.S3 = savedKindS3, view.s3.saved()
	case syncGist:
		saved.Kind, saved.Gist = savedKindGist, view.gist.saved()
	default:
		saved.URL, saved.User, saved.Path = view.url, view.user, view.path
	}
	if !view.last.IsZero() {
		saved.LastSync = view.last.UTC().Format(time.RFC3339)
	}
	return saved
}

// excludedFromBackup says whether a saved secret stays out of backups: the
// logins that reach the backup itself.
func excludedFromBackup(name string) bool {
	switch name {
	case webdavPasswordSecret, s3SecretName, gistTokenSecret, backupPasswordSecret:
		return true
	}
	return false
}

// backupDocument gathers what a backup holds: every shared record, deleted ones
// as tombstones, the trusted keys and the saved passwords. A locked vault is
// reported as vault.ErrLocked, since the passwords cannot be read.
func (state *State) backupDocument() (backup.Document, error) {
	if state.config == nil {
		return backup.Document{}, errors.New(state.messages.Tools.SyncErrNoStore)
	}
	records, err := state.config.Records()
	if err != nil {
		return backup.Document{}, err
	}
	document := backup.Document{Created: state.options.WallNow().UTC(), Device: state.config.Device(), Format: configFormat, Records: records}
	if data, err := os.ReadFile(state.options.KnownHostsPath); err == nil {
		document.KnownHosts = string(data)
	}
	if opened := state.secrets.vault; opened != nil {
		secrets, err := opened.Export()
		if err != nil {
			return backup.Document{}, err
		}
		for _, secret := range secrets {
			if !excludedFromBackup(secret.Name) {
				document.Secrets = append(document.Secrets, secret)
			}
		}
	}
	return document, nil
}

// backupErrorText says why a backup could not be made or opened.
func (state *State) backupErrorText(err error) string {
	m := state.messages.Tools
	switch {
	case errors.Is(err, backup.ErrWeakPassword):
		return m.BackupErrPasswordShort
	case errors.Is(err, backup.ErrWrongPassword):
		return m.BackupErrWrong
	case errors.Is(err, backup.ErrNotBackup):
		return m.BackupErrNotBackup
	case errors.Is(err, backup.ErrTooNew):
		return m.BackupErrTooNew
	case errors.Is(err, vault.ErrLocked):
		return m.BackupErrVault
	case errors.Is(err, cloudsync.ErrUnauthorized):
		// The service's own words (an S3 error code, GitHub's message) follow.
		if _, detail, found := strings.Cut(err.Error(), " ("); found {
			return m.SyncErrUnauthorized + " (" + detail
		}
		return m.SyncErrUnauthorized
	case errors.Is(err, cloudsync.ErrNotFound):
		return m.SyncErrNotFound
	case errors.Is(err, cloudsync.ErrInsecure):
		return m.SyncErrInsecure
	case errors.Is(err, cloudsync.ErrTooLarge):
		if state.sync.kind == syncGist {
			return m.GistErrTooLarge
		}
		return m.SyncErrTooLarge
	}
	return err.Error()
}

// applyBackup replaces the hosts, settings, host keys and saved passwords with a
// backup's, keeping what was there in a safety copy. It returns an error before
// changing anything when the restore cannot go through.
func (state *State) applyBackup(document backup.Document) (hosts int, err error) {
	m := state.messages.Tools
	if len(state.sessions) > 0 {
		return 0, errors.New(m.BackupErrSessions)
	}
	if state.config == nil {
		return 0, errors.New(m.SyncErrNoStore)
	}
	if document.Format > configFormat {
		return 0, backup.ErrTooNew
	}
	opened := state.secrets.vault
	if len(document.Secrets) > 0 && opened != nil && opened.HasMasterPassword() && !opened.Unlocked() {
		return 0, vault.ErrLocked
	}

	if data, readErr := os.ReadFile(state.options.StatePath); readErr == nil {
		if err := writeFileAtomic(state.options.StatePath+beforeRestoreSuffix, data); err != nil {
			return 0, err
		}
	}
	var entries []configstore.Entry
	for _, record := range document.Records {
		if !record.Deleted {
			entries = append(entries, configstore.Entry{Kind: record.Kind, ID: record.ID, Payload: record.Payload})
		}
	}
	incoming, _, err := joinState(entries, state.configBase.local)
	if err != nil {
		return 0, backup.ErrNotBackup
	}
	if incoming.AIProviders == nil && state.recordedProviders() > 0 {
		incoming.AIProviders = &[]savedAIProvider{} // a restore replaces what was recorded
	}
	state.restoreShared(incoming)
	state.hostDataChanged()
	state.rebuildTabOptions()
	state.save()

	if err := state.mergeKnownHosts(document.KnownHosts); err != nil {
		state.status = fmt.Sprintf(state.messages.Tools.BackupFailedFmt, err)
	}
	if opened != nil {
		for _, secret := range document.Secrets {
			if secret.Deleted || excludedFromBackup(secret.Name) {
				continue
			}
			if err := opened.Set(secret.Name, secret.Value); err != nil {
				state.status = fmt.Sprintf(state.messages.Tools.BackupSecretsFailedFmt, state.vaultErrorText(err))
				return len(state.hosts), nil
			}
		}
	}
	return len(state.hosts), nil
}

// mergeKnownHosts adds the backup's trusted keys that this computer lacks;
// what it already trusts stays.
func (state *State) mergeKnownHosts(incoming string) error {
	if strings.TrimSpace(incoming) == "" {
		return nil
	}
	current, err := os.ReadFile(state.options.KnownHostsPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(current), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	merged := strings.TrimRight(string(current), "\n")
	added := false
	for _, line := range strings.Split(incoming, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || have[line] {
			continue
		}
		if merged != "" {
			merged += "\n"
		}
		merged += line
		have[line], added = true, true
	}
	if added {
		return writeFileAtomic(state.options.KnownHostsPath, []byte(merged+"\n"))
	}
	return nil
}

// --- file backup ---------------------------------------------------------

func (state *State) openExportBackup() {
	m := state.messages.Tools
	state.openForm(formDialog{
		title: m.BackupExportTitle, description: m.BackupExportDialogDescription, submit: m.BackupExportSave,
		run: (*State).runExportBackup,
	},
		formField{label: m.BackupPassword, placeholder: m.BackupPasswordPlaceholder, secure: true},
		formField{label: m.BackupPasswordConfirm, secure: true},
	)
}

func (state *State) runExportBackup(form *formDialog) string {
	m := state.messages.Tools
	password := form.value(0)
	switch {
	case len(password) < backup.MinPasswordLength:
		return m.BackupErrPasswordShort
	case password != form.value(1):
		return m.BackupErrPasswordMismatch
	}
	document, err := state.backupDocument()
	if err != nil {
		return state.backupErrorText(err)
	}
	sealed, err := backup.Seal(document, password)
	if err != nil {
		return state.backupErrorText(err)
	}
	path, err := state.options.SaveFile(app.SaveFileOptions{
		Title:            m.BackupSaveTitle,
		SuggestedName:    "motty-" + state.options.WallNow().Format("20060102") + ".mottybak",
		DefaultExtension: "mottybak",
		Filters:          []app.FileFilter{{Name: "Motty backup", Pattern: "*.mottybak"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return ""
	}
	if err == nil {
		err = writePrivateAtomic(path, sealed)
	}
	if err != nil {
		return fmt.Sprintf(m.BackupFailedFmt, err)
	}
	state.status = fmt.Sprintf(m.BackupSavedFmt, path)
	return ""
}

func (state *State) openRestoreBackup() {
	m := state.messages.Tools
	if len(state.sessions) > 0 {
		state.status = m.BackupErrSessions
		return
	}
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   m.BackupPickerTitle,
		Filters: []app.FileFilter{{Name: "Motty backup", Pattern: "*.mottybak"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.status = fmt.Sprintf(m.BackupFailedFmt, err)
		return
	}
	info, err := os.Stat(paths[0])
	if err == nil && info.Size() > backup.MaxSize {
		err = backup.ErrTooLarge
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(paths[0])
	}
	if err != nil {
		state.status = fmt.Sprintf(m.BackupFailedFmt, err)
		return
	}
	state.openForm(formDialog{
		title: m.BackupRestoreTitle, description: m.BackupRestoreDescription, submit: m.BackupRestoreConfirm, danger: true,
		run: func(state *State, form *formDialog) string {
			document, err := backup.Open(data, form.value(0))
			if err != nil {
				return state.backupErrorText(err)
			}
			hosts, err := state.applyBackup(document)
			if err != nil {
				return state.backupErrorText(err)
			}
			state.status = fmt.Sprintf(state.messages.Tools.BackupRestoredFmt, hosts)
			return ""
		},
	}, formField{label: m.BackupPassword, secure: true})
}

// --- WebDAV --------------------------------------------------------------

func (state *State) openSyncConfigure() {
	m := state.messages.Tools
	form := state.openForm(formDialog{
		title: m.SyncDialogTitle, description: m.SyncDialogDescription, submit: m.SyncSave,
		run: (*State).runSyncConfigure,
	},
		formField{label: m.SyncURL, placeholder: "https://dav.example.com/dav"},
		formField{label: m.SyncUser},
		formField{label: m.SyncPassword, secure: true},
		formField{label: m.SyncPath, placeholder: defaultSyncPath},
		formField{label: m.BackupPassword, placeholder: m.BackupPasswordPlaceholder, secure: true},
	)
	form.set(0, state.sync.url)
	form.set(1, state.sync.user)
	form.set(3, state.sync.path)
}

func (state *State) runSyncConfigure(form *formDialog) string {
	m := state.messages.Tools
	address := strings.TrimSpace(form.value(0))
	if address == "" {
		state.clearSync()
		return ""
	}
	user, path := strings.TrimSpace(form.value(1)), strings.TrimSpace(form.value(3))
	if path == "" {
		path = defaultSyncPath
	}
	webdavPassword, backupPassword := form.value(2), form.value(4)
	configured := state.sync.kind == syncWebDAV && state.sync.url != ""
	if user == "" || (!configured && (webdavPassword == "" || backupPassword == "")) {
		return m.SyncErrFields
	}
	if backupPassword != "" && len(backupPassword) < backup.MinPasswordLength {
		return m.BackupErrPasswordShort
	}
	if _, err := webdav.New(webdav.Config{URL: address, User: user, Password: "x", Path: path}, nil); err != nil {
		if errors.Is(err, cloudsync.ErrInsecure) {
			return m.SyncErrInsecure
		}
		return m.SyncErrAddress
	}
	if message := state.keepSyncSecrets(webdavPasswordSecret, webdavPassword, backupPassword); message != "" {
		return message
	}
	if !configured || address != state.sync.url || path != state.sync.path {
		state.switchSync(syncState{kind: syncWebDAV, url: address, user: user, path: path})
	} else {
		state.sync.user = user
	}
	state.sync.rowText = ""
	state.save()
	state.testSync()
	return ""
}

func (state *State) clearSync() {
	state.sync = syncState{}
	if opened := state.secrets.vault; opened != nil {
		for _, name := range [...]string{webdavPasswordSecret, s3SecretName, gistTokenSecret, backupPasswordSecret} {
			_ = opened.Delete(name)
		}
	}
	state.save()
}

// syncLogin builds the store a request to the cloud needs. It reports false,
// with the reason on the status line, when the passwords cannot be read; a
// locked vault asks for its password and runs retry once it is given. A quiet
// login, for a sync nobody asked for, never opens a dialog: it keeps the reason
// for the settings page and waits for the vault to be unlocked.
func (state *State) syncLogin(retry func(), quiet bool) (store cloudsync.Store, backupPassword string, ok bool) {
	m := state.messages.Tools
	opened := state.secrets.vault
	fail := func(text string) (cloudsync.Store, string, bool) {
		if quiet {
			state.sync.err, state.sync.runText = text, ""
		} else {
			state.status = text
		}
		return nil, "", false
	}
	if !state.sync.configured() || opened == nil {
		return fail(m.SyncErrNeedsVault)
	}
	if opened.HasMasterPassword() && !opened.Unlocked() {
		if quiet {
			state.sync.waitingForVault = true
			return fail(m.BackupErrVault)
		}
		state.openVaultUnlock(nil, retry)
		return nil, "", false
	}
	secret, foundSecret, err := opened.Get(state.sync.secretName())
	if err == nil {
		var foundBackup bool
		backupPassword, foundBackup, err = opened.Get(backupPasswordSecret)
		foundSecret = foundSecret && foundBackup
	}
	if err != nil || !foundSecret {
		if err != nil {
			return fail(fmt.Sprintf(m.SyncFailedFmt, state.vaultErrorText(err)))
		}
		return fail(state.sync.fieldsError(m))
	}
	store, err = state.sync.newStore(secret)
	if err != nil {
		return fail(fmt.Sprintf(m.SyncFailedFmt, state.backupErrorText(err)))
	}
	state.sync.waitingForVault = false
	return store, backupPassword, true
}

// syncFailed ends a sync that did not go through. The reason stays on the
// settings page; a failure that waiting could cure (the network) is tried again
// later, one that needs the user (a wrong password) is not.
func (state *State) syncFailed(err error) {
	state.sync.working = false
	state.sync.err = state.backupErrorText(err)
	state.status = fmt.Sprintf(state.messages.Tools.SyncFailedFmt, state.sync.err)
	state.sync.runText = ""
	if state.sync.manual || !syncRetryable(err) {
		return
	}
	state.sync.retries++
	state.armSyncTimer(min(state.syncRetryBase<<(state.sync.retries-1), syncRetryMax))
}

// testSync checks the server in the background and says so on the status line.
func (state *State) testSync() {
	store, _, ok := state.syncLogin(state.testSync, false)
	if !ok {
		return
	}
	state.status = state.messages.Tools.SyncWorking
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()
		err := store.Test(ctx)
		state.aiPost(func(state *State) {
			if err != nil {
				state.syncFailed(err)
				return
			}
			state.status = state.messages.Tools.SyncTestOK
			state.scheduleSync() // a service just set up has not been synced yet
		})
	}()
}

// --- settings page -------------------------------------------------------

func (state *State) clickBackupSettings(id ui.ID) bool {
	switch id {
	case hitBackupExport:
		state.openExportBackup()
	case hitBackupRestore:
		state.openRestoreBackup()
	case hitSyncConfigure:
		state.openSyncConfigure()
	case hitSyncConfigureS3:
		state.openS3Configure()
	case hitSyncConfigureGist:
		state.openGistConfigure()
	case hitSyncNow:
		if state.sync.configured() {
			state.syncNow()
		}
	case hitSyncAuto:
		if state.sync.configured() {
			state.sync.manual = !state.sync.manual
			state.sync.err, state.sync.retries, state.sync.runText = "", 0, ""
			if state.sync.manual {
				state.stopSyncTimer()
			}
			state.save()
			if !state.sync.manual {
				state.scheduleSync()
			}
		}
	default:
		return false
	}
	return true
}

// syncTexts keeps the two descriptions of the cloud rows up to date; they are
// made again only when what they say changes (an empty text is made again).
func (state *State) syncTexts() (row, run string) {
	m, view := state.messages.Tools, &state.sync
	if view.textLanguage != state.language {
		view.rowText, view.runText, view.textLanguage = "", "", state.language
	}
	if view.rowText == "" {
		view.rowText = m.SyncRowNone
		if view.configured() {
			view.rowText = fmt.Sprintf(m.SyncRowFmt, view.location())
		}
	}
	if view.runText == "" || !view.runFor.Equal(view.last) {
		view.runFor = view.last
		if view.err != "" {
			view.runText = fmt.Sprintf(m.SyncRunFailedFmt, view.err)
		} else if view.last.IsZero() {
			view.runText = m.SyncRunNever
		} else {
			view.runText = fmt.Sprintf(m.SyncRunLastFmt, view.last.Local().Format("2006-01-02 15:04"))
		}
	}
	return view.rowText, view.runText
}

func (state *State) paintBackupSettings(page *settingsPage) {
	m := state.messages.Tools
	row, run := state.syncTexts()
	page.groups(func() {
		page.group(m.BackupGroupFile, func() {
			page.actionRow(m.BackupExportRow, m.BackupExportDescription, false,
				components.ButtonAction{ID: hitBackupExport, Label: m.BackupExport, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitBackupRestore, Label: m.BackupRestore, Variant: components.ButtonOutline},
			)
		})
		page.group(m.BackupGroupSync, func() {
			page.actionRow(m.SyncRow, row, false,
				components.ButtonAction{ID: hitSyncConfigure, Label: m.SyncConfigure, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitSyncConfigureS3, Label: m.SyncConfigureS3, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitSyncConfigureGist, Label: m.SyncConfigureGist, Variant: components.ButtonOutline})
			page.toggleRow(m.SyncAuto, m.SyncAutoDescription, !state.sync.manual, hitSyncAuto)
			page.actionRow(m.SyncRunRow, run, !state.sync.configured() || state.sync.working,
				components.ButtonAction{ID: hitSyncNow, Label: m.SyncNow, Variant: components.ButtonOutline},
			)
		})
	})
}
