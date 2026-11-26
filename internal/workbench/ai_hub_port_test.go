//go:build windows || darwin || linux

package workbench

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"motty/internal/ai/store"
)

// A copy of the application that remembers no port must try the default every
// other copy shares before letting the system choose one: a client's saved
// address is only stable if the address is the same without the setting.
func TestAnUnrememberedPortPrefersTheSharedDefault(t *testing.T) {
	shared := []int{mcpDefaultPort, 0}
	for _, remembered := range []int{0, -1, 70000, mcpDefaultPort} {
		if got := mcpHTTPPorts(remembered); !slices.Equal(got, shared) {
			t.Fatalf("ports for remembered %d = %v, want %v", remembered, got, shared)
		}
	}
	// A remembered port comes first, and the default is still its first
	// fallback, so a port another program took does not send the address somewhere
	// random while the shared one is free.
	if got, want := mcpHTTPPorts(4455), []int{4455, mcpDefaultPort, 0}; !slices.Equal(got, want) {
		t.Fatalf("ports for remembered 4455 = %v, want %v", got, want)
	}
}

// The address the settings page shows is the one a client was configured with;
// it must survive the application quitting and starting again.
func TestTheMCPAddressSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.db")
	start := func() (*AIHub, string) {
		t.Helper()
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{"ai.mcp.enabled": "on", "ai.mcp.mode": "observe"} {
			if err := db.SetSetting(key, value); err != nil {
				t.Fatal(err)
			}
		}
		db.Close()
		hub := NewAIHub(path)
		if err := hub.Reconfigure(); err != nil {
			t.Fatal(err)
		}
		address := hub.Address()
		if address == "" {
			t.Fatal("the hub did not start")
		}
		return hub, address
	}
	first, address := start()
	first.Close()
	second, again := start()
	defer second.Close()
	if again != address {
		t.Fatalf("the address changed from %s to %s across a restart", address, again)
	}
	// The port that was taken is the one the settings carry, so the next start
	// asks for it instead of the default again.
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	remembered, _, err := db.Setting("ai.mcp.port")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strconv.Atoi(remembered); err != nil {
		t.Fatalf("remembered port %q is not a number", remembered)
	}
	if !strings.HasSuffix(address, ":"+remembered+"/mcp") {
		t.Fatalf("address %q does not carry the remembered port %q", address, remembered)
	}
}
