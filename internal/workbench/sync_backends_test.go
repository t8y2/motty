package workbench

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"motty/internal/gist"
)

// fakeBucket is one S3 bucket in memory (path-style, ETag per content, If-Match
// honoured). The signature itself is checked in package s3; here a request
// without a SigV4 Authorization header for the right access key is refused.
type fakeBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
}

func newFakeBucket(t *testing.T) (*httptest.Server, *fakeBucket) {
	t.Helper()
	bucket := &fakeBucket{objects: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket.mu.Lock()
		defer bucket.mu.Unlock()
		fail := func(status int, code string) {
			w.WriteHeader(status)
			fmt.Fprintf(w, "<Error><Code>%s</Code><Message>no</Message></Error>", code)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIATEST/") {
			fail(403, "AccessDenied")
			return
		}
		name, found := strings.CutPrefix(r.URL.Path, "/mybucket/")
		if !found {
			fail(404, "NoSuchBucket")
			return
		}
		tag := func() string { sum := md5.Sum(bucket.objects[name]); return `"` + hex.EncodeToString(sum[:]) + `"` }
		body, exists := bucket.objects[name]
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" && exists {
				fail(412, "PreconditionFailed")
				return
			}
			if match := r.Header.Get("If-Match"); match != "" && (!exists || match != tag()) {
				fail(412, "PreconditionFailed")
				return
			}
			bucket.objects[name], _ = io.ReadAll(r.Body)
			bucket.puts++
			w.Header().Set("ETag", tag())
		case http.MethodGet:
			if !exists {
				fail(404, "NoSuchKey")
				return
			}
			w.Header().Set("ETag", tag())
			w.Write(body)
		}
	}))
	t.Cleanup(server.Close)
	return server, bucket
}

func configureS3(t *testing.T, state *State, server *httptest.Server) {
	t.Helper()
	state.Click(hitSyncConfigureS3)
	if state.modal != modalForm {
		t.Fatal("the S3 form did not open")
	}
	fillForm(state, server.URL, "", "mybucket", "", "")
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool {
		return state.modal == modalForm && state.form.fields[0].label == state.messages.Tools.S3AccessKey
	})
	fillForm(state, "AKIATEST", "s3-secret-value", testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("the S3 settings were refused: %q", state.form.dialogError)
	}
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
}

func TestSyncMergesTwoComputersThroughS3(t *testing.T) {
	server, bucket := newFakeBucket(t)
	first, _, _ := backupTestState(t)
	configureS3(t, first, server)
	if first.sync.kind != syncS3 || first.sync.s3.key != "motty/backup.mottybak" || !first.sync.s3.pathStyle {
		t.Fatalf("settings %+v", first.sync.s3)
	}
	saved, _, _ := loadState(first.options.StatePath)
	if saved.Sync == nil || saved.Sync.S3 == nil || saved.Sync.S3.Bucket != "mybucket" || saved.Sync.S3.AccessKey != "AKIATEST" {
		t.Fatalf("the bucket was not saved: %+v", saved.Sync)
	}
	rawState, _ := os.ReadFile(first.options.StatePath)
	for _, secret := range []string{"s3-secret-value", testBackupPassword} {
		if strings.Contains(string(rawState), secret) {
			t.Fatalf("%q is in the settings database", secret)
		}
	}

	first.terminalScrollback = 10000
	first.save()
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	if first.sync.etag == "" {
		t.Fatal("no version tag after the sync")
	}
	for name, body := range bucket.objects {
		if strings.Contains(string(body), "prod.example.com") {
			t.Fatalf("%s is readable in the bucket", name)
		}
	}
	puts := bucket.puts
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	if bucket.puts != puts {
		t.Fatal("a sync with nothing to send uploaded")
	}

	// A restart keeps the service.
	again := newTestState(t, first.options)
	if again.sync.kind != syncS3 || again.sync.s3.bucket != "mybucket" || again.sync.etag != first.sync.etag {
		t.Fatalf("after a restart: %+v", again.sync)
	}

	second, _, _ := backupTestState(t)
	configureS3(t, second, server)
	syncAndWait(t, second, hitSyncNow, second.messages.Tools.SyncDone)
	if second.terminalScrollback != 10000 || second.sync.etag == "" {
		t.Fatalf("scrollback %d, tag %q", second.terminalScrollback, second.sync.etag)
	}
}

func TestAWrongS3KeyIsReportedWithTheServicesWords(t *testing.T) {
	server, _ := newFakeBucket(t)
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigureS3)
	fillForm(state, server.URL, "", "mybucket", "", "")
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool {
		return state.modal == modalForm && state.form.fields[0].label == state.messages.Tools.S3AccessKey
	})
	fillForm(state, "AKIAWRONG", "secret", testBackupPassword)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return strings.Contains(state.status, "AccessDenied") })
	if !strings.Contains(state.status, state.messages.Tools.SyncErrUnauthorized) {
		t.Fatalf("status %q", state.status)
	}
}

