package gist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"motty/internal/cloudsync"
)

// The test binary stands in for gh: it answers `gh auth token --hostname H` the
// way the real one does, so the tests run the same way on every platform.
const fakeGHMode = "MOTTY_FAKE_GH_MODE"

func TestMain(m *testing.M) {
	switch mode := os.Getenv(fakeGHMode); {
	case mode == "":
		os.Exit(m.Run())
	case mode == "signed-out":
		fmt.Fprintln(os.Stderr, "no oauth token found for "+os.Args[len(os.Args)-1])
		os.Exit(1)
	case strings.HasPrefix(mode, "token:"):
		// The host is the last argument; the token is only given for the host it
		// was set for.
		want := strings.SplitN(mode, ":", 3)
		if len(os.Args) != 5 || os.Args[1] != "auth" || os.Args[2] != "token" || os.Args[3] != "--hostname" || os.Args[4] != want[1] {
			fmt.Fprintln(os.Stderr, "unexpected arguments", os.Args[1:])
			os.Exit(1)
		}
		fmt.Println(want[2])
	}
	os.Exit(0)
}

func fakeGH(t *testing.T, mode string) {
	t.Helper()
	t.Setenv(fakeGHMode, mode)
	previous := ghPath
	ghPath = func() (string, error) { return os.Args[0], nil }
	t.Cleanup(func() { ghPath = previous })
}

func TestTheGitHubCLILoginBacksUpWithoutAStoredToken(t *testing.T) {
	server, fake := newFakeGitHub(t)
	fakeGH(t, "token:127.0.0.1:"+testToken)
	client, err := New(Config{GitHubCLI: true, APIBase: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.Test(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := client.Put(ctx, []byte("sealed"), "")
	if err != nil || client.ID() == "" {
		t.Fatalf("create: %q %q %v", first, client.ID(), err)
	}
	second, err := client.Put(ctx, []byte("newer"), first)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	data, tag, err := client.Get(ctx)
	if err != nil || string(data) != "newer" || tag != second {
		t.Fatalf("get: %q %q %v", data, tag, err)
	}
	if len(fake.requests) == 0 {
		t.Fatal("nothing reached the server")
	}
}

// A login the user switches or refreshes in gh applies to the next sync without
// restarting Motty, which only holds if the token is asked for again each time.
func TestTheGitHubCLITokenIsReadAgainForEveryOperation(t *testing.T) {
	server, _ := newFakeGitHub(t)
	client, err := New(Config{GitHubCLI: true, APIBase: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fakeGH(t, "token:127.0.0.1:"+testToken)
	if err := client.Test(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeGHMode, "token:127.0.0.1:github_pat_revoked")
	if err := client.Test(ctx); !errors.Is(err, cloudsync.ErrUnauthorized) {
		t.Fatalf("a changed login was not picked up: %v", err)
	}
}

func TestTheGitHubCLISaysWhatIsMissing(t *testing.T) {
	server, _ := newFakeGitHub(t)
	client, err := New(Config{GitHubCLI: true, APIBase: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fakeGH(t, "signed-out")
	if err := client.Test(ctx); !errors.Is(err, ErrCLINotSignedIn) {
		t.Fatalf("signed out: %v", err)
	}
	if _, err := client.Put(ctx, []byte("x"), ""); !errors.Is(err, ErrCLINotSignedIn) {
		t.Fatalf("put signed out: %v", err)
	}
	ghPath = func() (string, error) { return "", ErrNoCLI }
	if err := client.Test(ctx); !errors.Is(err, ErrNoCLI) {
		t.Fatalf("no gh: %v", err)
	}
}

func TestTheGitHubCLIAndATokenAreNotMixed(t *testing.T) {
	if _, err := New(Config{GitHubCLI: true, Token: testToken}, nil); err == nil {
		t.Fatal("a token next to the CLI login was accepted")
	}
	if _, err := New(Config{Provider: Gitee, GitHubCLI: true}, nil); err == nil {
		t.Fatal("Gitee has no CLI login")
	}
}

func TestTheCLIHostFollowsTheAPIAddress(t *testing.T) {
	for api, want := range map[string]string{
		DefaultAPIBase:                        "github.com",
		"https://ghe.example.com/api/v3":      "ghe.example.com",
		"https://ghe.example.com:8443/api/v3": "ghe.example.com",
	} {
		if got := CLIHost(api); got != want {
			t.Errorf("CLIHost(%q) = %q, want %q", api, got, want)
		}
	}
}
