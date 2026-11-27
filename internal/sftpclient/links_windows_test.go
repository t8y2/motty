//go:build windows

package sftpclient

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type concurrentLinkRemote struct {
	remote
	hold time.Duration

	mu   sync.Mutex
	open int
	max  int
	sent map[string]string
}

func (r *concurrentLinkRemote) Symlink(target, link string) error {
	r.mu.Lock()
	r.open++
	if r.open > r.max {
		r.max = r.open
	}
	r.sent[link] = target
	r.mu.Unlock()
	time.Sleep(r.hold)
	r.mu.Lock()
	r.open--
	r.mu.Unlock()
	// Windows test servers may lack SeCreateSymbolicLinkPrivilege when creating
	// the remote link on disk; create a placeholder directory at link's target
	// is unnecessary because we are testing the SFTP client's link upload plan
	// and concurrency.
	return nil
}

func makeWindowsJunction(t *testing.T, linkPath, targetPath string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", linkPath, targetPath).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s -> %s failed: %v (%s)", linkPath, targetPath, err, out)
	}
}

func TestWindowsNuxtOutputJunctionsUploadConcurrentlyAsRelativePOSIXLinks(t *testing.T) {
	recorder := &concurrentLinkRemote{
		hold: 10 * time.Millisecond,
		sent: make(map[string]string),
	}
	worker, root := newProtocolWorkerOn(t, nil, func(client remote) Remote {
		recorder.remote = client
		return recorder
	})

	outputDir := filepath.Join(t.TempDir(), ".output")
	if err := os.MkdirAll(filepath.Join(outputDir, "public", "_nuxt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "server-entry.mjs"), []byte("export default {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const packages = 16
	for i := range packages {
		pkgStore := filepath.Join(outputDir, "server", "node_modules", ".pnpm", fmt.Sprintf("pkg-%02d@1.0.0", i), "node_modules", fmt.Sprintf("pkg-%02d", i))
		if err := os.MkdirAll(pkgStore, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgStore, "package.json"), []byte(fmt.Sprintf(`{"name":"pkg-%02d"}`, i)), 0o644); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(outputDir, "server", "node_modules", fmt.Sprintf("pkg-%02d", i))
		makeWindowsJunction(t, linkPath, pkgStore)
	}

	// Directly selecting a junction itself is still rejected, matching symlinks.
	directJunction := filepath.Join(outputDir, "server", "node_modules", "pkg-00")
	id, queued := worker.QueueUpload(directJunction, "direct", true, ExistingFail)
	if res := queueResult(t, id, queued, worker); !errors.Is(res.Err, ErrSymbolicLink) {
		t.Fatalf("selected junction upload = %+v, want ErrSymbolicLink", res)
	}

	// Uploading the .output directory transfers junctions as relative POSIX links
	// in parallel, without duplicating the target files or failing on ModeIrregular.
	id, queued = worker.QueueUpload(outputDir, ".output", true, ExistingFail)
	res := queueResult(t, id, queued, worker)
	if res.Err != nil || res.Files != packages+1 {
		t.Fatalf("upload .output result = %+v, want %d files", res, packages+1)
	}
	if recorder.max < 2 {
		t.Fatalf("concurrent links in flight = %d, want >= 2", recorder.max)
	}
	for i := range packages {
		key := fmt.Sprintf(".output/server/node_modules/pkg-%02d", i)
		want := fmt.Sprintf(".pnpm/pkg-%02d@1.0.0/node_modules/pkg-%02d", i, i)
		if got := recorder.sent[key]; got != want {
			t.Fatalf("sent link %s = %q, want %q", key, got, want)
		}
		pkgJSON := filepath.Join(root, ".output", "server", "node_modules", ".pnpm", fmt.Sprintf("pkg-%02d@1.0.0", i), "node_modules", fmt.Sprintf("pkg-%02d", i), "package.json")
		if _, err := os.Stat(pkgJSON); err != nil {
			t.Fatalf("missing uploaded package.json %s: %v", pkgJSON, err)
		}
	}
}

func TestWindowsOutOfRootJunctionIsRejected(t *testing.T) {
	recorder := &concurrentLinkRemote{
		sent: make(map[string]string),
	}
	worker, _ := newProtocolWorkerOn(t, nil, func(client remote) Remote {
		recorder.remote = client
		return recorder
	})

	base := t.TempDir()
	outsideTarget := filepath.Join(base, "external-store", "vue")
	if err := os.MkdirAll(outsideTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(base, ".output")
	nodeModules := filepath.Join(outputDir, "server", "node_modules")
	if err := os.MkdirAll(nodeModules, 0o755); err != nil {
		t.Fatal(err)
	}
	makeWindowsJunction(t, filepath.Join(nodeModules, "vue"), outsideTarget)

	id, queued := worker.QueueUpload(outputDir, ".output", true, ExistingFail)
	res := queueResult(t, id, queued, worker)
	if !errors.Is(res.Err, ErrSymbolicLink) {
		t.Fatalf("out-of-root junction upload = %+v, want ErrSymbolicLink", res)
	}
	if len(recorder.sent) != 0 {
		t.Fatalf("out-of-root junction unexpectedly created remote links: %+v", recorder.sent)
	}
}
