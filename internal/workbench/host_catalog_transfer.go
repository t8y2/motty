package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/vivaui/viva/app"
	"motty/internal/backup"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

const (
	hostCatalogEncryptedFormat  = "motty-host-catalog-encrypted"
	hostCatalogEncryptedVersion = 1
	// The two exports are saved under different names, so the files are told
	// apart on disk: only the encrypted one holds saved passwords.
	plainHostCatalogName     = "motty-hosts.json"
	encryptedHostCatalogName = "motty-hosts-encrypted.json"
)

type hostCatalogSecret struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type hostCatalogDocument struct {
	Format        string              `json:"format"`
	Version       int                 `json:"version"`
	Hosts         []savedHost         `json:"hosts"`
	Groups        []savedHostGroup    `json:"groups,omitempty"`
	Credentials   []savedCredential   `json:"credentials,omitempty"`
	ProxyProfiles []savedProxyProfile `json:"proxy_profiles,omitempty"`
	Secrets       []hostCatalogSecret `json:"secrets,omitempty"`
}

var errHostCatalogTooNew = errors.New("unsupported catalog version")

func (state *State) exportHostCatalog() {
	if state.appAccessBlocked() {
		return
	}
	hosts := state.exportedHosts()
	if len(hosts) == 0 && len(state.hostGroups) == 0 {
		return
	}
	m := state.messages
	state.openForm(formDialog{
		title:       m.HostCatalogExportTitle,
		description: m.HostCatalogExportDescription,
		submit:      m.HostCatalogExportEncrypted,
		backLabel:   m.HostCatalogExportPlain,
		back:        func(state *State, _ *formDialog) { state.exportPlainHostCatalog() },
		run:         (*State).runExportEncryptedHostCatalog,
	},
		formField{label: m.HostCatalogExportPassword, placeholder: m.HostCatalogExportPasswordPlaceholder, secure: true},
		formField{label: m.Tools.BackupPasswordConfirm, secure: true},
	)
}

func (state *State) runExportEncryptedHostCatalog(form *formDialog) string {
	m := state.messages
	password := form.value(0)
	switch {
	case password == "":
		return m.HostCatalogErrPasswordEmpty
	case len(password) < backup.MinPasswordLength:
		return m.Tools.BackupErrPasswordShort
	case password != form.value(1):
		return m.Tools.BackupErrPasswordMismatch
	}
	hosts := state.exportedHosts()
	if len(hosts) == 0 && len(state.hostGroups) == 0 {
		return ""
	}
	if opened := state.secrets.vault; opened != nil && opened.NeedsConversion() {
		return m.Tools.BackupErrVault
	}
	document, err := state.buildHostCatalogDocument(hosts, true)
	if err != nil {
		return state.hostPasswordSaveError(err)
	}
	plain, err := json.Marshal(document)
	if err != nil {
		return fmt.Sprintf(m.HostCatalogExportFailedFmt, err)
	}
	sealed, err := backup.SealEnvelope(hostCatalogEncryptedFormat, hostCatalogEncryptedVersion, plain, password)
	clear(plain)
	if err != nil {
		return state.backupErrorText(err)
	}
	state.writeExportedHostCatalog(sealed, len(document.Hosts), encryptedHostCatalogName)
	return ""
}

func (state *State) exportPlainHostCatalog() {
	hosts := state.exportedHosts()
	if len(hosts) == 0 && len(state.hostGroups) == 0 {
		return
	}
	document, err := state.buildHostCatalogDocument(hosts, false)
	if err != nil {
		state.showFileSaveFailure(state.messages.ExportHosts, err)
		return
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		state.showFileSaveFailure(state.messages.ExportHosts, err)
		return
	}
	data = append(data, '\n')
	state.writeExportedHostCatalog(data, len(document.Hosts), plainHostCatalogName)
}

