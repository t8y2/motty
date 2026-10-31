package sessionimport

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func write(t testing.TB, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func utf16le(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := []byte{0xff, 0xfe}
	for _, unit := range units {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return out
}

func TestPuTTYRegistryExportReadsEverySSHSession(t *testing.T) {
	reg := "Windows Registry Editor Version 5.00\r\n\r\n" +
		"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\Prod%20Web]\r\n" +
		"\"HostName\"=\"web.example.com\"\r\n\"PortNumber\"=dword:000008ae\r\n\"Protocol\"=\"ssh\"\r\n\"UserName\"=\"deploy\"\r\n" +
		"\"PublicKeyFile\"=\"C:\\\\keys\\\\prod.ppk\"\r\n\r\n" +
		"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\Serial]\r\n\"HostName\"=\"\"\r\n\"Protocol\"=\"serial\"\r\n\"SerialLine\"=\"COM7\"\r\n\"SerialSpeed\"=dword:0001c200\r\n\"SerialDataBits\"=dword:00000007\r\n\"SerialParity\"=dword:00000002\r\n\"SerialStopHalfbits\"=dword:00000004\r\n\"SerialFlowControl\"=dword:00000002\r\n\r\n" +
		"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\SshHostKeys]\r\n\"rsa2@22:x\"=\"0x1\"\r\n\r\n" +
		"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\Default%20Settings]\r\n\"HostName\"=\"\"\r\n\"Protocol\"=\"ssh\"\r\n\r\n" +
		"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\user@db.example.com]\r\n\"HostName\"=\"root@db.example.com\"\r\n\"Protocol\"=\"ssh\"\r\n"
	dir := t.TempDir()
	for name, data := range map[string][]byte{"utf16.reg": utf16le(reg), "utf8.reg": []byte(reg)} {
		result, err := ParsePuTTY(write(t, dir, name, data))
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Entries) != 3 {
			t.Fatalf("%s: entries = %+v warnings %v", name, result.Entries, result.Warnings)
		}
		prod := result.Entries[0]
		if prod.Name != "Prod Web" || prod.Host != "web.example.com" || prod.Port != 2222 || prod.User != "deploy" {
			t.Errorf("%s: prod = %+v", name, prod)
		}
		if console := result.Entries[1]; console.Protocol != Serial || console.Host != "COM7" || console.Line != "115200 7E2" || console.Flow != "rtscts" {
			t.Errorf("%s: serial = %+v", name, console)
		}
		if db := result.Entries[2]; db.Host != "db.example.com" || db.User != "root" {
			t.Errorf("%s: db = %+v: a user@host address is split", name, db)
		}
		if result.Skipped != 0 || result.Keys != 1 {
			t.Errorf("%s: skipped %d keys %d", name, result.Skipped, result.Keys)
		}
	}
}

func TestPuTTYUnixSessionFile(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "My%20Server", []byte("Present=1\nHostName=10.0.0.5\nPortNumber=22\nProtocol=ssh\nUserName=me\n"))
	result, err := ParsePuTTY(path)
	if err != nil || len(result.Entries) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if got := result.Entries[0]; got.Name != "My Server" || got.Host != "10.0.0.5" || got.Port != 22 || got.User != "me" {
		t.Fatalf("entry = %+v", got)
	}
	if _, err := ParsePuTTY(write(t, dir, "notes.txt", []byte("hello"))); err == nil {
		t.Fatal("an unrelated file was accepted")
	}
}

const xshellSession = "[CONNECTION]\r\nHost=192.168.1.20\r\nPort=2200\r\nProtocol=SSH\r\n[CONNECTION:AUTHENTICATION]\r\nUserName=admin\r\nPassword=ENCRYPTED\r\nUserKey=mykey\r\n"

