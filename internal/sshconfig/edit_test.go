package sshconfig

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func loadText(t *testing.T, content string) (*Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if content != "\x00missing" {
		writeTestConfig(t, path, content)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return config, path
}

func saved(t *testing.T, config *Config, path string) string {
	t.Helper()
	if err := config.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func str(value string) *string  { return &value }
func port(value uint16) *uint16 { return &value }

func TestDocumentRoundTripKeepsEveryByte(t *testing.T) {
	for _, input := range []string{
		"", "\n", "Host a\n", "Host a", "Host a\r\n  User x\r\n", "a\r\nb\nc\r\n", "\r\n\r\n", "# only\n\n\n", "x\r", "tab\tindent\n\tUser y\n",
	} {
		if got := string(newDoc("p", []byte(input), true, 0o600).bytes()); got != input {
			t.Errorf("round trip of %q = %q", input, got)
		}
	}
}

func FuzzDocumentRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "Host a\n  HostName b\n", "a\r\nb\nc", "\r\r\n\n\r"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if got := newDoc("p", data, true, 0o600).bytes(); !bytes.Equal(got, data) {
			t.Fatalf("round trip of %q = %q", data, got)
		}
	})
}

func TestApplyCreatesFenceBeforeGeneralBlocksAndKeepsTheRest(t *testing.T) {
	config, path := loadText(t, `# my hosts
Host mine
    HostName mine.example   # keep me
    User me

# defaults for everything
Host *
    ServerAliveInterval 30
`)
	if err := config.Apply(Edit{Alias: "web", HostName: str("10.0.0.5"), User: str("deploy"), Port: port(2200)}); err != nil {
		t.Fatal(err)
	}
	want := `# my hosts
Host mine
    HostName mine.example   # keep me
    User me

` + managedBegin + `
Host web
    HostName 10.0.0.5
    User deploy
    Port 2200
` + managedEnd + `

# defaults for everything
Host *
    ServerAliveInterval 30
`
	if got := saved(t, config, path); got != want {
		t.Fatalf("file =\n%s\nwant\n%s", got, want)
	}
	host, ok := config.Host("WEB")
	if !ok || host.HostName != "10.0.0.5" || host.User != "deploy" || host.Port != 2200 {
		t.Fatalf("evaluated host = %+v, %v", host, ok)
	}
}

func TestApplyAppendsToTheFenceAndRemoveTidiesUp(t *testing.T) {
	config, path := loadText(t, "Host mine\n    HostName mine.example\n")
	for _, alias := range []string{"one", "two"} {
		if err := config.Apply(Edit{Alias: alias, HostName: str(alias + ".example")}); err != nil {
			t.Fatal(err)
		}
	}
	got := saved(t, config, path)
	want := "Host mine\n    HostName mine.example\n\n" + managedBegin + "\nHost one\n    HostName one.example\n\nHost two\n    HostName two.example\n" + managedEnd + "\n"
	if got != want {
		t.Fatalf("file =\n%s\nwant\n%s", got, want)
	}
	for _, alias := range []string{"two", "one"} {
		removed, err := config.Remove(alias)
		if err != nil || !removed {
			t.Fatalf("Remove(%s) = %v, %v", alias, removed, err)
		}
	}
	if got := saved(t, config, path); got != "Host mine\n    HostName mine.example\n" {
		t.Fatalf("after removing everything, file = %q", got)
	}
}

