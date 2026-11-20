package sshbrowser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"motty/internal/portforward"
)

func TestFindBrowserHonorsTheChosenFamily(t *testing.T) {
	acceptAll := func(path string) (string, error) { return path, nil }
	for _, test := range []struct {
		family Family
		marker string
	}{
		{family: FamilyAuto, marker: "chrome"},
		{family: FamilyChrome, marker: "chrome"},
		{family: FamilyEdge, marker: "edge"},
		{family: FamilyChromium, marker: "chromium"},
	} {
		path, err := findBrowserWith(test.family, acceptAll)
		if err != nil {
			t.Fatalf("family %q: %v", test.family, err)
		}
		if !strings.Contains(strings.ToLower(path), test.marker) {
			t.Fatalf("family %q resolved %q, want a %s browser", test.family, path, test.marker)
		}
	}
}

func TestFindBrowserDoesNotFallBackToAnotherFamily(t *testing.T) {
	onlyChrome := func(path string) (string, error) {
		lower := strings.ToLower(path)
		if strings.Contains(lower, "chrome") && !strings.Contains(lower, "chromium") {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
	if _, err := findBrowserWith(FamilyEdge, onlyChrome); !errors.Is(err, ErrBrowserNotFound) {
		t.Fatalf("chosen Edge with only Chrome installed = %v, want ErrBrowserNotFound", err)
	}
}

func TestParseFamilyFallsBackToAutomatic(t *testing.T) {
	for value, want := range map[string]Family{
		"":         FamilyAuto,
		"chrome":   FamilyChrome,
		" edge ":   FamilyEdge,
		"chromium": FamilyChromium,
		"firefox":  FamilyAuto,
	} {
		if got := ParseFamily(value); got != want {
			t.Fatalf("ParseFamily(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestChromiumRoutesOAuthRedirectThroughSOCKS(t *testing.T) {
	if os.Getenv("MOTTY_TEST_SSH_BROWSER") != "1" {
		t.Skip("set MOTTY_TEST_SSH_BROWSER=1 to test with an installed Chromium browser")
	}
	path, err := findBrowser(FamilyAuto)
	if err != nil {
		t.Fatal(err)
	}
	var callbackAddress string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/authorize" {
			http.Redirect(w, r, "http://"+callbackAddress+"/callback?code=fixture", http.StatusFound)
			return
		}
		if r.URL.Path != "/callback" || r.URL.Query().Get("code") != "fixture" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "<html><body>remote-oauth-callback</body></html>")
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	callbackAddress = net.JoinHostPort("localhost", port)
	server.Start()
	destinations := make(chan string, 64)
	forward, err := portforward.Start(portforward.Config{
		ListenAddress: "127.0.0.1:0",
		Mode:          portforward.ModeSOCKS5,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			switch address {
			case "authorize.invalid:80", callbackAddress:
				destinations <- address
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			default:
				return nil, fmt.Errorf("unexpected destination %s", address)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := forward.Status().ListenAddress
	defer forward.Close()
	args := browserArgs(t.TempDir(), proxy)
	args = append(args[:len(args)-1], "--headless", "--disable-gpu", "--dump-dom", "http://authorize.invalid/authorize")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "<body>remote-oauth-callback</body>") {
		t.Fatalf("browser=%q err=%v output=%s", path, err, output)
	}
	forward.Close()
	close(destinations)
	var seen []string
	for destination := range destinations {
		seen = append(seen, destination)
	}
	if !slices.Contains(seen, "authorize.invalid:80") || !slices.Contains(seen, callbackAddress) {
		t.Fatalf("proxy destinations = %v", seen)
	}
	if client, err := net.DialTimeout("tcp", proxy, time.Second); err == nil {
		client.Close()
		t.Fatal("proxy still listening after close")
	}
	t.Logf("browser %s sent %v through the proxy", path, seen)
}