func TestXshellSessionFilesAndArchives(t *testing.T) {
	dir := t.TempDir()
	result, err := ParseXshell(write(t, dir, "router.xsh", utf16le(xshellSession)))
	if err != nil || len(result.Entries) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if got := result.Entries[0]; got.Name != "router" || got.Host != "192.168.1.20" || got.Port != 2200 || got.User != "admin" || result.Keys != 1 {
		t.Fatalf("entry = %+v keys %d", got, result.Keys)
	}

	archive := filepath.Join(dir, "export.xts")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, body := range map[string]string{
		"Servers/Prod/web.xsh":  strings.Replace(xshellSession, "192.168.1.20", "web.example.com", 1),
		"Servers\\Lab\\tel.xsh": "[CONNECTION]\r\nHost=lab\r\nProtocol=TELNET\r\n",
		"readme.txt":            "ignore me",
	} {
		member, _ := writer.Create(name)
		member.Write(utf16le(body))
	}
	writer.Close()
	file.Close()
	result, err = ParseXshell(archive)
	if err != nil || len(result.Entries) != 2 || result.Skipped != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	byName := map[string]Entry{}
	for _, entry := range result.Entries {
		byName[entry.Name] = entry
	}
	if got := byName["web"]; got.Group != "Servers/Prod" || got.Host != "web.example.com" || got.Protocol != SSH {
		t.Fatalf("entry = %+v", got)
	}
	if got := byName["tel"]; got.Group != "Servers/Lab" || got.Host != "lab" || got.Protocol != Telnet {
		t.Fatalf("Telnet entry = %+v", got)
	}
	if _, err := ParseXshell(write(t, dir, "bad.xsh", []byte("nothing"))); err == nil {
		t.Fatal("a file with no [CONNECTION] was accepted")
	}
}

func TestMobaXtermSessionsKeepFoldersAndSkipOtherProtocols(t *testing.T) {
	text := "[Bookmarks]\r\nSubRep=\r\nImgNum=42\r\nLocal box=#109#0%127.0.0.1%22%me%%-1%-1%%%22%%0%0%0%%%-1%0%0%0%%1080%%0%0%1%%0%%%%%0%_Std_Colors_0_%80%24%0%1%-1%<None>%%0%0%-1%-1#0# #-1\r\n" +
		"[Bookmarks_1]\r\nSubRep=Clients\\Acme\r\nImgNum=41\r\nWeb=#109#0%web.acme.test%2222%root%%-1%-1%%\r\nWindows=#91#4%10.1.1.1%3389%Administrator%\r\nFiles=#98#7%ftp.acme.test%21%u%\r\n"
	dir := t.TempDir()
	result, err := ParseMobaXterm(write(t, dir, "s.mxtsessions", []byte(text)))
	if err != nil || len(result.Entries) != 2 || result.Skipped != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	if got := result.Entries[0]; got.Name != "Local box" || got.Host != "127.0.0.1" || got.Port != 22 || got.User != "me" || got.Group != "" {
		t.Fatalf("first = %+v", got)
	}
	if got := result.Entries[1]; got.Name != "Web" || got.Group != "Clients/Acme" || got.Host != "web.acme.test" || got.Port != 2222 || got.User != "root" {
		t.Fatalf("second = %+v", got)
	}
	if _, err := ParseMobaXterm(write(t, dir, "x.txt", []byte("[Other]\na=b"))); err == nil {
		t.Fatal("a file with no [Bookmarks] was accepted")
	}
}

func TestWindTermSessionsFileKeepsGroupsAndSkipsOtherProtocols(t *testing.T) {
	text := `[
	{"session.group": "Production", "session.icon": "linux", "session.label": "Web", "session.port": 2222, "session.protocol": "SSH", "session.target": "web.example.com", "session.uuid": "a"},
	{"session.label": "Box", "session.protocol": "SSH", "session.target": "root@10.0.0.9", "session.identityFile": "C:\\k"},
	{"session.label": "Tel", "session.protocol": "Telnet", "session.target": "10.0.0.10"},
	{"session.label": "Serial", "session.protocol": "Serial"}
	]`
	dir := t.TempDir()
	result, err := ParseWindTerm(write(t, dir, "user.sessions", []byte(text)))
	if err != nil || len(result.Entries) != 3 || result.Skipped != 1 || result.Keys != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if got := result.Entries[0]; got.Name != "Web" || got.Group != "Production" || got.Host != "web.example.com" || got.Port != 2222 {
		t.Fatalf("first = %+v", got)
	}
	if got := result.Entries[1]; got.Host != "10.0.0.9" || got.User != "root" {
		t.Fatalf("second = %+v", got)
	}
	if _, err := ParseWindTerm(write(t, dir, "x.json", []byte(`{"a":1}`))); err == nil {
		t.Fatal("an object was accepted as a session list")
	}
}