func TestTheS3FormsRefuseAnInsecureEndpointAndAMissingSecret(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigureS3)
	fillForm(state, "http://s3.example.com", "", "mybucket", "", "")
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.SyncErrInsecure {
		t.Fatalf("an http endpoint: %q", state.form.dialogError)
	}
	state.form.set(0, "https://s3.example.com")
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool {
		return state.modal == modalForm && state.form.fields[0].label == state.messages.Tools.S3AccessKey
	})
	fillForm(state, "AKIATEST", "", testBackupPassword)
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.S3ErrFields || state.sync.configured() {
		t.Fatalf("no secret: %q, configured %v", state.form.dialogError, state.sync.configured())
	}
}

// fakeGistAPI is the gist routes of the GitHub API in memory.
type fakeGistAPI struct {
	mu      sync.Mutex
	content string
	commits int
	exists  bool
}

func newFakeGistAPI(t *testing.T) (*httptest.Server, *fakeGistAPI) {
	t.Helper()
	api := &fakeGistAPI{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer ghp_token" {
			w.WriteHeader(401)
			w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		view := func(status int) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{
				"id": "feedbeef", "history": []map[string]string{{"version": fmt.Sprint("v", api.commits)}},
				"files": map[string]any{"motty-backup.mottybak.b64": map[string]any{"content": api.content}},
			})
		}
		var body struct {
			Files map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/gists":
			w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/gists":
			json.NewDecoder(r.Body).Decode(&body)
			api.content, api.exists = body.Files["motty-backup.mottybak.b64"].Content, true
			api.commits++
			view(201)
		case r.URL.Path == "/gists/feedbeef" && api.exists && r.Method == http.MethodGet:
			view(200)
		case r.URL.Path == "/gists/feedbeef" && api.exists && r.Method == http.MethodPatch:
			json.NewDecoder(r.Body).Decode(&body)
			api.content = body.Files["motty-backup.mottybak.b64"].Content
			api.commits++
			view(200)
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, api
}

func configureGist(t *testing.T, state *State, server *httptest.Server, id string) {
	t.Helper()
	state.Click(hitSyncConfigureGist)
	if state.modal != modalForm {
		t.Fatal("the Gist form did not open")
	}
	fillForm(state, "ghp_token", id, "", server.URL, testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("the Gist settings were refused: %q", state.form.dialogError)
	}
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
}

func TestTheFirstGistSyncCreatesTheGistAndAnotherComputerTakesItIn(t *testing.T) {
	server, api := newFakeGistAPI(t)
	first, _, _ := backupTestState(t)
	configureGist(t, first, server, "")
	first.terminalScrollback = 10000
	first.save()
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	if first.sync.gist.id != "feedbeef" || !api.exists {
		t.Fatalf("the gist id was not remembered: %+v", first.sync.gist)
	}
	saved, _, _ := loadState(first.options.StatePath)
	if saved.Sync == nil || saved.Sync.Kind != savedKindGist || saved.Sync.Gist.ID != "feedbeef" {
		t.Fatalf("the gist was not saved: %+v", saved.Sync)
	}
	rawState, _ := os.ReadFile(first.options.StatePath)
	if strings.Contains(string(rawState), "ghp_token") || strings.Contains(api.content, "prod.example.com") {
		t.Fatal("a token is in the settings database, or the backup is readable in the gist")
	}
	commits := api.commits
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	if api.commits != commits {
		t.Fatalf("a sync with nothing to send made a commit (%d)", api.commits-commits)
	}

	second, _, _ := backupTestState(t)
	configureGist(t, second, server, "feedbeef")
	syncAndWait(t, second, hitSyncNow, second.messages.Tools.SyncDone)
	if second.terminalScrollback != 10000 {
		t.Fatalf("scrollback %d", second.terminalScrollback)
	}
}

func TestABadGistTokenSaysSo(t *testing.T) {
	server, _ := newFakeGistAPI(t)
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigureGist)
	fillForm(state, "ghp_wrong", "", "", server.URL, testBackupPassword)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return strings.Contains(state.status, "Bad credentials") })
}

func TestChoosingAnotherServiceReplacesTheFirstAndForgetsItsPassword(t *testing.T) {
	dav, _ := newFakeDAV(t)
	bucket, _ := newFakeBucket(t)
	state, _, _ := backupTestState(t)
	configureSync(t, state, dav)
	if _, found, _ := state.secrets.vault.Get(webdavPasswordSecret); !found {
		t.Fatal("the WebDAV password was not kept")
	}
	configureS3(t, state, bucket)
	if state.sync.kind != syncS3 || state.sync.url != "" || state.sync.etag != "" {
		t.Fatalf("sync %+v", state.sync)
	}
	if _, found, _ := state.secrets.vault.Get(webdavPasswordSecret); found {
		t.Fatal("the WebDAV password stayed after switching")
	}
	document, err := state.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range document.Secrets {
		if !secret.Deleted {
			t.Fatalf("a login reached the backup: %v", document.Secrets)
		}
	}
}

