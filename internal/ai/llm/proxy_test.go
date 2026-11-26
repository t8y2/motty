package llm

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPProxyCarriesRequestsAndUsesItsPassword(t *testing.T) {
	const user, password = "alice", "s3cret"
	var seen atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
		if r.Header.Get("Proxy-Authorization") != want {
			http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
			return
		}
		if r.URL.Host != "llm.invalid" {
			t.Errorf("proxy request host = %q", r.URL.Host)
		}
		seen.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"m1"},{"id":"m2"}]}`)
	}))
	defer proxy.Close()

	provider := NewOpenAI(Config{
		BaseURL:  "http://llm.invalid/v1",
		APIKey:   "test",
		ProxyURL: strings.Replace(proxy.URL, "http://", "http://"+user+":"+password+"@", 1),
	})
	names, err := provider.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !seen.Load() || len(names) != 2 || names[0] != "m1" {
		t.Fatalf("names = %v, proxy seen = %t", names, seen.Load())
	}
}

func TestSOCKS5ProxyCarriesRequests(t *testing.T) {
	const user, password = "alice", "s3cret"
	address := newTestSOCKS5(t, user, password)
	provider := NewOpenAI(Config{
		BaseURL:  "http://llm.invalid/v1",
		APIKey:   "test",
		ProxyURL: "socks5://" + user + ":" + password + "@" + address,
	})
	names, err := provider.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "m1" {
		t.Fatalf("names = %v", names)
	}
}

func TestUnsupportedProxySchemeFailsInsteadOfConnectingDirectly(t *testing.T) {
	provider := NewOpenAI(Config{BaseURL: "http://llm.invalid/v1", ProxyURL: "ftp://127.0.0.1:21"})
	if _, err := provider.Models(context.Background()); err == nil || !strings.Contains(err.Error(), "proxy") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseProxyURL(t *testing.T) {
	tests := []struct {
		raw, address, password string
		ok                     bool
	}{
		{"", "", "", true},
		{"http://127.0.0.1:7890", "http://127.0.0.1:7890", "", true},
		{"HTTPS://proxy.local:8443/", "https://proxy.local:8443", "", true},
		{"socks5://alice:s3cret@127.0.0.1:1080", "socks5://alice@127.0.0.1:1080", "s3cret", true},
		{"socks5h://127.0.0.1:1080", "socks5h://127.0.0.1:1080", "", true},
		{"socks5://:secret@127.0.0.1:1080", "", "", false},
		{"ftp://127.0.0.1:21", "", "", false},
		{"127.0.0.1:7890", "", "", false},
		{"http://127.0.0.1:7890/path", "", "", false},
	}
	for _, test := range tests {
		address, password, ok := ParseProxyURL(test.raw)
		if address != test.address || password != test.password || ok != test.ok {
			t.Errorf("ParseProxyURL(%q) = %q, %q, %t; want %q, %q, %t",
				test.raw, address, password, ok, test.address, test.password, test.ok)
		}
	}
}

func newTestSOCKS5(t *testing.T, user, password string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		head := make([]byte, 2)
		if _, err := io.ReadFull(reader, head); err != nil || head[0] != 5 {
			return
		}
		methods := make([]byte, head[1])
		if _, err := io.ReadFull(reader, methods); err != nil {
			return
		}
		if _, err := conn.Write([]byte{5, 2}); err != nil {
			return
		}
		auth := make([]byte, 2)
		if _, err := io.ReadFull(reader, auth); err != nil || auth[0] != 1 {
			return
		}
		name := make([]byte, auth[1])
		if _, err := io.ReadFull(reader, name); err != nil {
			return
		}
		var passLen [1]byte
		if _, err := io.ReadFull(reader, passLen[:]); err != nil {
			return
		}
		pass := make([]byte, passLen[0])
		if _, err := io.ReadFull(reader, pass); err != nil {
			return
		}
		if string(name) != user || string(pass) != password {
			_, _ = conn.Write([]byte{1, 1})
			return
		}
		if _, err := conn.Write([]byte{1, 0}); err != nil {
			return
		}
		request := make([]byte, 4)
		if _, err := io.ReadFull(reader, request); err != nil || request[0] != 5 || request[1] != 1 {
			return
		}
		switch request[3] {
		case 1:
			_, _ = io.ReadFull(reader, make([]byte, 4))
		case 3:
			var length [1]byte
			_, _ = io.ReadFull(reader, length[:])
			_, _ = io.ReadFull(reader, make([]byte, int(length[0])))
		case 4:
			_, _ = io.ReadFull(reader, make([]byte, 16))
		default:
			return
		}
		_, _ = io.ReadFull(reader, make([]byte, 2))
		if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
			return
		}
		if _, err := http.ReadRequest(reader); err != nil {
			return
		}
		body := `{"data":[{"id":"m1"}]}`
		response := &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", ProtoMajor: 1, ProtoMinor: 1,
			Header:        http.Header{"Content-Type": {"application/json"}, "Connection": {"close"}},
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
		}
		_ = response.Write(conn)
	}()
	return listener.Addr().String()
}
