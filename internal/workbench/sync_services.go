package workbench

import (
	"context"
	"errors"
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/backup"
	"motty/internal/cloudsync"
	"motty/internal/configstore"
	"motty/internal/gist"
	"motty/internal/vault"
)

type syncServiceID string

const (
	syncServiceWebDAV syncServiceID = "webdav"
	syncServiceS3     syncServiceID = "s3"
	syncServiceGitHub syncServiceID = "github"
	syncServiceGitee  syncServiceID = "gitee"
)

var syncServices = [...]syncServiceID{syncServiceWebDAV, syncServiceS3, syncServiceGitHub, syncServiceGitee}
var hitSyncServiceToggle = tree.Sub(backupUiHits, tree.Name("sync-service-toggle"))

// All accounts belong to this device; Active is the only account allowed to
// exchange backups. LegacyPassword limits the old shared password fallback to
// the account that used it before migration.
type savedSyncServices struct {
	Active         syncServiceID                `json:"active,omitempty"`
	Manual         bool                         `json:"manual,omitempty"`
	Profiles       map[syncServiceID]*savedSync `json:"profiles,omitempty"`
	LegacyPassword syncServiceID                `json:"legacy_password,omitempty"`
}

func (id syncServiceID) label() string {
	switch id {
	case syncServiceWebDAV:
		return "WebDAV"
	case syncServiceS3:
		return "S3"
	case syncServiceGitHub:
		return "GitHub"
	case syncServiceGitee:
		return "Gitee"
	}
	return string(id)
}

func (view syncState) serviceID() syncServiceID {
	switch view.kind {
	case syncS3:
		return syncServiceS3
	case syncGist:
		if view.gist.provider == gist.Gitee {
			return syncServiceGitee
		}
		return syncServiceGitHub
	}
	return syncServiceWebDAV
}

func (id syncServiceID) passwordName() string { return "sync-password:" + string(id) }

func (state *State) activeSyncService() syncServiceID {
	if !state.sync.configured() {
		return ""
	}
	return state.sync.serviceID()
}

func cloneSyncProfiles(profiles map[syncServiceID]*savedSync) map[syncServiceID]*savedSync {
	next := make(map[syncServiceID]*savedSync, len(profiles))
	for id, profile := range profiles {
		if profile == nil {
			continue
		}
		copy := *profile
		if profile.S3 != nil {
			v := *profile.S3
			copy.S3 = &v
		}
		if profile.Gist != nil {
			v := *profile.Gist
			copy.Gist = &v
		}
		next[id] = &copy
	}
	return next
}

func (state *State) savedSyncServicesValue() *savedSyncServices {
	profiles := cloneSyncProfiles(state.syncProfiles)
	active := state.activeSyncService()
	if active != "" {
		profiles[active] = state.savedSyncValue()
	}
	return &savedSyncServices{Active: active, Manual: state.sync.manual, Profiles: profiles, LegacyPassword: state.syncLegacyPassword}
}

func (state *State) restoreSyncServices(saved *savedSyncServices, legacy *savedSync) {
	if saved == nil {
		state.restoreSync(legacy)
		state.syncProfiles = map[syncServiceID]*savedSync{}
		state.syncLegacyPassword = state.activeSyncService()
		if state.syncLegacyPassword != "" {
			state.syncProfiles[state.syncLegacyPassword] = state.savedSyncValue()
		}
		return
	}
	state.syncProfiles = cloneSyncProfiles(saved.Profiles)
	state.syncLegacyPassword = saved.LegacyPassword
	previous := state.sync
	state.restoreSync(saved.Profiles[saved.Active])
	state.sync.manual = saved.Manual
	if !sameSyncAccount(previous, state.sync) {
		cancelSyncWork(previous)
		state.syncEpoch++
		state.sync.working, state.sync.testing, state.sync.tested = false, false, false
		state.sync.timer, state.sync.cancel = nil, nil
		state.sync.err, state.sync.rowText, state.sync.runText = "", "", ""
		state.sync.again, state.sync.retries = false, 0
	}
	if state.sync.manual {
		state.stopSyncTimer()
	}
}

func sameSyncAccount(a, b syncState) bool {
	return a.revision == b.revision && a.kind == b.kind && a.url == b.url && a.user == b.user && a.path == b.path && a.s3 == b.s3 && a.gist == b.gist && a.allowInsecureHTTP == b.allowInsecureHTTP
}

