//go:build !js

package netclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/vivaui/viva/app/proxy"
)

// The transport must pass the request's context to the system proxy reader:
// PAC downloads are part of that request's budget, including cancellation.
func TestProxyLookupUsesRequestContext(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name, want := "canceled", context.Canceled
		ctx, cancel := context.WithCancel(context.Background())
		if deadline {
			cancel()
			name, want = "expired", context.DeadlineExceeded
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}
		cancel()
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/", nil)
			if err != nil {
				t.Fatal(err)
			}
			p, err := newTransport().Proxy(request)
			if p != nil || !errors.Is(err, want) {
				t.Fatalf("proxy lookup = %v, %v; want %v", p, err, want)
			}
		})
	}
}

// When the desktop proxy reports a valid proxy, the transport must use it.
func TestProxyLookupUsesConfiguredProxy(t *testing.T) {
	orig := systemProxy
	defer func() { systemProxy = orig }()

	wantProxy, err := url.Parse("http://127.0.0.1:8888")
	if err != nil {
		t.Fatal(err)
	}
	systemProxy = func(ctx context.Context, target *url.URL) (*url.URL, error) {
		return wantProxy, nil
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := newTransport().Proxy(request)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if p == nil || p.String() != wantProxy.String() {
		t.Fatalf("got proxy %v, want %v", p, wantProxy)
	}
}

// When the desktop proxy settings cannot be read, default transport must
// fail-closed to avoid silently bypassing the proxy policy.
func TestProxyLookupFailsClosedWhenSystemProxyUnavailable(t *testing.T) {
	orig := systemProxy
	defer func() { systemProxy = orig }()

	simulatedErr := fmt.Errorf("%w: simulated unreadable proxy settings", proxy.ErrUnavailable)
	systemProxy = func(ctx context.Context, target *url.URL) (*url.URL, error) {
		return nil, simulatedErr
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := newTransport().Proxy(request)
	if p != nil {
		t.Fatalf("expected nil proxy, got %v", p)
	}
	if !errors.Is(err, proxy.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if !IsProxyUnavailable(err) {
		t.Fatalf("IsProxyUnavailable(%v) = false, want true", err)
	}
}

// DirectClient connects directly without evaluating desktop proxy settings.
func TestDirectClientBypassesProxy(t *testing.T) {
	client := DirectClient(5 * time.Second)
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("expected nil Proxy on direct transport")
	}
}
