package workbench

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"motty/internal/backup"
	"motty/internal/cloudsync"
	"motty/internal/configstore"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// Syncing. The server keeps one sealed file holding every shared record, every
// saved password and the trusted keys, each with the stamp of its version. A sync
// downloads it, takes in whatever is newer than what this computer has, and
// uploads the result if the server lacks something this computer knows. It never
// replaces the server's file with less than it held, so computers that sync in
// any order, and as often as they like, end up with the same data.

const (
	// syncAttempts is how often one sync starts over because another computer
	// replaced the file between its download and its upload.
	syncAttempts = 4
	// tombstoneKeep is how long a deletion is remembered, so that a computer
	// that was away for less than this still hears of it.
	tombstoneKeep = 90 * 24 * time.Hour
)

// syncNow merges this computer's data with the server's. The network work is on
// a goroutine; what touches the window's data is on the window's thread.
func (state *State) syncNow() { state.runSync(false) }

// runSync starts a sync unless one is running. A quiet one, started by a change
// or at start, never opens a dialog and keeps the status line as it is.
func (state *State) runSync(quiet bool) {
	if state.sync.working {
		return
	}
	store, password, ok := state.syncLogin(func() { state.runSync(false) }, quiet)
	if !ok {
		return
	}
	state.sync.working, state.sync.err, state.sync.runText = true, "", ""
	if !quiet {
		state.status = state.messages.Tools.SyncWorking
	}
	state.syncRound(store, password, 1)
}

// syncRound downloads the server's file.
func (state *State) syncRound(store cloudsync.Store, password string, attempt int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()
		var remote backup.Document
		data, tag, err := store.Get(ctx)
		missing := errors.Is(err, cloudsync.ErrNotFound)
		switch {
		case missing:
			err, tag = nil, ""
		case err == nil:
			remote, err = backup.Open(data, password)
		}
		state.aiPost(func(state *State) { state.syncMerge(store, password, attempt, remote, tag, missing, err) })
	}()
}

// syncMerge takes the server's data in and, if the server lacks anything, sends
// the union back.
func (state *State) syncMerge(store cloudsync.Store, password string, attempt int, remote backup.Document, tag string, missing bool, err error) {
	if err != nil {
		state.syncFailed(err)
		return
	}
	upload, err := state.mergeRemote(remote, missing)
	if err != nil {
		state.syncFailed(err)
		return
	}
	if upload == nil {
		state.syncDone(tag, "")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()
		sealed, err := backup.Seal(*upload, password)
		newTag := ""
		if err == nil {
			newTag, err = store.Put(ctx, sealed, tag)
		}
		if err == nil && newTag == "" {
			// The server did not say which version it stored; reading it back does.
			if _, read, readErr := store.Get(ctx); readErr == nil {
				newTag = read
			}
		}
		created := ""
		if holder, ok := store.(gistHolder); ok {
			created = holder.ID() // a first upload makes the gist; remember where
		}
		state.aiPost(func(state *State) {
			switch {
			case errors.Is(err, cloudsync.ErrChanged) && attempt < syncAttempts:
				state.syncRound(store, password, attempt+1)
			case err != nil:
				state.syncFailed(err)
			default:
				state.syncDone(newTag, created)
			}
		})
	}()
}

func (state *State) syncDone(tag, created string) {
	state.sync.working, state.sync.err, state.sync.retries = false, "", 0
	state.sync.etag, state.sync.last = tag, state.options.WallNow()
	state.sync.runText = ""
	if state.sync.kind == syncGist && created != "" && created != state.sync.gist.id {
		state.sync.gist.id, state.sync.rowText = created, ""
	}
	state.save()
	state.status = state.messages.Tools.SyncDone
	if state.sync.again {
		state.sync.again = false
		state.scheduleSync()
	}
}

// mergeRemote takes the server's data into this computer. It returns the
// document to upload, or nil when the server already holds everything here.
func (state *State) mergeRemote(remote backup.Document, missing bool) (*backup.Document, error) {
	if state.config == nil {
		return nil, errors.New(state.messages.Tools.SyncErrNoStore)
	}
	if remote.Format > configFormat {
		return nil, backup.ErrTooNew
	}
	opened := state.secrets.vault

	// Passwords saved before they were versioned get a stamp now, so that they are
	// compared with the server's by when they were last changed here and not
	// lost to any stamped copy.
	if opened != nil {
		if _, err := opened.Export(); err != nil {
			return nil, err
		}
	}
	var received []vault.Secret
	var stamps []configstore.Stamp
	for _, secret := range remote.Secrets {
		if excludedFromBackup(secret.Name) || !state.config.Acceptable(configstore.Stamp(secret.Stamp)) {
			continue
		}
		received = append(received, secret)
		stamps = append(stamps, configstore.Stamp(secret.Stamp))
	}
	keysChanged, storedKeysChanged := false, false
	if opened != nil && len(received) > 0 {
		applied, err := opened.Merge(received)
		if err != nil {
			return nil, err
		}
		for _, secret := range applied {
			keysChanged = keysChanged || strings.HasPrefix(secret.Name, aiKeySecretName(""))
			storedKeysChanged = storedKeysChanged || sshconn.IsStoredKey(secret.Name)
		}
	}
	if err := state.config.Observe(stamps...); err != nil {
		return nil, err
	}

	holdable, _ := holdableRecords(remote.Records)
	notesChanged, err := state.preserveNoteConflicts(holdable)
	if err != nil {
		return nil, err
	}
	result, err := state.config.Merge(holdable)
	if err != nil {
		return nil, err
	}
	if len(result.Applied) > 0 || notesChanged {
		if err := state.reloadShared(); err != nil {
			return nil, err
		}
		state.reloadOtherWindows()
	}
	if storedKeysChanged {
		state.reloadStoredIdentities()
	}
	if keysChanged && state.ai.store != nil {
		// A key was changed on another computer: the providers keep it in memory.
		state.pauseSave++
		state.reloadAI()
		state.pauseSave--
	}
	if err := state.mergeKnownHosts(remote.KnownHosts); err != nil {
		return nil, err
	}
	if _, err := state.config.Prune(tombstoneKeep); err != nil {
		return nil, err
	}
	if opened != nil {
		if _, err := opened.Prune(string(configstore.CutoffStamp(state.options.WallNow().Add(-tombstoneKeep)))); err != nil {
			return nil, err
		}
	}

	// What goes back is the union of the two, so nothing the server holds is lost
	// to a computer that could not read it (a locked vault, a newer build).
	records, err := state.config.Records()
	if err != nil {
		return nil, err
	}
	records = append(records, passedOn(records, remote.Records, holdable)...)
	local := backup.Document{Created: state.options.WallNow().UTC(), Device: state.config.Device(), Format: configFormat, Writer: configWriter(), Records: records}
	localHosts := ""
	if data, err := os.ReadFile(state.options.KnownHostsPath); err == nil {
		localHosts = string(data)
	}
	local.KnownHosts = unionLines(remote.KnownHosts, localHosts)
	if opened != nil {
		exported, err := opened.Export()
		if err != nil {
			return nil, err
		}
		for _, secret := range exported {
			if !excludedFromBackup(secret.Name) {
				local.Secrets = append(local.Secrets, secret)
			}
		}
	}
	local.Secrets = unionSecrets(local.Secrets, received)

	newer, err := state.config.Newer(remote.Records)
	if err != nil {
		return nil, err
	}
	if !missing && len(newer) == 0 && !secretsNewer(local.Secrets, remote.Secrets) && !hasNewLines(local.KnownHosts, remote.KnownHosts) {
		return nil, nil
	}
	return &local, nil
}

