package workbench

import (
	"errors"
	"strings"
	"testing"
)

func TestParseTargetReadsTheWaysPeopleWriteAHost(t *testing.T) {
	for input, want := range map[string]target{
		"example.com":                  {"", "example.com", 22},
		"  example.com  ":              {"", "example.com", 22},
		"deploy@example.com":           {"deploy", "example.com", 22},
		"deploy@example.com:2222":      {"deploy", "example.com", 2222},
		"example.com:2222":             {"", "example.com", 2222},
		"192.168.1.1":                  {"", "192.168.1.1", 22},
		"tester@127.0.0.1:2222":        {"tester", "127.0.0.1", 2222},
		"ssh deploy@example.com":       {"deploy", "example.com", 22},
		"ssh -p 2200 deploy@host":      {"deploy", "host", 2200},
		"ssh -p2200 host":              {"", "host", 2200},
		"ssh host -p 2200":             {"", "host", 2200},
		"deploy@host:22 ssh":           {},
		"user@[::1]:2222":              {"user", "::1", 2222},
		"user@[::1]":                   {"user", "::1", 22},
		"::1":                          {"", "::1", 22},
		"2001:db8::1":                  {"", "2001:db8::1", 22},
		"my_host-01.local":             {"", "my_host-01.local", 22},
		"first.last@example.com":       {"first.last", "example.com", 22},
		"a@b@example.com":              {"a@b", "example.com", 22},
		"ssh -p 2200 deploy@host:2300": {"deploy", "host", 2200}, // an explicit -p wins
		"ssh -l deploy host":           {"deploy", "host", 22},
		"ssh -ldeploy host":            {"deploy", "host", 22},
		"ssh -l other deploy@host":     {"deploy", "host", 22}, // user@host wins over -l
		"ssh://deploy@host:2222":       {"deploy", "host", 2222},
		"ssh://deploy@host:2222/":      {"deploy", "host", 2222},
		"SSH://host/":                  {"", "host", 22},
		"ssh://[::1]:2200":             {"", "::1", 2200},
		"fe80::1%en0":                  {"", "fe80::1%en0", 22},
		"deploy@[fe80::1%en0]:2222":    {"deploy", "fe80::1%en0", 2222},
	} {
		got, _, err := parseTarget(input)
		if input == "deploy@host:22 ssh" { // two destinations
			if err == nil {
				t.Errorf("%q: two destinations were accepted: %+v", input, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("parseTarget(%q) = %+v, %v; want %+v", input, got, err, want)
		}
	}
}

func TestParseTargetRejectsWhatItWouldHaveToGuess(t *testing.T) {
	for _, input := range []string{
		"", "   ", "ssh", "@host", "user@", "user@:22", "host:0", "host:65536", "host:abc", "host:-1",
		"-p", "ssh -p", "ssh -p 99999 host", "ssh -o StrictHostKeyChecking=no host", "ssh -i key host", "-pw", "-pw secret",
		"two words", "host name", "-leading.dash", "bad/host", "bad host:22", "user@host:22:33x",
		"[::1", "[::1]x", "[]:22", "fe80::1%", "fe80::1%en0:22",
		"example.com%zone", "ssh://host/path", "ssh://host?x=1", "ssh://", "ssh:// host", "ssh://host#frag", "ssh -l", "ssh -l user",
	} {
		if got, _, err := parseTarget(input); err == nil {
			t.Errorf("parseTarget(%q) = %+v, want an error", input, got)
		}
	}
}

func TestParseConnectionReadsTelnetToo(t *testing.T) {
	for input, want := range map[string]connection{
		"telnet host":             {target: target{"", "host", 23}, protocol: protocolTelnet},
		"telnet host 2323":        {target: target{"", "host", 2323}, protocol: protocolTelnet},
		"telnet://host/":          {target: target{"", "host", 23}, protocol: protocolTelnet},
		"telnet://host:2323":      {target: target{"", "host", 2323}, protocol: protocolTelnet},
		"TELNET://[::1]:9":        {target: target{"", "::1", 9}, protocol: protocolTelnet},
		"ssh deploy@host":         {target: target{"deploy", "host", 22}, protocol: protocolSSH},
		"ssh://deploy@host:2200/": {target: target{"deploy", "host", 2200}, protocol: protocolSSH},
	} {
		got, err := parseConnection(input)
		if err != nil || got != want {
			t.Errorf("parseConnection(%q) = %+v, %v; want %+v", input, got, err, want)
		}
	}
	for _, input := range []string{"telnet", "telnet host 0", "telnet host x", "telnet host 1 2", "telnet://", "telnet://bad host", "telnet://host/path"} {
		if got, err := parseConnection(input); err == nil {
			t.Errorf("parseConnection(%q) = %+v, want an error", input, got)
		}
	}
}

func TestParseTargetReadsAPasswordOnlyFromAURL(t *testing.T) {
	for _, test := range []struct {
		input    string
		want     target
		password string
	}{
		{"ssh://bob:s%40cret@host:2222/", target{"bob", "host", 2222}, "s@cret"},
		{"sftp://bob:pa%20ss@host/", target{"bob", "host", 22}, "pa ss"},
		{"SSH://bob:pw@[::1]:2200", target{"bob", "::1", 2200}, "pw"},
		{"ssh://bob@host", target{"bob", "host", 22}, ""},
		{"ssh://bob:@host", target{"bob", "host", 22}, ""},
		{"-p 2222 -l bob -pw secret host", target{"bob", "host", 2222}, "secret"},
		// Typed forms keep their old meaning: the colon is part of the login.
		{"bob:pw@host", target{"bob:pw", "host", 22}, ""},
	} {
		got, password, err := parseTarget(test.input)
		if err != nil || got != test.want || password != test.password {
			t.Errorf("parseTarget(%q) = %+v, %q, %v; want %+v, %q", test.input, got, password, err, test.want, test.password)
		}
	}
	for _, input := range []string{"ssh://:pw@host", "ssh://bob:%zz@host", "ssh://bob:" + strings.Repeat("x", 5000) + "@host"} {
		if got, _, err := parseTarget(input); err == nil {
			t.Errorf("parseTarget(%q) = %+v, want an error", input, got)
		}
	}
}

func TestParseConnectionReadsBastionClientLinks(t *testing.T) {
	for input, want := range map[string]connection{
		"xshell://ssh://bob:pw@host:2222":  {target{"bob", "host", 2222}, protocolSSH, "pw", false},
		"xshell://ssh//bob:pw@host:2222":   {target{"bob", "host", 2222}, protocolSSH, "pw", false},
		"xshell://ssh:bob:pw@host:2222":    {target{"bob", "host", 2222}, protocolSSH, "pw", false},
		"xshell://bob:pw@host:2222":        {target{"bob", "host", 2222}, protocolSSH, "pw", false},
		"XSHELL://ssh://bob:pw@host:2222/": {target{"bob", "host", 2222}, protocolSSH, "pw", false},
		"xshell://bob@host":                {target{"bob", "host", 22}, protocolSSH, "", false},
		"xshell://sftp://bob:pw@host:2222": {target{"bob", "host", 2222}, protocolSSH, "pw", true},
		"sftp://bob:pw@host:2222":          {target{"bob", "host", 2222}, protocolSSH, "pw", true},
		"xftp://sftp://bob:pw@host:2222":   {target{"bob", "host", 2222}, protocolSSH, "pw", true},
		"xftp://sftp:bob:pw@host:2222":     {target{"bob", "host", 2222}, protocolSSH, "pw", true},
		"xftp://bob:pw@host:2222":          {target{"bob", "host", 2222}, protocolSSH, "pw", true},
		"xshell://host.example.com:2222":   {target{"", "host.example.com", 2222}, protocolSSH, "", false},
		"xshell://ssh.example.com":         {target{"", "ssh.example.com", 22}, protocolSSH, "", false},
	} {
		got, err := parseConnection(input)
		if err != nil || got != want {
			t.Errorf("parseConnection(%q) = %+v, %v; want %+v", input, got, err, want)
		}
	}
	for _, input := range []string{"xftp://ftp://host", "xshell://ftp://host", "xftp://telnet://host"} {
		if got, err := parseConnection(input); !errors.Is(err, errUnsupportedLink) {
			t.Errorf("parseConnection(%q) = %+v, %v; want errUnsupportedLink", input, got, err)
		}
	}
}

func TestRedactLaunchTextHidesOnlyThePassword(t *testing.T) {
	for input, want := range map[string]string{
		"ssh://bob:secret@host:22":          "ssh://bob:***@host:22",
		"xshell://ssh://bob:secret@host:22": "xshell://ssh://bob:***@host:22",
		"xftp://bob:secret@host:22":         "xftp://bob:***@host:22",
		"ssh://bob@host:22":                 "ssh://bob@host:22",
		"-p 2222 -l bob -pw secret host":    "-p 2222 -l bob -pw *** host",
		"bob:secret@host":                   "bob:secret@host",
		"not a link":                        "not a link",
	} {
		if got := redactLaunchText(input); got != want {
			t.Errorf("redactLaunchText(%q) = %q, want %q", input, got, want)
		}
	}
}