// fakeGiteeAPI is the Gitee snippet API as the application uses it: the token
// is a query parameter, creation answers with an array, and files are a
// JSON-encoded string.
type fakeGiteeAPI struct {
	mu      sync.Mutex
	content string
	exists  bool
}

func newFakeGiteeAPI(t *testing.T) (*httptest.Server, *fakeGiteeAPI) {
	t.Helper()
	api := &fakeGiteeAPI{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		if r.URL.Query().Get("access_token") != "gitee_token" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(401)
			w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		view := func(status int, asArray bool) {
			files, _ := json.Marshal(map[string]any{gist.DefaultFile: map[string]any{"content": api.content, "truncated": false}})
			object := map[string]any{"id": "gitee-123", "history": "", "updated_at": "2026-01-01T00:00:00Z", "files": string(files)}
			w.WriteHeader(status)
			if asArray {
				json.NewEncoder(w).Encode([]any{object})
				return
			}
			json.NewEncoder(w).Encode(object)
		}
		var body struct {
			Description string `json:"description"`
			Public      *bool  `json:"public"`
			Files       map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/gists":
			w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/gists":
			json.NewDecoder(r.Body).Decode(&body)
			if body.Public == nil || *body.Public || body.Description == "" {
				w.WriteHeader(422)
				w.Write([]byte(`{"message":"Validation Failed"}`))
				return
			}
			api.content, api.exists = body.Files[gist.DefaultFile].Content, true
			view(201, true)
		case r.URL.Path == "/gists/gitee-123" && api.exists && r.Method == http.MethodGet:
			view(200, false)
		case r.URL.Path == "/gists/gitee-123" && api.exists && r.Method == http.MethodPatch:
			json.NewDecoder(r.Body).Decode(&body)
			api.content = body.Files[gist.DefaultFile].Content
			view(200, false)
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, api
}

func configureGitee(t *testing.T, state *State, server *httptest.Server, id string) {
	t.Helper()
	state.Click(hitSyncConfigureGitee)
	if state.modal != modalForm {
		t.Fatal("the Gitee form did not open")
	}
	fillForm(state, "gitee_token", id, "", server.URL, testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("the Gitee settings were refused: %q", state.form.dialogError)
	}
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
}

func TestGiteeSyncCreatesAndRemembersTheSnippet(t *testing.T) {
	server, api := newFakeGiteeAPI(t)
	state, _, _ := backupTestState(t)
	configureGitee(t, state, server, "")
	if state.sync.kind != syncGist || state.sync.gist.provider != gist.Gitee {
		t.Fatalf("sync %+v", state.sync)
	}
	syncAndWait(t, state, hitSyncNow, state.messages.Tools.SyncDone)
	if state.sync.gist.id != "gitee-123" || !api.exists {
		t.Fatalf("the snippet id was not remembered: %+v", state.sync.gist)
	}
	saved, _, _ := loadState(state.options.StatePath)
	if saved.Sync == nil || saved.Sync.Kind != savedKindGist || saved.Sync.Gist == nil ||
		saved.Sync.Gist.Provider != savedProviderGitee || saved.Sync.Gist.ID != "gitee-123" {
		t.Fatalf("the Gitee snippet was not saved: %+v", saved.Sync)
	}
	if _, found, _ := state.secrets.vault.Get(giteeTokenSecret); !found {
		t.Fatal("the Gitee token was not kept")
	}
	if _, found, _ := state.secrets.vault.Get(gistTokenSecret); found {
		t.Fatal("a stale GitHub token stayed after switching to Gitee")
	}
}

func TestSwitchingBetweenGitHubAndGiteeForgetsTheOtherToken(t *testing.T) {
	github, _ := newFakeGistAPI(t)
	gitee, _ := newFakeGiteeAPI(t)
	state, _, _ := backupTestState(t)
	configureGist(t, state, github, "")
	configureGitee(t, state, gitee, "")
	if _, found, _ := state.secrets.vault.Get(gistTokenSecret); found {
		t.Fatal("the GitHub token stayed after switching to Gitee")
	}
	if _, found, _ := state.secrets.vault.Get(giteeTokenSecret); !found {
		t.Fatal("the Gitee token was not kept")
	}
	configureGist(t, state, github, "")
	if _, found, _ := state.secrets.vault.Get(giteeTokenSecret); found {
		t.Fatal("the Gitee token stayed after switching to GitHub")
	}
	if _, found, _ := state.secrets.vault.Get(gistTokenSecret); !found {
		t.Fatal("the GitHub token was not kept")
	}
}