func TestElectermBookmarksKeepTheirFoldersAndSkipOtherKinds(t *testing.T) {
	const export = `{
	"bookmarkGroups": [
		{"id": "default", "title": "default", "bookmarkIds": ["free"], "bookmarkGroupIds": ["clients"]},
		{"id": "clients", "title": "Clients", "bookmarkIds": [], "bookmarkGroupIds": ["acme"]},
		{"id": "acme", "title": "Acme", "bookmarkIds": ["web"], "bookmarkGroupIds": ["prod"]},
		{"id": "prod", "title": "Prod", "bookmarkIds": ["db", "tel"], "bookmarkGroupIds": []},
		{"id": "ring-a", "title": "RingA", "bookmarkIds": ["box"], "bookmarkGroupIds": ["ring-b"]},
		{"id": "ring-b", "title": "RingB", "bookmarkIds": [], "bookmarkGroupIds": ["ring-a"]},
		{"id": "", "title": "nameless", "bookmarkIds": [], "bookmarkGroupIds": []}
	],
	"bookmarks": [
		{"id": "web", "title": "Web", "host": "web.acme.test", "port": "2222", "username": "root", "type": "ssh", "authType": "password", "password": "hunter2"},
		{"id": "db", "title": "", "host": "db.acme.test", "username": "deploy", "authType": "privateKey", "privateKey": "-----BEGIN KEY-----"},
		{"id": "tel", "title": "Switch", "host": "sw.lab", "port": 23, "type": "telnet", "username": "admin"},
		{"id": "box", "title": "Box", "host": "box.lab", "passphrase": "open sesame"},
		{"id": "free", "title": "Free", "host": "free.lab"},
		{"id": "bad", "title": "Bad", "host": "has space", "password": "never imported or counted"},
		{"id": "desk", "title": "Desktop", "host": "win.lab", "port": 3389, "type": "rdp"}
	]
}`
	dir := t.TempDir()
	result, err := Parse(Electerm, []string{write(t, dir, "bookmarks-2026-10-26.json", []byte(export))})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 5 || result.Skipped != 1 || result.Keys != 1 || result.PlainSecrets != 3 {
		t.Fatalf("%+v", result)
	}
	byName := map[string]Entry{}
	for _, entry := range result.Entries {
		byName[entry.Name] = entry
	}
	if got := byName["Web"]; got.Group != "Clients/Acme" || got.Host != "web.acme.test" || got.Port != 2222 || got.User != "root" {
		t.Fatalf("web = %+v", got)
	}
	// A bookmark with no title of its own is named after its address.
	if got := byName["db.acme.test"]; got.Group != "Clients/Acme/Prod" || got.User != "deploy" {
		t.Fatalf("db = %+v", got)
	}
	if got := byName["Switch"]; got.Protocol != Telnet || got.Port != 23 || got.User != "" || got.Group != "Clients/Acme/Prod" {
		t.Fatalf("telnet = %+v", got)
	}
	// Two groups that each sit in the other do not make a folder that runs on for
	// ever.
	if got := byName["Box"]; got.Group != "RingB/RingA" || got.Port != 0 || got.Protocol != SSH {
		t.Fatalf("box = %+v", got)
	}
	// Electerm's own "default" group holds what was never filed; it is not a folder.
	if got := byName["Free"]; got.Group != "" {
		t.Fatalf("free = %+v", got)
	}

	for _, notAnExport := range []string{`[{"id": "web"}]`, `{}`, `not json`} {
		if _, err := ParseElecterm(write(t, dir, "other.json", []byte(notAnExport))); err == nil {
			t.Fatalf("%s was accepted as an export", notAnExport)
		}
	}
	_, err = ParseElecterm(write(t, dir, "array.json", []byte(`[{"id": "web"}]`)))
	if err == nil || !strings.Contains(err.Error(), "JSON array") {
		t.Fatalf("an array export was refused with %v", err)
	}
}