func TestApplyEditsOnlyWhatTheUserWroteInPlace(t *testing.T) {
	config, path := loadText(t, `Host prod
  HostName old.example # was here
  User   root
  Port=22
  IdentityFile ~/.ssh/id_prod

Host other
    HostName other.example
`)
	err := config.Apply(Edit{Alias: "prod", HostName: str("new.example"), User: str(""), Port: port(2222), ProxyJump: str("bastion")})
	if err != nil {
		t.Fatal(err)
	}
	want := `Host prod
  HostName new.example # was here
  Port=2222
  IdentityFile ~/.ssh/id_prod
  ProxyJump bastion

Host other
    HostName other.example
`
	if got := saved(t, config, path); got != want {
		t.Fatalf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestSharedBlocksAreNeverEdited(t *testing.T) {
	config, path := loadText(t, "Host a b\n    HostName shared.example\n")
	if got := config.Ownership("a"); got != Shared {
		t.Fatalf("Ownership = %v", got)
	}
	if err := config.Apply(Edit{Alias: "a", User: str("x")}); !errors.Is(err, ErrShared) {
		t.Fatalf("Apply = %v, want ErrShared", err)
	}
	if err := config.Rename("a", "c"); !errors.Is(err, ErrShared) {
		t.Fatalf("Rename = %v, want ErrShared", err)
	}
	if config.Changed() {
		t.Fatal("a refused edit left changes behind")
	}
	if _, err := config.Remove("a"); err != nil {
		t.Fatalf("Remove of a name that has no block of its own = %v", err)
	}
	_ = path
}

func TestRemoveRefusesUserBlocks(t *testing.T) {
	config, _ := loadText(t, "Host mine\n    HostName mine.example\n")
	if removed, err := config.Remove("mine"); removed || !errors.Is(err, ErrNotManaged) {
		t.Fatalf("Remove = %v, %v", removed, err)
	}
	if config.Changed() {
		t.Fatal("a refused removal left changes behind")
	}
}

func TestRenameKeepsTheBlock(t *testing.T) {
	config, path := loadText(t, "Host old # note\n    HostName h.example\nHost taken\n")
	if err := config.Rename("old", "taken"); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename onto an existing host = %v", err)
	}
	if err := config.Rename("old", "new"); err != nil {
		t.Fatal(err)
	}
	if got := saved(t, config, path); got != "Host new # note\n    HostName h.example\nHost taken\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestSaveCreatesMissingFileAndDirectoryPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "config")
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Apply(Edit{Alias: "web", HostName: str("web.example")}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}
	want := managedBegin + "\nHost web\n    HostName web.example\n" + managedEnd + "\n"
	if data, _ := os.ReadFile(path); string(data) != want {
		t.Fatalf("file = %q", data)
	}
	if runtime.GOOS != "windows" {
		for target, mode := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
			if info, err := os.Stat(target); err != nil || info.Mode().Perm() != mode {
				t.Fatalf("%s mode = %v, %v; want %v", target, info.Mode().Perm(), err, mode)
			}
		}
	}
	if _, err := os.Stat(path + backupSuffix); err == nil {
		t.Fatal("a backup was made of a file that did not exist")
	}
}

func TestSaveKeepsModeBackupAndRefusesChangedFile(t *testing.T) {
	config, path := loadText(t, "Host a\n    HostName a.example\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Apply(Edit{Alias: "a", User: str("u")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Host a\n    HostName edited-meanwhile.example\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(); !errors.Is(err, ErrChanged) {
		t.Fatalf("Save over a changed file = %v, want ErrChanged", err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "edited-meanwhile") {
		t.Fatalf("the other change was lost: %q", data)
	}

	config, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Apply(Edit{Alias: "a", User: str("u")}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + backupSuffix)
	if err != nil || !strings.Contains(string(backup), "edited-meanwhile") {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
			t.Fatalf("mode = %v", info.Mode().Perm())
		}
	}
}

func TestSaveWritesThroughASymlinkAndKeepsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "dotfiles", "ssh_config")
	link := filepath.Join(directory, "config")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, target, "Host a\n    HostName a.example\n")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	config, err := Load(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Apply(Edit{Alias: "a", User: str("u")}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v, %v", info, err)
	}
	if data, _ := os.ReadFile(target); !strings.Contains(string(data), "User u") {
		t.Fatalf("target = %q", data)
	}
}

func TestEditsReachIncludedFilesAndKeepCRLF(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "config")
	included := filepath.Join(directory, "work.conf")
	writeTestConfig(t, root, "Include work.conf\r\nHost *\r\n    User fallback\r\n")
	writeTestConfig(t, included, "Host work\r\n    HostName work.example\r\n")
	config, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Ownership("work"); got != Own {
		t.Fatalf("Ownership = %v", got)
	}
	if err := config.Apply(Edit{Alias: "work", Port: port(2200)}); err != nil {
		t.Fatal(err)
	}
	if err := config.Apply(Edit{Alias: "new", HostName: str("new.example")}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(included); string(data) != "Host work\r\n    HostName work.example\r\n    Port 2200\r\n" {
		t.Fatalf("included file = %q", data)
	}
	data, _ := os.ReadFile(root)
	if strings.Contains(strings.ReplaceAll(string(data), "\r\n", ""), "\n") || !strings.Contains(string(data), managedBegin+"\r\nHost new\r\n") {
		t.Fatalf("root file = %q", data)
	}
}