func (state *State) syncProfile(id syncServiceID) syncState {
	if state.activeSyncService() == id {
		return state.sync
	}
	temporary := State{}
	temporary.restoreSync(state.syncProfiles[id])
	view := temporary.sync
	switch id {
	case syncServiceS3:
		view.kind = syncS3
		if state.syncProfiles[id] == nil {
			view.s3.pathStyle = true
		}
	case syncServiceGitHub:
		view.kind = syncGist
		view.gist.provider = gist.GitHub
	case syncServiceGitee:
		view.kind = syncGist
		view.gist.provider = gist.Gitee
	}
	return view
}

func cancelSyncWork(view syncState) {
	if view.timer != nil {
		view.timer.Stop()
	}
	if view.cancel != nil {
		view.cancel()
	}
}

// beginSyncRequest binds network cancellation to this account's lifetime.
func (state *State) beginSyncRequest() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	state.sync.cancel = cancel
	return ctx, cancel
}

// syncDraftSecrets resolves blank fields within the edited account, never from
// a different enabled account. It does not save or change the active service.
func (state *State) syncDraftSecrets(view syncState, secret, password string) (string, string, error) {
	opened := state.secrets.vault
	if opened == nil {
		return "", "", errors.New(state.messages.Tools.SyncErrNeedsVault)
	}
	get := func(name string) (string, error) { v, _, err := opened.Get(name); return v, err }
	var err error
	if secret == "" {
		secret, err = get(view.secretName())
		if err != nil {
			return "", "", err
		}
	}
	if password == "" {
		password, err = get(view.serviceID().passwordName())
		if err != nil {
			return "", "", err
		}
		if password == "" && state.syncLegacyPassword == view.serviceID() {
			password, err = get(backupPasswordSecret)
			if err != nil {
				return "", "", err
			}
		}
	}
	if secret == "" || password == "" {
		return "", "", errors.New(view.fieldsError(state.messages.Tools))
	}
	if len(password) < backup.MinPasswordLength {
		return "", "", backup.ErrWeakPassword
	}
	return secret, password, nil
}

// saveSyncProfile commits credentials and local configuration together. The
// vault rolls its changes back if the configuration write fails. Other windows
// are notified only after the vault lock is released.
func (state *State) saveSyncProfile(next syncState, secret, password string) string {
	secret, password, err := state.syncDraftSecrets(next, secret, password)
	if err != nil {
		return state.backupErrorText(err)
	}
	id := next.serviceID()
	previous, profiles, legacy := state.sync, state.syncProfiles, state.syncLegacyPassword
	previousSaved := state.lastSaved
	active := state.activeSyncService() == id
	state.syncProfiles = cloneSyncProfiles(profiles)
	if current := state.activeSyncService(); current != "" {
		state.syncProfiles[current] = state.savedSyncValue()
	}
	next.manual = previous.manual
	oldProfile := state.syncProfile(id)
	next.revision = oldProfile.revision
	if sameSyncAccount(oldProfile, next) {
		next.etag, next.last = oldProfile.etag, oldProfile.last
	}
	// A secret-only edit must reach other windows and cancel requests that still
	// carry the previous credentials, even when the address did not change.
	next.revision++
	temporary := State{sync: next}
	state.syncProfiles[id] = temporary.savedSyncValue()
	if active {
		state.sync = next
	}
	if state.syncLegacyPassword == id {
		state.syncLegacyPassword = ""
	}
	changes := []vault.Change{{Name: next.secretName(), Value: secret}, {Name: id.passwordName(), Value: password}}
	err = state.secrets.vault.ApplyWithCommit(changes, func() error { return state.saveWithResultModeRecovery(false, false, false) })
	if err != nil {
		state.sync, state.syncProfiles, state.syncLegacyPassword = previous, profiles, legacy
		if errors.Is(err, configstore.ErrConflict) {
			_ = state.refreshShared()
		}
		return fmt.Sprintf(state.messages.Tools.SyncFailedFmt, state.backupErrorText(err))
	}
	if active {
		cancelSyncWork(previous)
		state.syncEpoch++
	}
	state.configurationSaved(changedSavedFields(previousSaved, state.lastSaved), false, false)
	if active {
		state.scheduleSync()
	}
	return ""
}

