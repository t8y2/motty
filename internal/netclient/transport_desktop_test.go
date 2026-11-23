//go:build !js

package netclient

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
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
			proxy, err := newTransport().Proxy(request)
			if proxy != nil || !errors.Is(err, want) {
				t.Fatalf("proxy lookup = %v, %v; want %v", proxy, err, want)
			}
		})
	}
}