// passedOn are the server's records that this computer would not hold because it
// cannot read them (a newer build wrote them): they go back as they are, so an
// older build never strips what a newer one put on the server.
func passedOn(local, remote, holdable []configstore.Record) []configstore.Record {
	held := make(map[configstore.Key]bool, len(holdable))
	for _, record := range holdable {
		held[configstore.Key{Kind: record.Kind, ID: record.ID}] = true
	}
	have := make(map[configstore.Key]bool, len(local))
	for _, record := range local {
		have[configstore.Key{Kind: record.Kind, ID: record.ID}] = true
	}
	var passed []configstore.Record
	for _, record := range remote {
		key := configstore.Key{Kind: record.Kind, ID: record.ID}
		if !held[key] && !have[key] {
			passed = append(passed, record)
		}
	}
	return passed
}

// unionLines is remote's lines followed by the lines of local it lacks.
func unionLines(remote, local string) string {
	have := map[string]bool{}
	var lines []string
	for _, text := range []string{remote, local} {
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" && !have[line] {
				have[line] = true
				lines = append(lines, line)
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// reloadShared brings the window's shared data up to date with the store, after
// records came in from another computer. What is open keeps running.
func (state *State) reloadShared() error {
	if err := state.refreshShared(); err != nil {
		return err
	}
	state.save() // what was here and never recorded (a provider of the assistant, say)
	return nil
}

// refreshShared replaces the shared data with the store's without writing back.
func (state *State) refreshShared() error {
	entries, _, err := state.config.Load()
	if err != nil {
		return err
	}
	development := append([]byte(nil), state.configBase.development...)
	saved, base, err := joinState(entries, state.configBase.local)
	if err != nil {
		return err
	}
	base.development = development
	// This window's own value is nobody else's to rewrite: it is carried over so
	// the next save patches its own record rather than emptying it.
	base.windowName, base.window = state.configBase.windowName, state.configBase.window
	if saved.AIProviders == nil && state.recordedProviders(kindAIProvider) > 0 {
		saved.AIProviders = &[]savedAIProvider{} // all it had recorded are gone
	}
	if saved.AICLIProviders == nil && state.recordedProviders(kindAICLIProvider) > 0 {
		saved.AICLIProviders = &[]savedAIProvider{}
	}
	state.restoreShared(saved)
	state.configBase = base
	state.hostDataChanged()
	state.rebuildTabOptions()
	return nil
}

// unionSecrets is every secret of both lists, the later version of each name.
func unionSecrets(local, other []vault.Secret) []vault.Secret {
	byName := make(map[string]vault.Secret, len(local)+len(other))
	for _, list := range [][]vault.Secret{other, local} {
		for _, secret := range list {
			if known, ok := byName[secret.Name]; !ok || known.Stamp < secret.Stamp {
				byName[secret.Name] = secret
			}
		}
	}
	union := make([]vault.Secret, 0, len(byName))
	for _, secret := range byName {
		union = append(union, secret)
	}
	slices.SortFunc(union, func(left, right vault.Secret) int { return strings.Compare(left.Name, right.Name) })
	return union
}

// secretsNewer says whether local holds a secret that remote lacks or has an
// older version of.
func secretsNewer(local, remote []vault.Secret) bool {
	have := make(map[string]string, len(remote))
	for _, secret := range remote {
		have[secret.Name] = secret.Stamp
	}
	for _, secret := range local {
		if stamp, ok := have[secret.Name]; !ok || stamp < secret.Stamp {
			return true
		}
	}
	return false
}

// hasNewLines says whether local has a line that remote lacks.
func hasNewLines(local, remote string) bool {
	have := map[string]bool{}
	for _, line := range strings.Split(remote, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	for _, line := range strings.Split(local, "\n") {
		if line = strings.TrimSpace(line); line != "" && !have[line] {
			return true
		}
	}
	return false
}
