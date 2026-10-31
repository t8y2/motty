package systemproxy

import (
	"reflect"
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
	shared := windowsSettings("127.0.0.1:10808", "localhost;*.example.com;<local>;127.0.0.1")
	if shared.HTTP != "http://127.0.0.1:10808" || shared.HTTPS != shared.HTTP || shared.All != shared.HTTP {
		t.Fatalf("shared proxy = %+v", shared)
	}
	if shared.Bypass != "localhost,.example.com,127.0.0.1,::1" {
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

func TestGNOMEListReadsIgnoreHosts(t *testing.T) {
	got := gnomeList("['localhost', '127.0.0.0/8', '::1']")
	want := []string{"localhost", "127.0.0.0/8", "::1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gnomeList() = %q, want %q", got, want)
	}
}
