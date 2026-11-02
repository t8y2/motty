package systemproxy

import (
	"reflect"
	"strings"
	"testing"
)

func TestEnvironmentSetsBothSpellingsAndClearsMissingProxies(t *testing.T) {
	settings := Settings{HTTP: "http://127.0.0.1:10808", HTTPS: "http://127.0.0.1:10809", Bypass: "localhost,.example.com"}
	want := []string{
		"HTTP_PROXY=http://127.0.0.1:10808", "http_proxy=http://127.0.0.1:10808",
		"HTTPS_PROXY=http://127.0.0.1:10809", "https_proxy=http://127.0.0.1:10809",
		"ALL_PROXY=", "all_proxy=",
		"NO_PROXY=localhost,.example.com", "no_proxy=localhost,.example.com",
	}
	if got := Environment(settings); !reflect.DeepEqual(got, want) {
		t.Fatalf("Environment() = %q, want %q", got, want)
	}
}

func TestWindowsSettingsReadsSharedAndPerProtocolProxies(t *testing.T) {
	shared := windowsSettings("127.0.0.1:10808", "localhost;*.example.com;127.*;172.16.*;192.168.*;10.1.2.*;256.*;192.168.1*;<local>;127.0.0.1")
	if shared.HTTP != "http://127.0.0.1:10808" || shared.HTTPS != shared.HTTP || shared.All != shared.HTTP {
		t.Fatalf("shared proxy = %+v", shared)
	}
	if shared.Bypass != "localhost,.example.com,127.0.0.0/8,172.16.0.0/16,192.168.0.0/16,10.1.2.0/24,127.0.0.1,::1" {
		t.Fatalf("bypass = %q", shared.Bypass)
	}
	separate := windowsSettings("http=proxy.local:8080;https=https://secure.local:8443;socks=127.0.0.1:1080", "")
	if separate.HTTP != "http://proxy.local:8080" || separate.HTTPS != "https://secure.local:8443" || separate.All != "socks5://127.0.0.1:1080" {
		t.Fatalf("per-protocol proxy = %+v", separate)
	}
}

func TestDarwinSettingsReadsEnabledProtocolsAndExceptions(t *testing.T) {
	settings := darwinSettings(`
<dictionary> {
  HTTPEnable : 1
  HTTPProxy : 127.0.0.1
  HTTPPort : 10808
  HTTPSEnable : 0
  HTTPSProxy : ignored
  HTTPSPort : 8443
  SOCKSEnable : 1
  SOCKSProxy : ::1
  SOCKSPort : 1080
  ExceptionsList : <array> {
    0 : localhost
    1 : *.example.com
  }
}`)
	if settings.HTTP != "http://127.0.0.1:10808" || settings.HTTPS != "" || settings.All != "socks5://[::1]:1080" || settings.Bypass != "localhost,.example.com" {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestGNOMESettingsReadsAManualProxy(t *testing.T) {
	output := `org.gnome.system.proxy autoconfig-url ''
org.gnome.system.proxy ignore-hosts ['localhost', '*.example.com']
org.gnome.system.proxy mode 'manual'
org.gnome.system.proxy use-same-proxy true
org.gnome.system.proxy.http host '127.0.0.1'
org.gnome.system.proxy.http port 10808
org.gnome.system.proxy.https host ''
org.gnome.system.proxy.https port 0
org.gnome.system.proxy.socks host '::1'
org.gnome.system.proxy.socks port 1080
`
	settings := gnomeSettings(output)
	want := Settings{HTTP: "http://127.0.0.1:10808", All: "socks5://[::1]:1080", Bypass: "localhost,.example.com"}
	if settings != want {
		t.Fatalf("settings = %+v, want %+v", settings, want)
	}
	if !settings.Enabled() {
		t.Fatal("a manual proxy is not enabled")
	}
	for _, mode := range []string{"'none'", "'auto'"} {
		if settings := gnomeSettings(strings.Replace(output, "'manual'", mode, 1)); settings.Enabled() || settings.Bypass != "" {
			t.Fatalf("mode %s gave %+v", mode, settings)
		}
	}
}

func TestGNOMEListReadsIgnoreHosts(t *testing.T) {
	got := gnomeList("['localhost', '127.0.0.0/8', '::1']")
	want := []string{"localhost", "127.0.0.0/8", "::1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gnomeList() = %q, want %q", got, want)
	}
}
