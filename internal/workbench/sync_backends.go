package workbench

import (
	"errors"
	"fmt"
	"strings"

	"motty/internal/backup"
	"motty/internal/cloudsync"
	"motty/internal/gist"
	"motty/internal/i18n"
	"motty/internal/s3"
	"motty/internal/vault"
	"motty/internal/webdav"
)

// The places the encrypted backup can be kept. Each is a
// cloudsync.Store; this file holds what is particular to the settings of S3 and
// Gist and the forms that ask for them. WebDAV, the first one, is in
// backup_ui.go.

type syncKind int8

const (
	syncWebDAV syncKind = iota
	syncS3
	syncGist
)

// The names of the kinds in the state file; WebDAV has none, as it came first.
const (
	savedKindS3   = "s3"
	savedKindGist = "gist"
)

const (
	s3SecretName     = "s3:secret"
	gistTokenSecret  = "gist:token"
	giteeTokenSecret = "gitee:token"
)

// savedProviderGitee is how the state file names the Gitee snippet service;
// an empty provider is GitHub, which came first.
const savedProviderGitee = "gitee"

// s3Settings is the bucket and the key's public half. The secret half is in the
// vault.
type s3Settings struct {
	endpoint, region, bucket, key, accessKey string
	pathStyle                                bool
}

// savedS3 is s3Settings in the state file.
type savedS3 struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region,omitempty"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	PathStyle bool   `json:"path_style,omitempty"`
}

// gistSettings is where the backup is on a snippet service. The token is in the
// vault.
type gistSettings struct {
	provider      gist.Provider
	id, file, api string
}

// savedGist is gistSettings in the state file.
type savedGist struct {
	Provider string `json:"provider,omitempty"`
	ID       string `json:"id,omitempty"`
	File     string `json:"file,omitempty"`
	API      string `json:"api,omitempty"`
}

func (saved *savedS3) settings() s3Settings {
	return s3Settings{endpoint: saved.Endpoint, region: saved.Region, bucket: saved.Bucket, key: saved.Key,
		accessKey: saved.AccessKey, pathStyle: saved.PathStyle}
}

func (settings s3Settings) saved() *savedS3 {
	return &savedS3{Endpoint: settings.endpoint, Region: settings.region, Bucket: settings.bucket, Key: settings.key,
		AccessKey: settings.accessKey, PathStyle: settings.pathStyle}
}

func (saved *savedGist) settings() gistSettings {
	provider := gist.GitHub
	if saved.Provider == savedProviderGitee {
		provider = gist.Gitee
	}
	return gistSettings{provider: provider, id: saved.ID, file: saved.File, api: saved.API}
}

func (settings gistSettings) saved() *savedGist {
	saved := &savedGist{ID: settings.id, File: settings.file, API: settings.api}
	if settings.provider == gist.Gitee {
		saved.Provider = savedProviderGitee
	}
	return saved
}

func (settings gistSettings) tokenSecretName() string {
	if settings.provider == gist.Gitee {
		return giteeTokenSecret
	}
	return gistTokenSecret
}

// gistHolder is a store that may have made a gist during an upload.
type gistHolder interface{ ID() string }

// configured says whether a service is set.
func (view *syncState) configured() bool {
	switch view.kind {
	case syncS3:
		return view.s3.endpoint != "" && view.s3.bucket != ""
	case syncGist:
		return true
	}
	return view.url != ""
}

// secretName is the vault entry that holds the service's password, key or token.
func (view *syncState) secretName() string {
	switch view.kind {
	case syncS3:
		return s3SecretName
	case syncGist:
		return view.gist.tokenSecretName()
	}
	return webdavPasswordSecret
}

// location describes where the backup is kept, for the settings page.
func (view *syncState) location() string {
	switch view.kind {
	case syncS3:
		return strings.TrimSuffix(view.s3.endpoint, "/") + "/" + view.s3.bucket + "/" + strings.TrimPrefix(view.s3.key, "/")
	case syncGist:
		file := view.gist.file
		if file == "" {
			file = gist.DefaultFile
		}
		name := "GitHub Gist"
		if view.gist.provider == gist.Gitee {
			name = "Gitee"
		}
		if view.gist.id == "" {
			return name + " (" + file + ")"
		}
		return name + " " + view.gist.id + " (" + file + ")"
	}
	return strings.TrimSuffix(view.url, "/") + "/" + strings.TrimPrefix(view.path, "/")
}

// fieldsError is what to say when the service's secret or the backup password
// is not in the vault.
func (view *syncState) fieldsError(m i18n.ToolsMessages) string {
	switch view.kind {
	case syncS3:
		return m.S3ErrFields
	case syncGist:
		return m.GistErrFields
	}
	return m.SyncErrFields
}

