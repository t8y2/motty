package winscpini

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSites(t *testing.T) {
	data := bom + strings.Join([]string{
		`[Sessions\Default%20Settings]`, "FSProtocol=5",
		`[Sessions\Folder/%EF%BB%BF%E6%97%A5]`, // BOM belongs at the start of the whole key.
		`[Sessions\%EF%BB%BFFolder/%E6%97%A5%20%2B]`,
		"HostName=example.com", "UserName=ops%2Bteam", "PublicKeyFile=C:%5CKeys%5Cmy%20key.ppk",
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
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sites, skipped := c.Sites()
	wantSites := []Site{
		{Name: "Folder/日 +", Host: "example.com", User: "ops+team", Port: 22, KeyPath: `C:\Keys\my key.ppk`},
		{Name: "生", Host: "scp.example", Port: 2222},
		{Name: "SFTP", Host: "secure.example", User: "login", Port: 22},
	}
	if !reflect.DeepEqual(sites, wantSites) || skipped != 7 {
		t.Fatalf("Sites() = (%#v, %d), want (%#v, 7)", sites, skipped, wantSites)
	}
}

func TestLoadRequiresExistingRegularFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.ini")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load missing = %v, want os.ErrNotExist", err)
	}
	if _, err := Load(dir); !errors.Is(err, errNotRegular) {
		t.Fatalf("Load directory = %v, want errNotRegular", err)
	}
}