func TestElectermGroupNestingIsBoundedAndKeepsTheInnermostFolders(t *testing.T) {
	const levels = 5000
	groups := make([]string, 0, levels)
	for level := range levels {
		members, children := `""`, `""`
		if level == levels-1 {
			members = `"deep"`
		} else {
			children = fmt.Sprintf(`"g%d"`, level+1)
		}
		groups = append(groups, fmt.Sprintf(`{"id": "g%d", "title": "G%d", "bookmarkIds": [%s], "bookmarkGroupIds": [%s]}`,
			level, level, members, children))
	}
	export := `{"bookmarkGroups": [` + strings.Join(groups, ",") + `], "bookmarks": [{"id": "deep", "title": "Deep", "host": "deep.lab"}]}`
	result, err := Parse(Electerm, []string{write(t, t.TempDir(), "deep.json", []byte(export))})
	if err != nil || len(result.Entries) != 1 {
		t.Fatalf("%v %+v", err, result)
	}
	folders := strings.Split(result.Entries[0].Group, "/")
	if len(folders) != maxGroupDepth || folders[len(folders)-1] != fmt.Sprintf("G%d", levels-1) {
		t.Fatalf("%d folders, last %q", len(folders), folders[len(folders)-1])
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "shortened") {
		t.Fatalf("warnings = %q", result.Warnings)
	}
}

func TestNetcattyHostsExportKeepsGroupsAndSkipsWhatItCannotConnect(t *testing.T) {
	const header = "Groups,Label,Tags,Notes,Hostname/IP,Protocol,Port,Username,Password,KeyPath,Passphrase,Proxy,ProxyJump,LocalShell,SshInputCompatibility\r\n"
	export := "\uFEFF" + header +
		"Project/Prod,\"Web, primary\",\"prod,web\",\"two\nlines\",web.example.com,ssh,2222,deploy,hunter2,,,,,,true\r\n" +
		"Project/Prod,DB,,,10.0.0.5,ssh,22,admin,,~/.ssh/id_ed25519,open sesame,,,,true\r\n" +
		"/Lab//Nested/,v6,,,[2001:db8::1],ssh,22,root,,,,,,,true\r\n" +
		",Switch,,,sw.lab,telnet,2323,admin,,,,,,,\r\n" +
		",Shell,,,localhost,local,22,me,,,,,,/bin/zsh,\r\n" +
		",'=cmd,,,box.lab,ssh,22,'-dash,,,,,,,true\r\n" +
		",Bad,,,has space,ssh,22,x,topsecret,,,,,,true\r\n" +
		",,,,,ssh,22,x,,,,,,,true\r\n"
	dir := t.TempDir()
	result, err := Parse(Netcatty, []string{write(t, dir, "hosts_export_2026-10-31.csv", []byte(export))})
	if err != nil {
		t.Fatal(err)
	}
	// The local shell is skipped; "has space" is rejected and its password is not
	// counted; the row with no address is silently ignored.
	if len(result.Entries) != 5 || result.Skipped != 1 || result.Keys != 1 || result.PlainSecrets != 2 {
		t.Fatalf("%+v", result)
	}
	byName := map[string]Entry{}
	for _, entry := range result.Entries {
		byName[entry.Name] = entry
	}
	if got := byName["Web, primary"]; got.Group != "Project/Prod" || got.Host != "web.example.com" || got.Port != 2222 || got.User != "deploy" {
		t.Fatalf("web = %+v", got)
	}
	if got := byName["DB"]; got.Group != "Project/Prod" || got.Host != "10.0.0.5" || got.User != "admin" {
		t.Fatalf("db = %+v", got)
	}
	if got := byName["v6"]; got.Group != "Lab/Nested" || got.Host != "2001:db8::1" {
		t.Fatalf("v6 = %+v", got)
	}
	if got := byName["Switch"]; got.Protocol != Telnet || got.Port != 2323 || got.User != "" || got.Group != "" {
		t.Fatalf("telnet = %+v", got)
	}
	// Netcatty puts a quote before a cell a spreadsheet would run as a formula.
	if got := byName["=cmd"]; got.User != "-dash" {
		t.Fatalf("guarded = %+v", got)
	}

	for _, notAnExport := range []string{"", "Name,Port\r\nweb,22\r\n"} {
		if _, err := ParseNetcatty(write(t, dir, "other.csv", []byte(notAnExport))); err == nil {
			t.Fatalf("%q was accepted as an export", notAnExport)
		}
	}
}