// newStore builds the client for the service with its secret.
func (view *syncState) newStore(secret string) (cloudsync.Store, error) {
	switch view.kind {
	case syncS3:
		config := view.s3.config(secret)
		config.AllowInsecureHTTP = view.allowInsecureHTTP
		return s3.New(config, nil)
	case syncGist:
		return gist.New(gist.Config{Provider: view.gist.provider, Token: secret, ID: view.gist.id, File: view.gist.file, APIBase: view.gist.api}, nil)
	}
	return webdav.New(webdav.Config{URL: view.url, User: view.user, Password: secret, Path: view.path, AllowInsecureHTTP: view.allowInsecureHTTP}, nil)
}

func (settings s3Settings) config(secret string) s3.Config {
	return s3.Config{Endpoint: settings.endpoint, Region: settings.region, Bucket: settings.bucket, Key: settings.key,
		AccessKey: settings.accessKey, Secret: secret, PathStyle: settings.pathStyle}
}

// switchSync makes next the service, forgetting what was known of the file on
// the previous one, and drops the secrets of the services no longer used.
func (state *State) switchSync(next syncState) {
	if opened := state.secrets.vault; opened != nil && (next.kind != state.sync.kind || next.secretName() != state.sync.secretName()) {
		_ = opened.Delete(state.sync.secretName())
	}
	state.stopSyncTimer()
	state.syncEpoch++
	next.working = false
	state.sync = next
}

// keepSyncSecrets stores the service's secret and the backup password in the
// vault; an empty one keeps what is there. It returns the text of the problem,
// or "" when it went through.
func (state *State) keepSyncSecrets(secretName, secret, backupPassword string) string {
	m := state.messages.Tools
	opened := state.secrets.vault
	if opened == nil {
		return m.SyncErrNeedsVault
	}
	for _, entry := range [...]struct{ name, value string }{{secretName, secret}, {backupPasswordSecret, backupPassword}} {
		if entry.value == "" {
			continue
		}
		if err := opened.Set(entry.name, entry.value); err != nil {
			if errors.Is(err, vault.ErrNeedsConversion) {
				return m.BackupErrVault
			}
			return fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
		}
	}
	return ""
}

// hasSyncSecret says whether the vault already holds the entry.
func (state *State) hasSyncSecret(name string) bool {
	if state.secrets.vault == nil {
		return false
	}
	_, found, err := state.secrets.vault.Get(name)
	return err == nil && found
}

// --- S3 ------------------------------------------------------------------

// openS3Configure asks for the bucket, then (a second form) for the access key.
func (state *State) openS3Configure() {
	m := state.messages.Tools
	current := state.sync.s3
	if state.sync.kind != syncS3 {
		current = s3Settings{pathStyle: true}
	}
	form := state.openForm(formDialog{
		title: m.S3DialogTitle, description: m.S3DialogDescription, submit: m.SyncSave,
		run:        (*State).runS3Bucket,
		checkLabel: m.SyncAllowInsecureHTTP, checkDescription: m.SyncInsecureHTTPDescription,
		checked: state.sync.kind == syncS3 && state.sync.allowInsecureHTTP,
	},
		formField{label: m.S3Endpoint, placeholder: "https://s3.eu-west-1.amazonaws.com"},
		formField{label: m.S3Region, placeholder: "us-east-1"},
		formField{label: m.S3Bucket},
		formField{label: m.S3Key, placeholder: s3.DefaultKey},
		formField{label: m.S3PathStyle, placeholder: "yes"},
	)
	form.set(0, current.endpoint)
	form.set(1, current.region)
	form.set(2, current.bucket)
	form.set(3, current.key)
	if current.endpoint != "" {
		form.set(4, map[bool]string{true: "yes", false: "no"}[current.pathStyle])
	}
}

func (state *State) runS3Bucket(form *formDialog) string {
	m := state.messages.Tools
	endpoint := strings.TrimSpace(form.value(0))
	if endpoint == "" {
		state.clearSync()
		return ""
	}
	settings := s3Settings{
		endpoint: endpoint, region: strings.TrimSpace(form.value(1)), bucket: strings.TrimSpace(form.value(2)),
		key: strings.TrimSpace(form.value(3)), accessKey: state.sync.s3.accessKey,
	}
	if settings.key == "" {
		settings.key = s3.DefaultKey
	}
	switch strings.ToLower(strings.TrimSpace(form.value(4))) {
	case "", "yes", "y", "true", "1":
		settings.pathStyle = true
	case "no", "n", "false", "0":
	default:
		return m.S3ErrFields
	}
	if settings.bucket == "" {
		return m.S3ErrFields
	}
	check := settings.config("x")
	check.AccessKey = "x"
	check.AllowInsecureHTTP = form.checked
	if _, err := s3.New(check, nil); err != nil {
		if errors.Is(err, cloudsync.ErrInsecure) {
			return m.SyncErrInsecure
		}
		return m.SyncErrAddress
	}
	// The second form opens once this one has closed, in a frame of its own, so
	// that its first field takes the focus like any newly opened dialog.
	allowInsecureHTTP := form.checked
	state.aiPost(func(state *State) { state.openS3Login(settings, allowInsecureHTTP) })
	return ""
}

