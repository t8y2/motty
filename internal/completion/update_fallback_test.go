package completion

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpdateFallbackUsesMirrorAuthority(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/https://api.github.com/") {
			http.Error(w, "blocked", http.StatusForbidden)
			return
		}
		if r.Host != updateMirror {
			http.Error(w, "wrong authority", http.StatusForbidden)
			return
		}
		io.WriteString(w, `{"tag_name":"v1.9.0"}`)
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteTransport{target: server.URL}}
	var latest struct {
		Tag string `json:"tag_name"`
	}
	if err := getJSON(context.Background(), client, "https://api.github.com/repos/carapace-sh/carapace-bin/releases/latest", &latest); err != nil || latest.Tag != "v1.9.0" {
		t.Fatalf("mirrored release: tag=%q err=%v", latest.Tag, err)
	}
}

func TestUpdateFallbackRetriesStalledResponseBodies(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(fmt.Sprintf("archive=%t", archive), func(t *testing.T) {
			body := []byte(`{"tag_name":"v1.9.0"}`)
			if archive {
				body = releaseArchive(t)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Host != updateMirror {
					w.WriteHeader(http.StatusOK)
					w.Write(body[:len(body)/2])
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				w.Write(body)
			}))
			defer server.Close()
			client := &http.Client{Transport: rewriteTransport{target: server.URL}, Timeout: 100 * time.Millisecond}
			if archive {
				catalog, _, err := FetchCatalog(context.Background(), client, Release{Tag: "v1.9.0", Commit: "commit-sha"})
				if err != nil || catalog == nil || catalog.Root("tool") == nil {
					t.Fatalf("mirrored archive: catalog=%v err=%v", catalog, err)
				}
			} else {
				var latest struct {
					Tag string `json:"tag_name"`
				}
				if err := getJSON(context.Background(), client, "https://api.github.com/repos/carapace-sh/carapace-bin/releases/latest", &latest); err != nil || latest.Tag != "v1.9.0" {
					t.Fatalf("mirrored release: tag=%q err=%v", latest.Tag, err)
				}
			}
			if requests.Load() != 2 {
				t.Fatalf("expected one complete attempt per source, got %d", requests.Load())
			}
		})
	}
}

func TestUpdateFallbackDoesNotRetryInvalidArchives(t *testing.T) {
	// A complete but corrupt gzip stream is an integrity failure, not a
	// transport interruption. It must not be hidden by a different source.
	body := releaseArchive(t)
	body[len(body)-8] ^= 0xff
	requests := 0
	client := &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	if _, _, err := FetchCatalog(context.Background(), client, Release{Tag: "v1.9.0", Commit: "commit-sha"}); err == nil {
		t.Fatal("corrupt archive succeeded")
	}
	if requests != 1 {
		t.Fatalf("integrity failure retried %d requests", requests)
	}
}

func TestUpdateFallbackDoesNotRetryCanceledBodies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteTransport{target: server.URL}}
	if _, _, err := FetchCatalog(ctx, client, Release{Tag: "v1.9.0", Commit: "commit-sha"}); err == nil {
		t.Fatal("canceled download succeeded")
	}
	if requests.Load() != 1 {
		t.Fatalf("canceled download retried %d requests", requests.Load())
	}
}
