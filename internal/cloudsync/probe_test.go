package cloudsync_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"motty/internal/cloudsync"
	"motty/internal/gist"
	"motty/internal/s3"
	"motty/internal/webdav"
)

func TestReadWriteProbePreservesRealBackupAndCleansTemporaryObjects(t *testing.T) {
	for _, provider := range []string{"webdav", "s3", "github", "gitee"} {
		for _, cleanupFails := range []bool{false, true} {
			t.Run(provider+map[bool]string{false: "/success", true: "/cleanup-failure"}[cleanupFails], func(t *testing.T) {
				var mu sync.Mutex
				objects := map[string]string{}
				writes, deletes := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					snippets := provider == "github" || provider == "gitee"
					if snippets {
						switch r.Method {
						case http.MethodPost:
							if r.URL.Path != "/gists" {
								t.Errorf("unexpected create path %s", r.URL.Path)
								w.WriteHeader(400)
								return
							}
							var body struct {
								Files map[string]struct {
									Content string `json:"content"`
								} `json:"files"`
							}
							json.NewDecoder(r.Body).Decode(&body)
							for name, file := range body.Files {
								if !strings.HasPrefix(name, ".motty-test-") {
									t.Errorf("unsafe file name %s", name)
								}
								objects[name] = file.Content
							}
							writes++
							w.WriteHeader(201)
						case http.MethodGet:
							if r.URL.Path != "/gists/abc123" {
								t.Errorf("read original gist %s", r.URL.Path)
								w.WriteHeader(400)
								return
							}
						case http.MethodDelete:
							if r.URL.Path != "/gists/abc123" {
								t.Errorf("unsafe delete %s", r.URL.Path)
								w.WriteHeader(400)
								return
							}
							deletes++
							if cleanupFails {
								w.WriteHeader(403)
								return
							}
							clear(objects)
							w.WriteHeader(204)
							return
						default:
							w.WriteHeader(405)
							return
						}
						files := map[string]any{}
						for name, value := range objects {
							files[name] = map[string]string{"content": value}
						}
						json.NewEncoder(w).Encode(map[string]any{"id": "abc123", "history": []map[string]string{{"version": "one"}}, "files": files})
						return
					}
					if !strings.Contains(r.URL.Path, "/.motty-test-") {
						t.Errorf("probe touched backup: %s", r.URL.Path)
						w.WriteHeader(400)
						return
					}
					switch r.Method {
					case http.MethodPut:
						if r.Header.Get("If-None-Match") != "*" {
							t.Error("probe was not create-only")
						}
						data, _ := io.ReadAll(r.Body)
						objects[r.URL.Path] = string(data)
						writes++
						w.Header().Set("ETag", `"test"`)
						w.WriteHeader(201)
					case http.MethodGet:
						w.Header().Set("ETag", `"test"`)
						io.WriteString(w, objects[r.URL.Path])
					case http.MethodDelete:
						deletes++
						if cleanupFails {
							w.WriteHeader(403)
							return
						}
						delete(objects, r.URL.Path)
						w.WriteHeader(204)
					default:
						w.WriteHeader(405)
					}
				}))
				defer server.Close()
				var store cloudsync.Store
				var err error
				switch provider {
				case "webdav":
					store, err = webdav.New(webdav.Config{URL: server.URL + "/dav", Path: "backup", User: "user", Password: "secret"}, nil)
				case "s3":
					store, err = s3.New(s3.Config{Endpoint: server.URL, Bucket: "bucket", Key: "backup", Region: "us-east-1", AccessKey: "key", Secret: "secret", PathStyle: true}, nil)
				default:
					kind := gist.GitHub
					if provider == "gitee" {
						kind = gist.Gitee
					}
					store, err = gist.New(gist.Config{Provider: kind, APIBase: server.URL, ID: "def456", Token: "token"}, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				err = cloudsync.TestReadWrite(context.Background(), store)
				if cleanupFails && (err == nil || !strings.Contains(err.Error(), "temporary test object")) {
					t.Fatalf("cleanup failure hidden: %v", err)
				}
				if !cleanupFails && err != nil {
					t.Fatal(err)
				}
				if writes != 1 || deletes != 1 || (!cleanupFails && len(objects) != 0) {
					t.Fatalf("writes=%d deletes=%d objects=%d", writes, deletes, len(objects))
				}
				if client, ok := store.(*gist.Client); ok && client.ID() != "def456" {
					t.Fatal("test replaced the configured snippet id")
				}
			})
		}
	}
}

type cancelProbeStore struct{ cancel context.CancelFunc }

func (store cancelProbeStore) Test(context.Context) error                          { return nil }
func (store cancelProbeStore) Put(context.Context, []byte, string) (string, error) { return "tag", nil }
func (store cancelProbeStore) Get(ctx context.Context) ([]byte, string, error) {
	store.cancel()
	return nil, "", ctx.Err()
}
func TestReadWriteProbeCleansUpAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleaned := false
	err := cloudsync.Probe(ctx, cancelProbeStore{cancel}, func(cleanup context.Context) error { cleaned = true; return cleanup.Err() })
	if err == nil || !cleaned {
		t.Fatalf("cancellation cleanup: err=%v cleaned=%v", err, cleaned)
	}
}

type failedProbePut struct{ err error }

func (store failedProbePut) Test(context.Context) error { return nil }
func (store failedProbePut) Put(context.Context, []byte, string) (string, error) {
	return "", store.err
}
func (store failedProbePut) Get(context.Context) ([]byte, string, error) {
	panic("read after failed upload")
}
func TestReadWriteProbeCanceledUploadStillCleansButConflictsDoNot(t *testing.T) {
	for _, failure := range []error{context.Canceled, cloudsync.ErrChanged} {
		cleaned := false
		err := cloudsync.Probe(context.Background(), failedProbePut{failure}, func(ctx context.Context) error { cleaned = true; return ctx.Err() })
		if err == nil || cleaned != (failure == context.Canceled) {
			t.Fatalf("failure=%v cleanup=%v error=%v", failure, cleaned, err)
		}
	}
}
