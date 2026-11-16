package gist

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"motty/internal/cloudsync"
)

const testToken = "github_pat_test_token"

// fakeGitHub is the gist part of the GitHub REST API: bearer authentication, a
// secret gist created by POST, edited by PATCH (files not named stay), a commit
// history whose newest version changes with every edit, and JSON error bodies.
type fakeGitHub struct {
	mu       sync.Mutex
	gists    map[string]*fakeGist
	requests []string
}

type fakeGist struct {
	files   map[string]string
	commits []string
}

func (g *fakeGist) json(id string) map[string]any {
	files := map[string]any{}
	for name, content := range g.files {
		files[name] = map[string]any{"filename": name, "content": content, "size": len(content), "truncated": false}
	}
	history := []map[string]string{}
	for i := len(g.commits) - 1; i >= 0; i-- {
		history = append(history, map[string]string{"version": g.commits[i]})
	}
	return map[string]any{"id": id, "public": false, "updated_at": "2026-01-01T00:00:00Z", "history": history, "files": files}
}

func (g *fakeGist) commit() {
	sum := sha1.Sum([]byte(fmt.Sprint(len(g.commits), g.files)))
	g.commits = append(g.commits, hex.EncodeToString(sum[:]))
}

func newFakeGitHub(t *testing.T) (*httptest.Server, *fakeGitHub) {
	t.Helper()
	fake := &fakeGitHub{gists: map[string]*fakeGist{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.requests = append(fake.requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		reply := func(status int, v any) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(v)
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			reply(401, map[string]string{"message": "Bad credentials"})
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" {
			reply(415, map[string]string{"message": "Unsupported"})
			return
		}
		var body struct {
			Description string `json:"description"`
			Public      *bool  `json:"public"`
			Files       map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				reply(400, map[string]string{"message": "Problems parsing JSON"})
				return
			}
		}
		id := strings.TrimPrefix(r.URL.Path, "/gists/")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/gists":
			reply(200, []any{})
		case r.Method == http.MethodPost && r.URL.Path == "/gists":
			if body.Public == nil || *body.Public || len(body.Files) == 0 {
				reply(422, map[string]string{"message": "Validation Failed"})
				return
			}
			g := &fakeGist{files: map[string]string{}}
			for name, file := range body.Files {
				g.files[name] = file.Content
			}
			g.commit()
			fake.gists["abc123"] = g
			reply(201, g.json("abc123"))
		case strings.HasPrefix(r.URL.Path, "/gists/"):
			g, ok := fake.gists[id]
			if !ok {
				reply(404, map[string]string{"message": "Not Found"})
				return
			}
			switch r.Method {
			case http.MethodGet:
				reply(200, g.json(id))
			case http.MethodPatch:
				for name, file := range body.Files {
					g.files[name] = file.Content
				}
				g.commit()
				reply(200, g.json(id))
			}
		default:
			reply(404, map[string]string{"message": "Not Found"})
		}
	}))
	t.Cleanup(server.Close)
	return server, fake
}