// writeExportedHostCatalog asks where to save an export and writes it there
// privately; suggestedName is the file name offered.
func (state *State) writeExportedHostCatalog(data []byte, count int, suggestedName string) {
	path, err := state.options.SaveFile(app.SaveFileOptions{
		Title:            state.messages.ExportHosts,
		SuggestedName:    suggestedName,
		DefaultExtension: "json",
		Filters:          []app.FileFilter{{Name: "Motty host catalog", Pattern: "*.json"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err != nil {
		state.showActionFailure(state.messages.ExportHosts, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	if err := writePrivateAtomic(path, data); err != nil {
		state.showFileSaveFailure(state.messages.ExportHosts, err)
		return
	}
	state.status = fmt.Sprintf(state.messages.HostCatalogExportedFmt, count, path)
}

func (state *State) buildHostCatalogDocument(hosts []Host, includeSecrets bool) (hostCatalogDocument, error) {
	document := hostCatalogDocument{
		Format:  hostCatalogFormat,
		Version: hostCatalogSchemaVersion,
		Hosts:   make([]savedHost, 0, len(hosts)),
		Groups:  make([]savedHostGroup, 0, len(state.hostGroups)),
	}
	for _, host := range hosts {
		document.Hosts = append(document.Hosts, savedHostFromHost(host))
	}
	for _, group := range state.hostGroups {
		if len(state.hostSelections) == 0 || catalogContainsGroup(hosts, group.Name) {
			document.Groups = append(document.Groups, savedHostGroup{Name: group.Name, Collapsed: !group.Expanded, Explicit: group.Explicit, Pinned: group.Pinned, Order: group.Order})
		}
	}
	if !includeSecrets {
		return document, nil
	}
	var err error
	document.Credentials, document.ProxyProfiles, document.Secrets, err = state.collectExportedHostSecrets(hosts)
	if err != nil {
		return hostCatalogDocument{}, err
	}
	return document, nil
}

func (state *State) collectExportedHostSecrets(hosts []Host) ([]savedCredential, []savedProxyProfile, []hostCatalogSecret, error) {
	var byName map[string]string
	if opened := state.secrets.vault; opened != nil && opened.Len() > 0 {
		exported, err := opened.Export()
		if err != nil {
			return nil, nil, nil, err
		}
		byName = make(map[string]string, len(exported))
		for _, entry := range exported {
			if !entry.Deleted {
				byName[entry.Name] = entry.Value
			}
		}
	}

	var credentials []savedCredential
	seenCredentials := make(map[string]bool)
	var proxyProfiles []savedProxyProfile
	seenProfiles := make(map[string]bool)
	secrets := make(map[string]string)

	for _, host := range hosts {
		if host.CatalogID != "" && len(byName) > 0 {
			hostPrefix := "host:" + host.CatalogID + ":"
			proxyPrefix := hostProxyPasswordPrefix + host.CatalogID + ":"
			for name, value := range byName {
				if strings.HasPrefix(name, hostPrefix) || strings.HasPrefix(name, proxyPrefix) {
					secrets[name] = value
				}
			}
		}
		if host.Credential != "" && !seenCredentials[host.Credential] {
			if cred, ok := state.credentialByID(host.Credential); ok {
				seenCredentials[cred.ID] = true
				credentials = append(credentials, savedCredential{ID: cred.ID, Name: cred.Name, User: cred.User})
				if value, has := byName[credentialSecretName(cred.ID)]; has {
					secrets[credentialSecretName(cred.ID)] = value
				}
			}
		}
		if host.ProxyProfile != "" && !seenProfiles[host.ProxyProfile] {
			if profile, ok := state.proxyProfileByID(host.ProxyProfile); ok {
				seenProfiles[profile.ID] = true
				proxyProfiles = append(proxyProfiles, savedProxyProfile{
					ID: profile.ID, Name: profile.Name, Kind: proxyKindString(profile.Kind),
					Address: profile.Address, User: profile.User,
				})
				if value, has := byName[proxyProfileSecretName(profile.ID)]; has {
					secrets[proxyProfileSecretName(profile.ID)] = value
				}
			}
		}
	}

	var secretList []hostCatalogSecret
	if len(secrets) > 0 {
		secretList = make([]hostCatalogSecret, 0, len(secrets))
		for name, value := range secrets {
			secretList = append(secretList, hostCatalogSecret{Name: name, Value: value})
		}
		sort.Slice(secretList, func(i, j int) bool { return secretList[i].Name < secretList[j].Name })
	}
	return credentials, proxyProfiles, secretList, nil
}

func (state *State) exportedHosts() []Host {
	if len(state.hostSelections) == 0 {
		return state.hosts
	}
	hosts := make([]Host, 0, len(state.hostSelections))
	for _, host := range state.hosts {
		if state.hostSelected(host.ID) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func savedHostFromHost(host Host) savedHost {
	return savedHost{
		ID: host.CatalogID, Label: host.Label, Group: host.Group,
		Tags: splitHostTags(host), Icon: host.Icon.String(), Color: host.Color,
		Address: host.Address, User: host.User, Port: host.Port, System: host.System.String(),
		savedTransport: host.transport(),
	}
}

func catalogContainsGroup(hosts []Host, name string) bool {
	for _, host := range hosts {
		if hostGroupWithin(strings.TrimSpace(host.Group), name) {
			return true
		}
	}
	return false
}

func (state *State) importHostCatalog() {
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   state.messages.HostCatalogPickerTitle,
		Filters: []app.FileFilter{{Name: "Motty host catalog", Pattern: "*.json"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.showActionFailure(state.messages.HostCatalogPickerTitle, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	data, err := readCatalogBytes(paths[0])
	if err != nil {
		detail := state.messages.ActionFeedback.ImportHostCatalogFailed
		if errors.Is(err, backup.ErrTooLarge) {
			detail = state.messages.ActionFeedback.BackupTooLarge
		}
		state.showFileReadFailure(state.messages.HostCatalogPickerTitle, err, detail)
		return
	}
	if encrypted, err := isEncryptedHostCatalog(data); err != nil {
		detail := state.messages.ActionFeedback.ImportHostCatalogFailed
		if errors.Is(err, errHostCatalogTooNew) {
			detail = state.messages.ActionFeedback.ImportUpdateRequired
		}
		state.showFileReadFailure(state.messages.HostCatalogPickerTitle, err, detail)
		return
	} else if encrypted {
		state.promptEncryptedHostCatalogImport(data)
		return
	}
	document, err := parsePlainHostCatalog(data)
	if err != nil {
		detail := state.messages.ActionFeedback.ImportHostCatalogFailed
		if errors.Is(err, errHostCatalogTooNew) {
			detail = state.messages.ActionFeedback.ImportUpdateRequired
		}
		state.showFileReadFailure(state.messages.HostCatalogPickerTitle, err, detail)
		return
	}
	added, duplicates := state.applyHostCatalog(document)
	state.status = fmt.Sprintf(state.messages.HostCatalogImportedFmt, added, duplicates)
	if added > 0 || len(document.Groups) > 0 {
		state.save()
	}
}

func (state *State) promptEncryptedHostCatalogImport(data []byte) {
	m := state.messages
	state.openForm(formDialog{
		title:       m.HostCatalogImportPasswordTitle,
		description: m.HostCatalogImportPasswordDescription,
		submit:      m.HostCatalogImportUnlock,
		run: func(state *State, form *formDialog) string {
			password := form.value(0)
			if password == "" {
				return m.Tools.BackupErrWrong
			}
			plain, err := backup.OpenEnvelope(hostCatalogEncryptedFormat, hostCatalogEncryptedVersion, data, password)
			if err != nil {
				if errors.Is(err, backup.ErrTooNew) {
					return m.Tools.BackupErrTooNew
				}
				if errors.Is(err, backup.ErrNotBackup) {
					return m.ActionFeedback.ImportHostCatalogFailed
				}
				return m.Tools.BackupErrWrong
			}
			document, err := parseHostCatalogDocument(plain)
			clear(plain)
			if err != nil {
				if errors.Is(err, errHostCatalogTooNew) {
					return m.Tools.BackupErrTooNew
				}
				return m.ActionFeedback.ImportHostCatalogFailed
			}
			added, duplicates, err := state.applyHostCatalogWithSecrets(document)
			if err != nil {
				if errors.Is(err, vault.ErrNeedsConversion) {
					return m.Tools.BackupErrVault
				}
				return state.hostPasswordSaveError(err)
			}
			state.status = fmt.Sprintf(m.HostCatalogImportedFmt, added, duplicates)
			return ""
		},
	}, formField{label: m.HostCatalogExportPassword, secure: true})
}

func readCatalogBytes(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > backup.MaxSize+1<<16 {
		return nil, backup.ErrTooLarge
	}
	return os.ReadFile(path)
}

func isEncryptedHostCatalog(data []byte) (bool, error) {
	var header struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return false, err
	}
	if header.Format != hostCatalogEncryptedFormat {
		return false, nil
	}
	if header.Version > hostCatalogEncryptedVersion {
		return false, fmt.Errorf("%w %d", errHostCatalogTooNew, header.Version)
	}
	if header.Version < 1 {
		return false, fmt.Errorf("unsupported catalog version %d", header.Version)
	}
	return true, nil
}

func readHostCatalog(path string) (hostCatalogDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hostCatalogDocument{}, err
	}
	return parsePlainHostCatalog(data)
}

func parsePlainHostCatalog(data []byte) (hostCatalogDocument, error) {
	document, err := parseHostCatalogDocument(data)
	if err != nil {
		return hostCatalogDocument{}, err
	}
	// Plain unencrypted catalog files never carry secrets.
	document.Credentials = nil
	document.ProxyProfiles = nil
	document.Secrets = nil
	return document, nil
}

func parseHostCatalogDocument(data []byte) (hostCatalogDocument, error) {
	var document hostCatalogDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return hostCatalogDocument{}, err
	}
	if document.Format != hostCatalogFormat {
		return hostCatalogDocument{}, fmt.Errorf("unsupported format %q", document.Format)
	}
	if document.Version > hostCatalogSchemaVersion {
		return hostCatalogDocument{}, fmt.Errorf("%w %d", errHostCatalogTooNew, document.Version)
	}
	if document.Version < 1 {
		return hostCatalogDocument{}, fmt.Errorf("unsupported catalog version %d", document.Version)
	}
	return document, nil
}

func (state *State) applyHostCatalog(document hostCatalogDocument) (added, duplicates int) {
	added, duplicates, _ = state.applyCatalogDocument(document, false)
	return added, duplicates
}

func (state *State) applyHostCatalogWithSecrets(document hostCatalogDocument) (added, duplicates int, err error) {
	return state.applyCatalogDocument(document, true)
}

func (state *State) applyCatalogDocument(document hostCatalogDocument, withSecrets bool) (added, duplicates int, err error) {
	if withSecrets && len(document.Secrets) > 0 {
		if state.secrets.vault == nil {
			return 0, 0, vault.ErrUnavailable
		}
		if state.secrets.vault.NeedsConversion() {
			return 0, 0, vault.ErrNeedsConversion
		}
	}

	prevHosts := append([]Host(nil), state.hosts...)
	prevGroups := append([]hostGroup(nil), state.hostGroups...)
	prevCredentials := append([]credential(nil), state.credentials...)
	prevProxyProfiles := append([]proxyProfile(nil), state.proxyProfiles...)
	prevNextHostID := state.nextHostID
	prevSelectedHostID := state.selectedHostID
	restoreSnapshot := func() {
		state.hosts = prevHosts
		state.hostGroups = prevGroups
		state.credentials = prevCredentials
		state.proxyProfiles = prevProxyProfiles
		state.nextHostID = prevNextHostID
		state.selectedHostID = prevSelectedHostID
		state.hostDataChanged()
	}

	usedIDs := make(map[string]struct{}, len(state.hosts)+len(document.Hosts))
	for _, host := range state.hosts {
		if host.CatalogID != "" {
			usedIDs[host.CatalogID] = struct{}{}
		}
	}
	for _, group := range document.Groups {
		name := normalizeHostGroupPath(group.Name)
		if index := state.hostGroupIndex(name); index >= 0 {
			state.hostGroups[index].Explicit = state.hostGroups[index].Explicit || group.Explicit
		} else {
			state.hostGroups = append(state.hostGroups, hostGroup{Name: name, Expanded: !group.Collapsed, Explicit: group.Explicit, Pinned: group.Pinned, Order: max(0, group.Order)})
		}
	}

	var hostSecrets map[string][]hostCatalogSecret
	var sharedSecrets map[string]string
	if withSecrets && len(document.Secrets) > 0 {
		hostSecrets, sharedSecrets = indexCatalogSecrets(document.Secrets)
	}

	var vaultChanges []vault.Change
	var addedCredentials, addedProfiles []string
	for _, entry := range document.Hosts {
		host, ok := state.hostFromCatalogEntry(entry)
		if !ok {
			continue
		}
		if state.hostIndexSame(host) >= 0 {
			duplicates++
			continue
		}
		host.ID = state.nextHostID
		state.nextHostID++
		host.CatalogID = uniqueCatalogID(host.CatalogID, host, usedIDs)
		state.hosts = append(state.hosts, host)
		state.selectedHostID = host.ID
		added++

		if withSecrets {
			if entry.ID != "" {
				for _, secret := range hostSecrets[entry.ID] {
					vaultChanges = append(vaultChanges, vault.Change{
						Name:  remapHostCatalogSecretName(secret.Name, entry.ID, host.CatalogID),
						Value: secret.Value,
					})
				}
			}
			if host.Credential != "" {
				addedCredentials = append(addedCredentials, host.Credential)
			}
			if host.ProxyProfile != "" {
				addedProfiles = append(addedProfiles, host.ProxyProfile)
			}
		}
	}

	var extraMetadata bool
	if withSecrets && added > 0 {
		for _, entry := range decodeCredentials(document.Credentials) {
			if !containsString(addedCredentials, entry.ID) {
				continue
			}
			_, exists := state.credentialByID(entry.ID)
			if !exists && len(state.credentials) < maxCredentials {
				state.credentials = append(state.credentials, entry)
				extraMetadata = true
				exists = true
			}
			if exists {
				if value, ok := sharedSecrets[credentialSecretName(entry.ID)]; ok {
					vaultChanges = append(vaultChanges, vault.Change{Name: credentialSecretName(entry.ID), Value: value})
				}
			}
		}
		for _, entry := range decodeProxyProfiles(document.ProxyProfiles) {
			if !containsString(addedProfiles, entry.ID) {
				continue
			}
			_, exists := state.proxyProfileByID(entry.ID)
			if !exists && len(state.proxyProfiles) < maxProxyProfiles {
				state.proxyProfiles = append(state.proxyProfiles, entry)
				extraMetadata = true
				exists = true
			}
			if exists {
				if value, ok := sharedSecrets[proxyProfileSecretName(entry.ID)]; ok {
					vaultChanges = append(vaultChanges, vault.Change{Name: proxyProfileSecretName(entry.ID), Value: value})
				}
			}
		}
	}

	if !withSecrets {
		if added > 0 || len(document.Groups) > 0 {
			state.hostDataChanged()
		}
		return added, duplicates, nil
	}

	commit := func(_ int) error {
		if added > 0 || len(document.Groups) > 0 || extraMetadata {
			state.hostDataChanged()
			return state.saveWithResult()
		}
		return nil
	}
	if len(vaultChanges) > 0 {
		if _, err := state.secrets.vault.ApplyMissingWithCommit(vaultChanges, commit); err != nil {
			restoreSnapshot()
			return 0, 0, err
		}
		return added, duplicates, nil
	}
	if err := commit(0); err != nil {
		restoreSnapshot()
		return 0, 0, err
	}
	return added, duplicates, nil
}

func indexCatalogSecrets(secrets []hostCatalogSecret) (byHost map[string][]hostCatalogSecret, shared map[string]string) {
	byHost = make(map[string][]hostCatalogSecret)
	shared = make(map[string]string)
	for _, entry := range secrets {
		switch {
		case strings.HasPrefix(entry.Name, "host:"):
			rest := strings.TrimPrefix(entry.Name, "host:")
			if id, _, ok := strings.Cut(rest, ":"); ok && id != "" {
				byHost[id] = append(byHost[id], entry)
			}
		case strings.HasPrefix(entry.Name, hostProxyPasswordPrefix):
			rest := strings.TrimPrefix(entry.Name, hostProxyPasswordPrefix)
			if id, _, ok := strings.Cut(rest, ":"); ok && id != "" {
				byHost[id] = append(byHost[id], entry)
			}
		case strings.HasPrefix(entry.Name, "credential:") || strings.HasPrefix(entry.Name, "proxy-profile:"):
			shared[entry.Name] = entry.Value
		}
	}
	return byHost, shared
}

func remapHostCatalogSecretName(name, oldID, newID string) string {
	if oldID == newID {
		return name
	}
	if rest, ok := strings.CutPrefix(name, "host:"+oldID+":"); ok {
		return "host:" + newID + ":" + rest
	}
	if rest, ok := strings.CutPrefix(name, hostProxyPasswordPrefix+oldID+":"); ok {
		return hostProxyPasswordPrefix + newID + ":" + rest
	}
	return name
}

func containsString(list []string, target string) bool {
	for _, value := range list {
		if value == target {
			return true
		}
	}
	return false
}

func (state *State) hostFromCatalogEntry(entry savedHost) (Host, bool) {
	entry.Label = strings.TrimSpace(entry.Label)
	entry.Address = strings.TrimSpace(entry.Address)
	entry.User = strings.TrimSpace(entry.User)
	var host Host
	if !host.applyTransport(entry.savedTransport) {
		return Host{}, false
	}
	if entry.Label == "" || !validEndpoint(host.Protocol, entry.Address) || !validCatalogID(entry.ID) {
		return Host{}, false
	}
	icon, iconOK := parseHostIcon(entry.Icon)
	color, colorOK := normalizeHostColor(entry.Color)
	tags, tagsOK := normalizeHostTags(entry.Tags)
	if !iconOK || !tagsOK || !colorOK {
		return Host{}, false
	}
	host.CatalogID, host.Label, host.Group, host.Icon = entry.ID, entry.Label, strings.TrimSpace(entry.Group), icon
	host.Color = color
	host.Address, host.User, host.Port, host.System = entry.Address, entry.User, entry.Port, sshconn.ParseRemoteSystem(entry.System)
	if !validWSLEndpoint(host) {
		return Host{}, false
	}
	host.normalizeEndpoint()
	setHostTags(&host, tags)
	host.finish()
	return host, true
}

func validCatalogID(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 128 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}