func (state *State) openS3Login(settings s3Settings, allowInsecureHTTP bool) {
	m := state.messages.Tools
	form := state.openForm(formDialog{
		title: m.S3LoginTitle, description: m.S3LoginDescription, submit: m.SyncSave,
		run: func(state *State, form *formDialog) string {
			return state.runS3Login(form, settings, allowInsecureHTTP)
		},
	},
		formField{label: m.S3AccessKey},
		formField{label: m.S3Secret, secure: true},
		formField{label: m.BackupPassword, placeholder: m.BackupPasswordPlaceholder, secure: true},
	)
	form.set(0, settings.accessKey)
}

func (state *State) runS3Login(form *formDialog, settings s3Settings, allowInsecureHTTP bool) string {
	m := state.messages.Tools
	settings.accessKey = strings.TrimSpace(form.value(0))
	secret, backupPassword := form.value(1), form.value(2)
	// A secret or backup password left empty keeps the one in the vault, as long
	// as there is one.
	haveSecret := state.sync.kind == syncS3 && state.hasSyncSecret(s3SecretName)
	haveBackup := state.hasSyncSecret(backupPasswordSecret)
	if settings.accessKey == "" || secret == "" && !haveSecret || backupPassword == "" && !haveBackup {
		return m.S3ErrFields
	}
	if backupPassword != "" && len(backupPassword) < backup.MinPasswordLength {
		return m.BackupErrPasswordShort
	}
	if message := state.keepSyncSecrets(s3SecretName, secret, backupPassword); message != "" {
		return message
	}
	if state.sync.kind != syncS3 || state.sync.s3 != settings {
		state.switchSync(syncState{kind: syncS3, s3: settings})
	}
	state.sync.allowInsecureHTTP = allowInsecureHTTP
	state.sync.rowText = ""
	state.save()
	state.testSync()
	return ""
}

// --- Gist and Gitee ------------------------------------------------------

func (state *State) openGistConfigure() {
	state.openSnippetConfigure(gist.GitHub)
}

func (state *State) openGiteeConfigure() {
	state.openSnippetConfigure(gist.Gitee)
}

func (state *State) openSnippetConfigure(provider gist.Provider) {
	m := state.messages.Tools
	title, description := m.GistDialogTitle, m.GistDialogDescription
	idLabel := m.GistID
	if provider == gist.Gitee {
		title, description, idLabel = m.GiteeDialogTitle, m.GiteeDialogDescription, m.GiteeID
	}
	fields := []formField{
		{label: m.GistToken, secure: true},
		{label: idLabel},
		{label: m.GistFile, placeholder: gist.DefaultFile},
		{label: m.GistAPI, placeholder: gist.DefaultAPIBaseFor(provider)},
	}
	fields = append(fields, formField{label: m.BackupPassword, placeholder: m.BackupPasswordPlaceholder, secure: true})
	form := state.openForm(formDialog{
		title: title, description: description, submit: m.SyncSave,
		run: func(state *State, form *formDialog) string { return state.runSnippetConfigure(provider, form) },
	}, fields...)
	if state.sync.kind == syncGist && state.sync.gist.provider == provider {
		form.set(1, state.sync.gist.id)
		form.set(2, state.sync.gist.file)
		form.set(3, state.sync.gist.api)
	}
}

func (state *State) runSnippetConfigure(provider gist.Provider, form *formDialog) string {
	m := state.messages.Tools
	settings := gistSettings{provider: provider, id: strings.TrimSpace(form.value(1)), file: strings.TrimSpace(form.value(2)), api: strings.TrimSpace(form.value(3))}
	token, backupPassword := strings.TrimSpace(form.value(0)), form.value(4)
	if settings.file == gist.DefaultFile {
		settings.file = ""
	}
	if settings.api == gist.DefaultAPIBaseFor(provider) {
		settings.api = ""
	}
	secretName := settings.tokenSecretName()
	haveToken := state.sync.kind == syncGist && state.sync.gist.provider == provider && state.hasSyncSecret(secretName)
	haveBackup := state.hasSyncSecret(backupPasswordSecret)
	if token == "" && !haveToken || backupPassword == "" && !haveBackup {
		return m.GistErrFields
	}
	if backupPassword != "" && len(backupPassword) < backup.MinPasswordLength {
		return m.BackupErrPasswordShort
	}
	if _, err := gist.New(gist.Config{Provider: provider, Token: "x", ID: settings.id, File: settings.file, APIBase: settings.api}, nil); err != nil {
		if errors.Is(err, cloudsync.ErrInsecure) {
			return m.SyncErrInsecure
		}
		return m.SyncErrAddress
	}
	if message := state.keepSyncSecrets(secretName, token, backupPassword); message != "" {
		return message
	}
	if state.sync.kind != syncGist || state.sync.gist != settings {
		state.switchSync(syncState{kind: syncGist, gist: settings})
	}
	state.sync.rowText = ""
	state.save()
	state.testSync()
	return ""
}
