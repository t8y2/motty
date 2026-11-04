package workbench

import (
	"reflect"
	"strings"
	"testing"
)

func kinds(found []detected) []actionKind {
	var out []actionKind
	for _, item := range found {
		out = append(out, item.kind)
	}
	return out
}

func TestTextUnderThePointerIsClassifiedByWhatItCouldBe(t *testing.T) {
	for _, test := range []struct {
		token string
		user  string
		want  []actionKind
		ssh   string // the connection offered, when one is
	}{
		{"10.0.0.5", "root", []actionKind{actionSSH, actionCopy}, "root@10.0.0.5"},
		{"10.0.0.5:8080", "", []actionKind{actionSSH, actionBrowser, actionCopy}, "10.0.0.5:8080"},
		{"db.example.com:5432", "app", []actionKind{actionSSH, actionBrowser, actionCopy}, "app@db.example.com:5432"},
		{"alice@build.example.com", "root", []actionKind{actionSSH, actionCopy}, "alice@build.example.com"},
		{"[2001:db8::1]:22", "", []actionKind{actionSSH, actionBrowser, actionCopy}, "[2001:db8::1]:22"},
		{"2001:db8::1", "", []actionKind{actionSSH, actionCopy}, "[2001:db8::1]"},
		{"/var/log/syslog", "", []actionKind{actionFiles, actionDownload, actionCopy}, ""},
		{"/etc/nginx/nginx.conf:42:7", "", []actionKind{actionFiles, actionDownload, actionCopy}, ""},
		{"~/backup/site.tar.gz", "", []actionKind{actionExtract, actionCopy}, ""},
		{"site.zip", "", []actionKind{actionExtract, actionCopy}, ""},
		{"./run.sh", "", []actionKind{actionCopy}, ""},
		// What only looks like an address or a path offers nothing.
		{"main.go:42", "", nil, ""},
		{"server.log", "", nil, ""},
		{"999.1.1.1", "", nil, ""},
		{"12:30:45", "", nil, ""},
		{"host.example.com:99999", "", nil, ""},
		{"https://example.com:8443/x", "", nil, ""},
		{"hello", "", nil, ""},
		{"/", "", nil, ""},
	} {
		found := classifyToken(test.token, test.user)
		if got := kinds(found); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%q offers %v, want %v", test.token, got, test.want)
			continue
		}
		if test.ssh != "" && found[0].target != test.ssh {
			t.Errorf("%q connects to %q, want %q", test.token, found[0].target, test.ssh)
		}
	}
}

func TestAnArchiveIsUnpackedIntoAFolderOfItsOwnAndNeverRun(t *testing.T) {
	for token, want := range map[string]string{
		"site.tar.gz":         "mkdir -- 'site' && tar -xf 'site.tar.gz' -C 'site'",
		"~/dl/it's.zip":       "unzip -q -- ~/'dl/it'\\''s.zip' -d 'it'\\''s'",
		"/srv/backups/db.tgz": "mkdir -- 'db' && tar -xf '/srv/backups/db.tgz' -C 'db'",
	} {
		found := classifyToken(token, "")
		if len(found) == 0 || found[0].kind != actionExtract || found[0].target != want {
			t.Errorf("%q gives %+v, want %q first", token, found, want)
		}
	}
}

func TestAMenuRowNamesWhatItActsOnAndStaysWithinTheMenu(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	for _, item := range []detected{
		{kind: actionExtract, label: "backup-2026-10-19-full.tar.gz"},
		{kind: actionBrowser, label: "internal-service.corp.example.com:8443"},
		{kind: actionFiles, label: "/var/log/nginx/error.log"},
		{kind: actionSSH, label: "10.0.0.5"},
	} {
		row := state.actionLabel(item)
		if n := len([]rune(row)); n > maxActionLabel {
			t.Errorf("%q is %d characters, more than the menu shows", row, n)
		}
		if !strings.Contains(row, "…") && !strings.Contains(row, item.label) {
			t.Errorf("%q lost the name it acts on", row)
		}
	}
	// The ends of a shortened name stay: the host's start and the file's extension.
	row := state.actionLabel(detected{kind: actionExtract, label: "backup-2026-10-19-full.tar.gz"})
	if !strings.HasSuffix(row, ".tar.gz") {
		t.Errorf("%q lost the archive's extension", row)
	}
}