func newTestClient(t *testing.T, server *httptest.Server, id string) *Client {
	t.Helper()
	client, err := New(Config{Token: testToken, ID: id, APIBase: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestTheFirstUploadCreatesASecretGistAndLaterOnesUpdateIt(t *testing.T) {
	server, fake := newFakeGitHub(t)
	client := newTestClient(t, server, "")
	ctx := context.Background()
	if err := client.Test(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Get(ctx); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("get with no gist: %v", err)
	}
	sealed := []byte{0, 1, 2, 250, 251, 252, '{', '}'}
	first, err := client.Put(ctx, sealed, "")
	if err != nil || first == "" || client.ID() != "abc123" {
		t.Fatalf("create: tag %q id %q %v", first, client.ID(), err)
	}
	if got := fake.gists["abc123"].files[DefaultFile]; got != base64.StdEncoding.EncodeToString(sealed) {
		t.Fatalf("the file holds %q, not the base64 of the backup", got)
	}
	data, tag, err := client.Get(ctx)
	if err != nil || string(data) != string(sealed) || tag != first {
		t.Fatalf("get: %v %q %v", data, tag, err)
	}
	second, err := client.Put(ctx, []byte("newer"), first)
	if err != nil || second == first {
		t.Fatalf("update: %q %v", second, err)
	}
	data, tag, _ = client.Get(ctx)
	if string(data) != "newer" || tag != second {
		t.Fatalf("after the update: %q %q", data, tag)
	}
}

func TestAnUpdateNeverReplacesARevisionItHasNotSeen(t *testing.T) {
	server, fake := newFakeGitHub(t)
	client := newTestClient(t, server, "")
	ctx := context.Background()
	first, _ := client.Put(ctx, []byte("one"), "")
	// Another computer saves.
	other := newTestClient(t, server, "abc123")
	if _, err := other.Put(ctx, []byte("from elsewhere"), first); err != nil {
		t.Fatal(err)
	}
	patches := 0
	for _, request := range fake.requests {
		if strings.HasPrefix(request, "PATCH") {
			patches++
		}
	}
	if _, err := client.Put(ctx, []byte("stale"), first); !errors.Is(err, cloudsync.ErrChanged) {
		t.Fatalf("a stale update: %v", err)
	}
	for _, request := range fake.requests {
		if strings.HasPrefix(request, "PATCH") {
			patches--
		}
	}
	if patches != 0 {
		t.Fatal("the refused update still reached the gist")
	}
	if data, _, _ := other.Get(ctx); string(data) != "from elsewhere" {
		t.Fatalf("the gist holds %q", data)
	}
}

func TestOtherFilesInTheGistAreLeftAlone(t *testing.T) {
	server, fake := newFakeGitHub(t)
	g := &fakeGist{files: map[string]string{"notes.txt": "keep me"}}
	g.commit()
	fake.gists["abc123"] = g
	client := newTestClient(t, server, "abc123")
	if _, _, err := client.Get(context.Background()); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("a gist without the backup file: %v", err)
	}
	if _, err := client.Put(context.Background(), []byte("x"), ""); err != nil {
		t.Fatal(err)
	}
	if g.files["notes.txt"] != "keep me" || len(g.files) != 2 {
		t.Fatalf("files %v", g.files)
	}
}

func TestMissingGistAndBadTokenSayWhatIsWrong(t *testing.T) {
	server, _ := newFakeGitHub(t)
	ctx := context.Background()
	missing := newTestClient(t, server, "dead")
	if _, _, err := missing.Get(ctx); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("get of a missing gist: %v", err)
	}
	if err := missing.Test(ctx); err == nil || errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("test of a missing gist: %v", err)
	}
	if _, err := missing.Put(ctx, []byte("x"), "someversion"); !errors.Is(err, cloudsync.ErrChanged) {
		t.Fatalf("update of a gist that went away: %v", err)
	}

	bad, err := New(Config{Token: "wrong", APIBase: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{"test": bad.Test(ctx), "put": func() error { _, e := bad.Put(ctx, []byte("x"), ""); return e }()} {
		if !errors.Is(err, cloudsync.ErrUnauthorized) || !strings.Contains(err.Error(), "Bad credentials") {
			t.Errorf("%s with a bad token: %v", name, err)
		}
	}
}

func TestABackupTooBigForAGistFileIsRefusedBeforeItIsSent(t *testing.T) {
	server, fake := newFakeGitHub(t)
	client := newTestClient(t, server, "")
	_, err := client.Put(context.Background(), make([]byte, 800_000), "") // 1.07 MB as base64
	if !errors.Is(err, cloudsync.ErrTooLarge) || len(fake.requests) != 0 {
		t.Fatalf("%v, %d requests", err, len(fake.requests))
	}
	if _, err := client.Put(context.Background(), make([]byte, 700_000), ""); err != nil { // 933 KB
		t.Fatal(err)
	}
}

func TestTheAPIAddressIsHTTPSOrLoopback(t *testing.T) {
	if _, err := New(Config{Token: "t", APIBase: "http://api.example.com"}, nil); !errors.Is(err, cloudsync.ErrInsecure) {
		t.Fatalf("http: %v", err)
	}
	if _, err := New(Config{Token: "t", APIBase: "https://ghe.example.com/api/v3"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Token: "t", APIBase: "http://127.0.0.1:1"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Token: ""}, nil); err == nil {
		t.Fatal("no token accepted")
	}
	if _, err := New(Config{Token: "t", ID: "../users"}, nil); err == nil {
		t.Fatal("a path in the gist id accepted")
	}
}

func TestAFirstUploadToAnExistingGistDoesNotReplaceTheFileIfItIsThere(t *testing.T) {
	server, _ := newFakeGitHub(t)
	ctx := context.Background()
	first := newTestClient(t, server, "")
	if _, err := first.Put(ctx, []byte("mine"), ""); err != nil {
		t.Fatal(err)
	}
	second := newTestClient(t, server, "abc123")
	if _, err := second.Put(ctx, []byte("theirs, later"), ""); !errors.Is(err, cloudsync.ErrChanged) {
		t.Fatalf("creating over an existing file: %v", err)
	}
	if data, _, _ := second.Get(ctx); string(data) != "mine" {
		t.Fatalf("the gist holds %q", data)
	}
}

const testGiteeToken = "gitee_test_token"

// fakeGitee is the snippet part of the Gitee API: the token is a query
// parameter, a created snippet is returned inside an array, and files are a
// JSON-encoded string rather than an object.
type fakeGitee struct {
	mu       sync.Mutex
	gists    map[string]*fakeGist
	requests []string
}

func (g *fakeGist) giteeJSON(id string) map[string]any {
	files := map[string]any{}
	for name, content := range g.files {
		files[name] = map[string]any{"filename": name, "content": content, "truncated": false}
	}
	encoded, _ := json.Marshal(files)
	return map[string]any{"id": id, "public": false, "updated_at": "2026-01-01T00:00:00Z", "history": "", "files": string(encoded)}
}

func newFakeGitee(t *testing.T) (*httptest.Server, *fakeGitee) {
	t.Helper()
	fake := &fakeGitee{gists: map[string]*fakeGist{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.requests = append(fake.requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		reply := func(status int, v any) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(v)
		}
		if r.URL.Query().Get("access_token") != testGiteeToken || r.Header.Get("Authorization") != "" {
			reply(401, map[string]string{"message": "Bad credentials"})
			return
		}
		var body struct {
			Description string `json:"description"`
			Public      *bool  `json:"public"`
			Files       map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				reply(400, map[string]string{"message": "Problems parsing JSON"})
				return
			}
		}
		id := strings.TrimPrefix(r.URL.Path, "/gists/")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/gists":
			reply(200, []any{})
		case r.Method == http.MethodPost && r.URL.Path == "/gists":
			if body.Public == nil || *body.Public || body.Description == "" || len(body.Files) == 0 {
				reply(422, map[string]string{"message": "Validation Failed"})
				return
			}
			g := &fakeGist{files: map[string]string{}}
			for name, file := range body.Files {
				g.files[name] = file.Content
			}
			fake.gists["gitee-123"] = g
			reply(201, []any{g.giteeJSON("gitee-123")})
		case strings.HasPrefix(r.URL.Path, "/gists/"):
			g, ok := fake.gists[id]
			if !ok {
				reply(404, map[string]string{"message": "Not Found"})
				return
			}
			switch r.Method {
			case http.MethodGet:
				reply(200, g.giteeJSON(id))
			case http.MethodPatch:
				for name, file := range body.Files {
					g.files[name] = file.Content
				}
				reply(200, g.giteeJSON(id))
			}
		default:
			reply(404, map[string]string{"message": "Not Found"})
		}
	}))
	t.Cleanup(server.Close)
	return server, fake
}

