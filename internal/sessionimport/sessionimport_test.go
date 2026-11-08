package sessionimport

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
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

func TestXshellArchiveReadsLegacyChineseMemberNames(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "sessions.xts")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range []struct {
		name   string
		legacy bool
	}{
		{"Xshell/NAS存储/管理主机.xsh", true},
		{"Xshell/云服务器/生产.xsh", false},
	} {
		name := entry.name
		if entry.legacy {
			encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(name))
			if err != nil {
				t.Fatal(err)
			}
			name = string(encoded)
		}
		member, err := writer.CreateHeader(&zip.FileHeader{Name: name, NonUTF8: entry.legacy})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(utf16le(xshellSession)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ParseXshell(archive)
	if err != nil || len(result.Entries) != 2 {
		t.Fatalf("entries = %+v, err = %v", result.Entries, err)
	}
	groups := map[string]string{}
	for _, entry := range result.Entries {
		groups[entry.Name] = entry.Group
	}
	if groups["管理主机"] != "Xshell/NAS存储" || groups["生产"] != "Xshell/云服务器" {
		t.Fatalf("imported names and folders = %q", groups)
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

func TestMobaXtermChineseEncoding(t *testing.T) {
	text := "[Bookmarks]\r\nSubRep=客户\\生产\r\n中文主机=#109#0%web.example.test%22%root%\r\n"
	legacy, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"gb18030.mxtsessions": legacy,
		"utf8.mxtsessions":    []byte(text),
		"utf16.mxtsessions":   utf16le(text),
	} {
		path := write(t, t.TempDir(), name, contents)
		result, err := ParseMobaXterm(path)
		if name == "utf8.mxtsessions" && errors.Is(err, ErrAmbiguousMobaEncoding) {
			result, err = ParseMobaXtermWithEncoding(path, MobaUTF8)
		}
		if err != nil || len(result.Entries) != 1 {
			t.Fatalf("%s: entries=%d err=%v", name, len(result.Entries), err)
		}
		if got := result.Entries[0]; got.Name != "中文主机" || got.Group != "客户/生产" {
			t.Fatalf("%s: name=%q group=%q", name, got.Name, got.Group)
		}
	}
}

// The visible import path must read a BOM-less .xsh a Chinese Windows writes
// as GB18030: its host and user arrive as Chinese, not as dropped bytes
// (issue #332).
func TestParseXshellReadsBOMLessGB18030Sessions(t *testing.T) {
	text := "[CONNECTION]\r\nHost=生产主机.example\r\nPort=22\r\nProtocol=SSH\r\n[CONNECTION:AUTHENTICATION]\r\nUserName=管理员\r\n"
	legacy, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ParseXshell(write(t, t.TempDir(), "中文会话.xsh", legacy))
	if err != nil || len(result.Entries) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if got := result.Entries[0]; got.Name != "中文会话" || got.Host != "生产主机.example" || got.User != "管理员" {
		t.Fatalf("entry = %+v", got)
	}
}

func TestMobaXtermMalformedLegacyEncodingDoesNotDropBytes(t *testing.T) {
	path := write(t, t.TempDir(), "broken.mxtsessions", append([]byte("[Bookmarks]\n"), []byte{'x', 0x81, '=', '#', '1', '#', '0', '%', 'h', '%', '2', '2', '%', 'u', '%', '\n'}...))
	if _, err := ParseMobaXterm(path); err == nil {
		t.Fatal("invalid legacy bytes were accepted and silently dropped")
	}
}

func TestMobaXtermAmbiguousGB18030CanBeSelectedExplicitly(t *testing.T) {
	text := "[Bookmarks]\n隆=#109#0%host.example.test%22%root%\n"
	legacy, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil || !utf8.Valid(legacy) {
		t.Fatal("fixture must be ambiguous but valid UTF-8")
	}
	path := write(t, t.TempDir(), "ambiguous.mxtsessions", legacy)
	if _, err := ParseMobaXterm(path); !errors.Is(err, ErrAmbiguousMobaEncoding) {
		t.Fatalf("default import did not pause for ambiguous text: %v", err)
	}
	plain := write(t, t.TempDir(), "plain.mxtsessions", []byte("[Bookmarks]\nWeb=#109#0%web.example.test%22%root%\n"))
	if result, err := Parse(MobaXterm, []string{plain, path}); !errors.Is(err, ErrAmbiguousMobaEncoding) || len(result.Entries) != 0 {
		t.Fatalf("mixed import committed files before resolving encoding: entries=%d err=%v", len(result.Entries), err)
	}
	result, err := ParseWithOptions(MobaXterm, []string{path}, ParseOptions{MobaEncoding: MobaGB18030})
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Name != "隆" {
		t.Fatalf("explicit GB18030 did not recover the Chinese name: entries=%d err=%v", len(result.Entries), err)
	}
	result, err = ParseWithOptions(MobaXterm, []string{path}, ParseOptions{MobaEncoding: MobaUTF8})
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Name != "¡" {
		t.Fatalf("explicit UTF-8 was ignored: entries=%d err=%v", len(result.Entries), err)
	}
}

func TestMobaXtermRejectsDamagedUTF16(t *testing.T) {
	valid := utf16le("[Bookmarks]\nName=#109#0%host.example.test%22%root%\n")
	for name, damaged := range map[string][]byte{
		"odd byte":           append(append([]byte(nil), valid...), 0xaa),
		"unpaired surrogate": append(append([]byte(nil), valid...), 0x00, 0xd8),
		"isolated low":       append(append([]byte(nil), valid...), 0x00, 0xdc),
	} {
		if _, err := ParseMobaXterm(write(t, t.TempDir(), name+".mxtsessions", damaged)); err == nil {
			t.Fatalf("%s was silently accepted", name)
		}
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
	if byName["Web"].Password != "hunter2" || byName["db.acme.test"].Password != "" || byName["Switch"].Password != "" {
		t.Fatal("only password-authenticated SSH sessions may carry login passwords")
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
	if byName["Web, primary"].Password != "hunter2" || byName["DB"].Password != "" || byName["Switch"].Password != "" {
		t.Fatal("only SSH rows without a key may carry login passwords")
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

func TestPlaintextPasswordsStayWithTheirOwnEntriesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	first := write(t, dir, "first.csv", []byte("Hostname/IP,Protocol,Username,Password\nweb.example.test,ssh,root,first\n"))
	second := write(t, dir, "second.csv", []byte("Hostname/IP,Protocol,Username,Password\ndb.example.test,ssh,admin,'=second\n"))
	result, err := Parse(Netcatty, []string{first, second})
	if err != nil || len(result.Entries) != 2 {
		t.Fatalf("entries=%d err=%v", len(result.Entries), err)
	}
	if result.Entries[0].Password != "first" || result.Entries[1].Password != "=second" {
		t.Fatal("password candidates were lost or changed across files")
	}
	if strings.Contains(strings.Join(result.Warnings, "\n"), "first") || strings.Contains(strings.Join(result.Warnings, "\n"), "second") {
		t.Fatal("secret appeared in import warning")
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

// This test covers the encoding ladder of the shared reader: a BOM-less ANSI
// export from a Chinese Windows (as Xshell writes them)
// is GB18030 and must read as Chinese, not as dropped bytes; valid UTF-8 is
// never reinterpreted; bytes that survive no decoding still drop their bad
// parts (issue #332).
func TestDecodingANSIGB18030SessionsAndStillDroppingTheRest(t *testing.T) {
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("Host=主机.example\r\nUser=管理员\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(encoded); got != "Host=主机.example\r\nUser=管理员\r\n" {
		t.Fatalf("GB18030 text = %q", got)
	}
	if got := decode([]byte("Host=utf8 主机\r\n")); got != "Host=utf8 主机\r\n" {
		t.Fatalf("valid UTF-8 was reinterpreted: %q", got)
	}
	if got := decode(utf16le("Host=宽字符\r\n")); got != "Host=宽字符\r\n" {
		t.Fatalf("UTF-16 BOM text = %q", got)
	}
	// 0x80 is neither valid UTF-8 nor GB18030, so only the bad byte drops.
	if got := decode([]byte{'H', 0x80, 'o'}); got != "Ho" {
		t.Fatalf("undecodable bytes = %q, want \"Ho\"", got)
	}
}