func TestDamagedFenceRefusesToWrite(t *testing.T) {
	config, _ := loadText(t, managedBegin+"\nHost a\n    HostName a.example\n")
	if err := config.Apply(Edit{Alias: "b", HostName: str("b.example")}); !errors.Is(err, ErrMarkers) {
		t.Fatalf("Apply = %v, want ErrMarkers", err)
	}
}

func TestBlocksNextToTheFenceStayApart(t *testing.T) {
	config, path := loadText(t, "Host before\n    HostName before.example\n")
	for _, alias := range []string{"inside"} {
		if err := config.Apply(Edit{Alias: alias, HostName: str("in.example")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.Apply(Edit{Alias: "before", User: str("b")}); err != nil {
		t.Fatal(err)
	}
	got := saved(t, config, path)
	want := "Host before\n    HostName before.example\n    User b\n\n" + managedBegin + "\nHost inside\n    HostName in.example\n" + managedEnd + "\n"
	if got != want {
		t.Fatalf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestValuesAreQuotedWhenNeeded(t *testing.T) {
	config, path := loadText(t, "Host a\n    HostName a.example\n")
	if err := config.Apply(Edit{Alias: "a", User: str(`my user`)}); err != nil {
		t.Fatal(err)
	}
	if got := saved(t, config, path); !strings.Contains(got, `User "my user"`) {
		t.Fatalf("file = %q", got)
	}
	host, _ := config.Host("a")
	if host.User != "my user" {
		t.Fatalf("user read back = %q", host.User)
	}
	if err := config.Apply(Edit{Alias: "a", User: str("bad%u")}); err == nil {
		t.Fatal("a value with a token was written")
	}
}

func TestAliasForAndValidAlias(t *testing.T) {
	for label, want := range map[string]string{
		"web":          "web",
		"生产 DB":        "生产-DB",
		"  a  b  ":     "a-b",
		"a*b?c":        "a-b-c",
		"-lead":        "lead",
		"!!!":          "",
		"":             "",
		"user@host:22": "user@host:22",
	} {
		if got := AliasFor(label); got != want {
			t.Errorf("AliasFor(%q) = %q, want %q", label, got, want)
		}
		if want != "" && !ValidAlias(want) {
			t.Errorf("ValidAlias(%q) = false", want)
		}
	}
}

func TestFormatJumps(t *testing.T) {
	got := FormatJumps([]JumpHost{{Host: "bastion"}, {Host: "::1", User: "u", Port: 2200}, {Host: "h", Port: 22}})
	if got != "bastion,u@[::1]:2200,h:22" {
		t.Fatalf("FormatJumps = %q", got)
	}
	parsed, err := parseProxyJump(got)
	if err != nil || len(parsed) != 3 || parsed[1] != (JumpHost{Host: "::1", User: "u", Port: 2200}) {
		t.Fatalf("parse back = %+v, %v", parsed, err)
	}
}

// Adding a host must never change what the others in the file evaluate to, whatever
// the file looks like.
func FuzzApplyLeavesOtherHostsAlone(f *testing.F) {
	for _, seed := range []string{
		"", "Host a\n  HostName b\n", "Match all\n  User x\nHost *\n  Port 1\n", "Host a b\r\n  User u\r\n",
		managedBegin + "\nHost a\n" + managedEnd + "\n", "Host *\n", "# c\nHost x*\n  User y",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Skip()
		}
		config, err := Load(path)
		if err != nil {
			t.Skip()
		}
		before := config.Result().Hosts
		if err := config.Apply(Edit{Alias: "zz-new-host", HostName: str("zz.example"), User: str("zz")}); err != nil {
			return
		}
		after := config.Result().Hosts
		byAlias := map[string]Host{}
		for _, host := range after {
			byAlias[strings.ToLower(host.Alias)] = host
		}
		for _, host := range before {
			if got := byAlias[strings.ToLower(host.Alias)]; !reflect.DeepEqual(got, host) {
				t.Fatalf("host %q changed from %+v to %+v\nfile:\n%s", host.Alias, host, got, content)
			}
		}
		if _, ok := byAlias["zz-new-host"]; !ok {
			t.Fatalf("the new host is missing\nfile:\n%s", content)
		}
	})
}
