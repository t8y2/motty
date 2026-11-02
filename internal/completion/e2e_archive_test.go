package completion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestFetchCatalogRealCarapaceArchive runs the production download, archive and
// import path over a real Carapace release tarball. The tarball is 27 MB and is
// not committed, so the test runs only when MOTTY_COMPLETION_ARCHIVE points at
// one; the pinned v1.8.2 archive is the reference input:
//
//	MOTTY_COMPLETION_ARCHIVE=/path/carapace-ff108d6.tar.gz \
//	    go test ./internal/completion -run CarapaceArchive -v
//
// v1.8.2 ships nine files with the known unquoted plain-scalar defect, so a
// successful import repairs nine of them, skips none and still covers 742
// roots. MOTTY_COMPLETION_RELEASE and MOTTY_COMPLETION_COMMIT override the
// default release identity for a different archive.
func TestFetchCatalogRealCarapaceArchive(t *testing.T) {
	path := os.Getenv("MOTTY_COMPLETION_ARCHIVE")
	if path == "" {
		t.Skip("MOTTY_COMPLETION_ARCHIVE is not set; set it to a carapace-bin release tarball")
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	release := Release{
		Tag:    "v1.8.2",
		Commit: "ff108d6122f0cb441622fc412770e84a65078bdb",
	}
	if tag := os.Getenv("MOTTY_COMPLETION_RELEASE"); tag != "" {
		release.Tag = tag
	}
	if commit := os.Getenv("MOTTY_COMPLETION_COMMIT"); commit != "" {
		release.Commit = commit
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/tar.gz/"+release.Commit) {
			http.NotFound(writer, request)
			return
		}
		writer.Write(archive)
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteTransport{target: server.URL}}
	catalog, stats, err := FetchCatalog(context.Background(), client, release)
	if err != nil {
		t.Fatalf("importing %s: %v", path, err)
	}
	t.Logf("%s: release=%s roots=%d nodes=%d flags=%d values=%d repaired=%d skipped-sources=%d",
		path, catalog.Release, stats.Roots, stats.Nodes, stats.Flags, stats.Values,
		stats.RepairedFiles, stats.SkippedSources)
	if stats.SkippedSources != 0 {
		t.Fatalf("the reference archive lost %d sources", stats.SkippedSources)
	}
	if stats.RepairedFiles == 0 {
		t.Fatal("the reference archive contains the upstream defect but nothing was repaired")
	}
	if catalog.RootCount() < 700 {
		t.Fatalf("only %d roots were imported", catalog.RootCount())
	}
	if mpv := catalog.Root("mpv"); mpv != nil {
		if _, ok := catalog.FindFlag(mpv, "--osd-align-y"); !ok {
			t.Fatal("mpv was imported but its repaired flag is missing")
		}
	}
}