func TestAnEntryWithNoUsableAddressIsWarnedAbout(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "s.mxtsessions", []byte("[Bookmarks]\nBad=#109#0%has space%22%u%\nAlso=#109#0%%22%u%\n"))
	result, err := ParseMobaXterm(path)
	if err != nil || len(result.Entries) != 0 || len(result.Warnings) != 2 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestParseJoinsFilesAndReportsOnlyWhenNothingWasRead(t *testing.T) {
	dir := t.TempDir()
	good := write(t, dir, "a.xsh", []byte(xshellSession))
	bad := write(t, dir, "b.xsh", []byte("nothing"))
	result, err := Parse(Xshell, []string{good, bad})
	if err != nil || len(result.Entries) != 1 || len(result.Warnings) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := Parse(Xshell, []string{bad}); err == nil {
		t.Fatal("no entries and a failure must be an error")
	}
	if _, err := Parse(Xshell, nil); err == nil {
		t.Fatal("no files must be an error")
	}
	if _, err := Parse(PuTTY, []string{filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("a missing file must be an error")
	}
}

func TestNamesAreMadeSafeToShow(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "s.mxtsessions", []byte("[Bookmarks]\nA\x1b[2Jb=#109#0%h.example%22%u%\n"))
	result, _ := ParseMobaXterm(path)
	if len(result.Entries) != 1 || strings.ContainsAny(result.Entries[0].Name, "\x1b") {
		t.Fatalf("%+v", result.Entries)
	}
}

func TestBastionXshellSessionIgnoresPlaceholderKeysAndAcceptsSSH2(t *testing.T) {
	const bastion = "[CONNECTION]\r\nHost=127.0.0.1\r\nPort=63871\r\nProtocol=SSH\r\n" +
		"[CONNECTION:AUTHENTICATION]\r\nUserName=root\r\nPassword=ENCRYPTED\r\nUserKey=fake_rsa\r\n" +
		"[CONNECTION:SSH]\r\nCipherList=aes128-cbc,aes192-cbc,aes256-cbc\r\n"
	dir := t.TempDir()
	result, err := ParseXshell(write(t, dir, "tunnel.xsh", []byte(bastion)))
	if err != nil || len(result.Entries) != 1 || result.Keys != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	got := result.Entries[0]
	if got.Host != "127.0.0.1" || got.Port != 63871 || got.User != "root" || got.Protocol != SSH {
		t.Fatalf("entry = %+v", got)
	}
	if !IsLoopbackHost(got.Host) || !IsPlaceholderKey("fake_rsa") || IsPlaceholderKey("mykey") {
		t.Fatalf("loopback/placeholder helpers: host %v fake %v real %v", IsLoopbackHost(got.Host), IsPlaceholderKey("fake_rsa"), IsPlaceholderKey("mykey"))
	}

	ssh2 := strings.Replace(bastion, "Protocol=SSH", "Protocol=SSH2", 1)
	result, err = ParseXshell(write(t, dir, "ssh2.xsh", []byte(ssh2)))
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Protocol != SSH {
		t.Fatalf("SSH2 synonym: %+v %v", result, err)
	}
	for _, host := range []string{"127.0.0.1", "localhost", "::1", "LocalHost"} {
		if !IsLoopbackHost(host) {
			t.Fatalf("%q should be loopback", host)
		}
	}
	if IsLoopbackHost("example.com") || IsLoopbackHost("192.168.1.1") {
		t.Fatal("remote hosts must not look like loopback")
	}
}

func FuzzParsers(f *testing.F) {
	f.Add([]byte(xshellSession))
	f.Add([]byte("[Bookmarks]\nA=#109#0%h%22%u%\n"))
	f.Add([]byte("Windows Registry Editor Version 5.00\n\n[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\x]\n\"HostName\"=\"h\"\n"))
	f.Add(utf16le(xshellSession))
	f.Add([]byte(`[{"session.label":"a","session.target":"h","session.port":22,"session.protocol":"SSH"}]`))
	f.Add([]byte(`{"bookmarks":[{"id":"a","title":"t","host":"h","port":22,"type":"telnet"}],"bookmarkGroups":[{"id":"g","title":"G","bookmarkIds":["a"],"bookmarkGroupIds":["g"]}]}`))
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(dir, "fuzz.in")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, client := range []Client{PuTTY, Xshell, MobaXterm, WindTerm, Electerm} {
			result, _ := Parse(client, []string{path})
			for _, entry := range result.Entries {
				if entry.Host == "" || entry.Port < 0 || entry.Port > 65535 || strings.ContainsAny(entry.Name+entry.Host, "\x00\x1b\n") {
					t.Fatalf("unsafe entry %+v", entry)
				}
			}
		}
	})
}