func TestGiteeSnippetsUseTheirAPIShape(t *testing.T) {
	server, fake := newFakeGitee(t)
	client, err := New(Config{Provider: Gitee, Token: testGiteeToken, APIBase: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.Test(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Get(ctx); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("get with no snippet: %v", err)
	}
	sealed := []byte{0, 1, 2, 250, 251, 252, '{', '}'}
	first, err := client.Put(ctx, sealed, "")
	if err != nil || first == "" || client.ID() != "gitee-123" {
		t.Fatalf("create: tag %q id %q %v", first, client.ID(), err)
	}
	if got := fake.gists["gitee-123"].files[DefaultFile]; got != base64.StdEncoding.EncodeToString(sealed) {
		t.Fatalf("the snippet holds %q, not the base64 of the backup", got)
	}
	data, tag, err := client.Get(ctx)
	if err != nil || string(data) != string(sealed) || tag != first {
		t.Fatalf("get: %q %q %v", data, tag, err)
	}
	second, err := client.Put(ctx, []byte("newer"), first)
	if err != nil || second == first {
		t.Fatalf("update: %q %v", second, err)
	}
	if _, err := client.Put(ctx, []byte("stale"), first); !errors.Is(err, cloudsync.ErrChanged) {
		t.Fatalf("a stale update: %v", err)
	}
	data, tag, err = client.Get(ctx)
	if err != nil || string(data) != "newer" || tag != second {
		t.Fatalf("after the update: %q %q %v", data, tag, err)
	}
	for _, request := range fake.requests {
		if !strings.Contains(request, "access_token="+testGiteeToken) {
			t.Fatalf("a request did not carry the token: %s", request)
		}
	}
}

func TestGiteeUsesItsOwnDefaultAPIAndAcceptsItsSnippetIDs(t *testing.T) {
	client, err := New(Config{Provider: Gitee, Token: "t"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := client.base.String(); got != GiteeAPIBase {
		t.Fatalf("default API base %q", got)
	}
	if _, err := New(Config{Provider: Gitee, Token: "t", ID: "gitee-123_ab"}, nil); err != nil {
		t.Fatalf("a Gitee snippet id was refused: %v", err)
	}
	if _, err := New(Config{Provider: Gitee, Token: "t", ID: "../users"}, nil); err == nil {
		t.Fatal("a path in the snippet id accepted")
	}
}
