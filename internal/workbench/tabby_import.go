package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/vivaui/viva/app"
	"motty/internal/keyidentity"
	"motty/internal/sessionimport"
	"motty/internal/sshconfig"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

func (state *State) importTabby() {
	m := state.messages.Tools
	state.modal = modalNone
	title := fmt.Sprintf(m.ImportPickerFmt, importClientName(sessionimport.Tabby))
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: title, AllowMultiple: true})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		state.status = m.ImportCanceled
		return
	}
	if err != nil {
		state.showActionFailure(title, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	if len(paths) > 1 {
		state.readSessionImport(sessionimport.Tabby, paths, sessionimport.ParseOptions{})
		return
	}
	source, err := sessionimport.OpenTabbyConfig(paths[0])
	if err != nil {
		state.showFileReadFailure(title, err, fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, "Tabby"))
		return
	}
	if source.HasVault() {
		state.promptTabbyVault(source)
		return
	}
	state.importTabbyProfiles(source)
}

func (state *State) importTabbyProfiles(source *sessionimport.TabbyConfigImport) {
	result, err := source.ReadProfiles()
	if err != nil {
		m := state.messages.Tools
		state.showFileReadFailure(fmt.Sprintf(m.ImportPickerFmt, "Tabby"), err,
			fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, "Tabby"))
		return
	}
	state.prepareSessionImport(sessionimport.Tabby, result, false)
}

func (state *State) promptTabbyVault(source *sessionimport.TabbyConfigImport) {
	m := state.messages.Tools
	form := formDialog{
		title: m.TabbyVaultTitle, description: m.TabbyVaultDescription, submit: m.TabbyVaultUnlock,
		run: func(state *State, form *formDialog) string {
			passphrase := []byte(form.value(0))
			form.set(0, "")
			defer clear(passphrase)
			if len(passphrase) == 0 {
				return m.TabbyVaultPassword
			}
			result, err := source.Unlock(passphrase)
			if errors.Is(err, sessionimport.ErrTabbyVaultUnlock) {
				return m.TabbyVaultUnlockFailed
			}
			if err != nil {
				return m.TabbyVaultReadFailed
			}
			state.closeModal()
			state.prepareSessionImport(sessionimport.Tabby, result, true)
			return ""
		},
	}
	if !source.RequiresUnlock() {
		form.description = m.TabbyVaultOptional
		form.backLabel = m.TabbyVaultSkip
		form.back = func(state *State, _ *formDialog) {
			state.importTabbyProfiles(source)
		}
	}
	state.openForm(form, formField{label: m.TabbyVaultPassword, secure: true})
}

func tabbyImportHasKeys(entries []sessionimport.Entry) bool {
	return slices.ContainsFunc(entries, func(entry sessionimport.Entry) bool {
		return entry.SSHSettings != nil && len(entry.SSHSettings.IdentityFiles) > 0
	})
}

// planTabbyKeys prepares copies of the referenced local key files and updates
// their host references in the same commit.
func (state *State) planTabbyKeys(entries []sessionimport.Entry, plan *hostImportPlan) ([]vault.Change, error) {
	hosts := make(map[sessionImportHostKey]Host, len(state.hosts)+len(plan.hosts))
	for _, host := range state.hosts {
		if host.isSSH() {
			hosts[sessionImportHostKeyFor(host)] = host
		}
	}
	for _, host := range plan.hosts {
		if host.isSSH() {
			hosts[sessionImportHostKeyFor(host)] = host
		}
	}
	sources := make(map[string]bool)
	var paths []string
	matched := make(map[uint32]map[string]bool)
	for _, entry := range entries {
		key := sessionImportHostKeyFor(Host{Label: entry.Name, Address: entry.Host, User: entry.User, Port: uint16(entry.Port)})
		host, ok := hosts[key]
		if !ok || entry.SSHSettings == nil {
			continue
		}
		for _, path := range entry.SSHSettings.IdentityFiles {
			if !sources[path] {
				paths = append(paths, path)
				sources[path] = true
			}
			if matched[host.ID] == nil {
				matched[host.ID] = make(map[string]bool)
			}
			matched[host.ID][path] = true
		}
	}

	refs := make(map[string]string, len(paths))
	var changes []vault.Change
	var skipped int
	for _, path := range paths {
		ref, keyChanges, err := state.planTabbyPrivateKey(path)
		if err != nil {
			return nil, err
		}
		if ref == "" {
			skipped++
			continue
		}
		refs[path] = ref
		changes = append(changes, keyChanges...)
	}
	plan.hosts = slices.Clone(plan.hosts)
	for index, host := range plan.hosts {
		if options := migratedTabbyKeyOptions(host, matched[host.ID], refs); options != nil {
			plan.hosts[index].SSHOptions = options
		}
	}
	plan.hostUpdates = make(map[uint32]*sshconfig.ConnectionOptions)
	for _, host := range state.hosts {
		if options := migratedTabbyKeyOptions(host, matched[host.ID], refs); options != nil {
			plan.hostUpdates[host.ID] = options
		}
	}
	if skipped > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(state.messages.Tools.TabbyImportKeysSkippedFmt, skipped))
	}
	return changes, nil
}