func (state *State) toggleSyncService(id syncServiceID) {
	previous, profiles := state.sync, state.syncProfiles
	previousSaved := state.lastSaved
	active := state.activeSyncService()
	next := syncState{manual: previous.manual}
	if active != id {
		if state.syncProfiles[id] == nil {
			return
		}
		next = state.syncProfile(id)
		if _, _, err := state.syncDraftSecrets(next, "", ""); err != nil {
			state.showActionWarning(state.messages.Tools.SyncServicesTitle, state.backupErrorText(err))
			return
		}
		next.manual = previous.manual
	}
	state.syncProfiles = cloneSyncProfiles(profiles)
	if active != "" {
		state.syncProfiles[active] = state.savedSyncValue()
	}
	state.sync = next
	if err := state.saveWithResultModeRecovery(false, false, false); err != nil {
		state.sync, state.syncProfiles = previous, profiles
		state.showActionFailure(state.messages.Tools.SyncServicesTitle, err, state.backupErrorText(err))
		return
	}
	cancelSyncWork(previous)
	state.syncEpoch++
	state.configurationSaved(changedSavedFields(previousSaved, state.lastSaved), false, false)
	state.scheduleSync()
}

func (state *State) paintSyncServices(page *settingsPage) {
	m := state.messages.Tools
	page.hintRow(m.SyncServicesHint)
	editHits := []gfx.ID{hitSyncConfigure, hitSyncConfigureS3, hitSyncConfigureGist, hitSyncConfigureGitee}
	active := state.activeSyncService()
	for index, id := range syncServices {
		profile := state.syncProfile(id)
		configured := state.syncProfiles[id] != nil || active == id
		description := m.SyncServiceEmpty
		if configured {
			description = m.SyncServiceSaved + " \u00b7 " + profile.location()
			if active == id {
				_, run := state.syncTexts()
				description = m.SyncServiceActive + " \u00b7 " + profile.location() + "\n" + run
			}
		}
		toggle := m.SyncEnable
		if active == id {
			toggle = m.SyncDisable
		}
		page.actionRowWrap(id.label(), description, false,
			components.ButtonAction{ID: editHits[index], Label: m.SyncEdit, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSyncServiceToggle.At(index), Label: toggle, Variant: components.ButtonOutline, Disabled: !configured})
	}
	if active == "" {
		page.hintRow(m.SyncNoActive)
	}
}

// testSyncDraft probes the unsaved account. It never changes the enabled
// service, stores a token, or replaces a backup. Closing/editing the form makes
// the result stale; cleanup of a temporary object still runs after cancellation.
func (state *State) testSyncDraft(view syncState, secret, password string, form *formDialog) string {
	secret, password, err := state.syncDraftSecrets(view, secret, password)
	if err != nil {
		return state.backupErrorText(err)
	}
	store, err := view.newStore(secret)
	if err != nil {
		return state.backupErrorText(err)
	}
	serial := state.formSerial
	profileRevision := state.syncProfile(view.serviceID()).revision
	values := form.values()
	checked := form.checked
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	form.testCancel = cancel
	form.testSuccess = state.messages.Tools.SyncReadWriteOK
	go func() {
		defer cancel()
		var err error
		if preparer, ok := store.(interface{ Prepare(context.Context) error }); ok {
			err = preparer.Prepare(ctx)
		} else {
			err = store.Test(ctx)
		}
		if err == nil {
			data, _, readErr := store.Get(ctx)
			if readErr == nil {
				_, err = backup.Open(data, password)
			} else if !errors.Is(readErr, cloudsync.ErrNotFound) {
				err = readErr
			}
		}
		if err == nil {
			err = cloudsync.TestReadWrite(ctx, store)
		}
		state.aiPost(func(state *State) {
			if state.formSerial != serial || state.modal != modalForm {
				return
			}
			if state.form.values() != values || state.form.checked != checked || state.syncProfile(view.serviceID()).revision != profileRevision {
				state.clearFormTestStatus()
				state.form.testRunning = false
				state.form.testNote = ""
				state.form.dialogError = state.messages.Tools.SyncTestEdited
				return
			}
			state.finishFormTest(serial, err)
		})
	}()
	return ""
}

// values is a comparable snapshot, including credentials, kept only for this
// test's lifetime; it is never logged or persisted.
func (form *formDialog) values() [maxFormFields]string {
	var values [maxFormFields]string
	for index := 0; index < form.count; index++ {
		values[index] = form.value(index)
	}
	return values
}
