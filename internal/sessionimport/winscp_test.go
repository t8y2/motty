package sessionimport

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWinSCPClientParsesSites(t *testing.T) {
	data := bom + strings.Join([]string{
		`[Sessions\Default%20Settings]`, "FSProtocol=5",
		`[Sessions\Folder/%EF%BB%BF%E6%97%A5]`, // BOM belongs at the start of the whole key.
		`[Sessions\%EF%BB%BFFolder/%E6%97%A5%20%2B]`,
		"HostName=example.com", "UserName=ops%2Bteam", "PublicKeyFile=C:%5CKeys%5Cmy%20key.ppk", "Password=encoded",
		`[sessions\生]`, "hostname=scp.example", "FSProtocol=0", "PortNumber=2222",
		`[Sessions\SFTP]`, "HostName=login@secure.example", "FSProtocol=2",
		`[Sessions\FTP]`, "HostName=ftp.example", "FSProtocol=5",
		`[Sessions\Broken]`, "PortNumber=65536", "HostName=broken.example",
		`[Sessions\Ambiguous]`, "HostName=a.example", "HOSTNAME=b.example",
		`[Sessions\DUP]`, "HostName=a.example",
		`[Sessions\%64up]`, "HostName=b.example",
		`[Sessions\Workspace]`, "IsWorkspace=1", "HostName=a.example",
		`[Configuration\Interface]`, "anything=keep", "",
	}, "\r\n")
	path := filepath.Join(t.TempDir(), "WinSCP.ini")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := ParseWithOptions(WinSCP, []string{path}, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Name: "日 +", Group: "Folder", Host: "example.com", User: "ops+team", Port: 22, Source: "WinSCP.ini",
			SSHSettings: &SSHSettings{IdentityFiles: []string{`C:\Keys\my key.ppk`}}},
		{Name: "生", Host: "scp.example", Port: 2222, Source: "WinSCP.ini"},
		{Name: "SFTP", Host: "secure.example", User: "login", Port: 22, Source: "WinSCP.ini"},
	}
	if !reflect.DeepEqual(result.Entries, want) || result.Skipped != 7 {
		t.Fatalf("entries = (%+v, %d skipped), want (%+v, 7 skipped)", result.Entries, result.Skipped, want)
	}
}

func TestParseWinSCPRequiresExistingRegularFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := ParseWinSCP(filepath.Join(dir, "missing.ini")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load missing = %v, want os.ErrNotExist", err)
	}
	if _, err := ParseWinSCP(dir); !errors.Is(err, errWinSCPNotRegular) {
		t.Fatalf("Load directory = %v, want errWinSCPNotRegular", err)
	}
}

func TestWinSCPImportsRoutesAndSkipsUnsupportedProxies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WinSCP.ini")
	data := `[Sessions\Combined]
HostName=target.example
ProxyMethod=2
ProxyHost=proxy.example
ProxyUsername=proxy%2Buser
ProxyPasswordEnc=fake-credential
Tunnel=1
TunnelHostName=jump.example
TunnelUserName=jumpuser
TunnelPublicKeyFile=C:%5Ckeys%5Cjump.ppk
[Sessions\HTTP]
HostName=target.example
ProxyMethod=3
ProxyHost=2001:db8::1
ProxyPort=3128
[Sessions\Tunnel]
HostName=target.example
Tunnel=1
TunnelHostName=jump.example
TunnelPortNumber=2222
[Sessions\Unsupported]
HostName=target.example
ProxyMethod=1
[Sessions\Incomplete]
HostName=target.example
Tunnel=1
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ParseWinSCP(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Name: "Combined", Host: "target.example", Port: 22, Source: "WinSCP.ini", SSHSettings: &SSHSettings{
			Proxy: &OutboundProxy{Kind: ProxySOCKS5, Address: "proxy.example:80", User: "proxy+user"},
			Jump:  &SSHJump{Host: "jump.example", User: "jumpuser", Port: 22, KeyPath: `C:\keys\jump.ppk`},
		}},
		{Name: "HTTP", Host: "target.example", Port: 22, Source: "WinSCP.ini", SSHSettings: &SSHSettings{
			Proxy: &OutboundProxy{Kind: ProxyHTTPConnect, Address: "[2001:db8::1]:3128"},
		}},
		{Name: "Tunnel", Host: "target.example", Port: 22, Source: "WinSCP.ini", SSHSettings: &SSHSettings{
			Jump: &SSHJump{Host: "jump.example", Port: 2222},
		}},
	}
	if !reflect.DeepEqual(result.Entries, want) || result.Skipped != 2 {
		t.Fatalf("entries = (%+v, %d skipped), want (%+v, 2 skipped)", result.Entries, result.Skipped, want)
	}
}