func migratedTabbyKeyOptions(host Host, matched map[string]bool, refs map[string]string) *sshconfig.ConnectionOptions {
	if host.SSHOptions == nil || len(matched) == 0 {
		return nil
	}
	var options *sshconfig.ConnectionOptions
	for index, path := range host.SSHOptions.IdentityFiles {
		if ref := refs[path]; matched[path] && ref != "" {
			if options == nil {
				options = host.SSHOptions.Clone()
			}
			options.IdentityFiles[index] = ref
		}
	}
	return options
}

func (state *State) planTabbyPrivateKey(path string) (string, []vault.Change, error) {
	// Avoid opening named pipes or devices reached through a session file.
	file, err := os.Stat(path)
	if err != nil || !file.Mode().IsRegular() {
		return "", nil, nil
	}
	data, err := keyidentity.ReadFile(path)
	if err != nil {
		return "", nil, nil
	}
	defer clear(data)
	name := filepath.Base(path)
	info, err := keyidentity.InspectData(name, data)
	if err != nil || info.SecurityKey {
		return "", nil, nil
	}
	// A user certificate is a single authorized_keys line. Reject unusual
	// companions before opening them, just like the private key itself.
	const maxCertificateSize = 1 << 20
	certFile, err := os.Stat(path + "-cert.pub")
	if err != nil && !errors.Is(err, os.ErrNotExist) ||
		err == nil && (!certFile.Mode().IsRegular() || certFile.Size() > maxCertificateSize) {
		return "", nil, nil
	}
	certificate, err := companionCertificate(path, info.PublicKey)
	if err != nil {
		// Keep the existing file path so the certificate setup is unchanged.
		return "", nil, nil
	}
	key := storedKey{Name: name, Key: string(data), Certificate: certificate}
	ref, err := state.tabbyStoredKeyRef(key, info.Fingerprint)
	if err != nil {
		return "", nil, err
	}
	changes := []vault.Change{{Name: ref, Value: marshalStoredKey(key)}}
	if !info.Encrypted {
		return ref, changes, nil
	}
	vaultFile := state.secrets.vault
	if _, found, err := vaultFile.Get(passphraseSecretName(ref)); err != nil || found {
		return ref, changes, err
	}
	// If Motty already knows the file's passphrase, carry it over to its copy.
	password, found, err := vaultFile.Get(passphraseSecretName(path))
	if err != nil {
		return "", nil, err
	}
	if found {
		if _, err := keyidentity.Parse(data, []byte(password)); err == nil {
			changes = append(changes, vault.Change{Name: passphraseSecretName(ref), Value: password})
		}
	}
	return ref, changes, nil
}

// Reuse an existing copy of the same key and certificate. Otherwise identical
// imports use the same reference, so concurrent windows and retries converge.
func (state *State) tabbyStoredKeyRef(key storedKey, fingerprint string) (string, error) {
	for _, identity := range state.identities {
		if !identity.Stored || identity.Fingerprint != fingerprint {
			continue
		}
		stored, err := readStoredKeyRecord(state.secrets.vault, identity.Path)
		if err != nil {
			return "", err
		}
		if stored.Key == key.Key && stored.Certificate == key.Certificate {
			return identity.Path, nil
		}
	}
	key.Name = ""
	digest := sha256.Sum256([]byte(marshalStoredKey(key)))
	return sshconn.StoredKeyPrefix + hex.EncodeToString(digest[:]), nil
}
