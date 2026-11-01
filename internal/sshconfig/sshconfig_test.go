package sshconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseFile(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		setup        func(t *testing.T, directory string)
		wantHosts    []Host
		wantWarnings []WarningCode
	}{
		{
			name: "precedence multiple aliases and wildcard defaults",
			config: `
Host alpha beta
    HostName shared.example
Host a*
    User wildcard-user
    Port 2200
Host *
    User fallback-user
    Port 22
Host alpha
    HostName too-late.example
    User too-late
`,
			wantHosts: []Host{
				{Alias: "alpha", HostName: "shared.example", User: "wildcard-user", Port: 2200},
				{Alias: "beta", HostName: "shared.example", User: "fallback-user", Port: 22},
			},
		},
		{
			name: "negated wildcard",
			config: `
Host alpha beta
Host * !beta
    User selected
`,
			wantHosts: []Host{
				{Alias: "alpha", HostName: "alpha", User: "selected"},
				{Alias: "beta", HostName: "beta"},
			},
		},
		{
			name: "pure wildcards are not imported",
			config: `
Host *
    User fallback
Host prod-?
    Port 22
`,
			wantHosts: []Host{},
		},
		{
			name: "relative home include and cycle",
			config: `
Include child.conf
Include ~/shared.conf
Host root
    HostName root.example
`,
			setup: func(t *testing.T, directory string) {
				writeTestConfig(t, filepath.Join(directory, "child.conf"), `
Host included
    User child-user
Include config
`)
				writeTestConfig(t, filepath.Join(directory, "shared.conf"), `
Host home
    HostName home.example
`)
			},
			wantHosts: []Host{
				{Alias: "included", HostName: "included", User: "child-user"},
				{Alias: "home", HostName: "home.example"},
				{Alias: "root", HostName: "root.example"},
			},
			wantWarnings: []WarningCode{WarningIncludeCycle},
		},
		{
			name: "quoted values path expansion and proxy jumps",
			config: `
Host quoted
    HostName = "ssh.example.com"
    User 'Jane Doe'
    IdentityFile "~/keys/id with space"
    ProxyJump "alice@jump1:2200,[2001:db8::1]:2222,bob@jump3"
`,
			wantHosts: []Host{
				{
					Alias:        "quoted",
					HostName:     "ssh.example.com",
					User:         "Jane Doe",
					IdentityFile: filepath.Join("$HOME", "keys", "id with space"),
					ProxyJump: []JumpHost{
						{Host: "jump1", User: "alice", Port: 2200},
						{Host: "2001:db8::1", Port: 2222},
						{Host: "jump3", User: "bob"},
					},
				},
			},
		},
		{
			name: "match commands unsupported directives and dynamic values",
			config: `
Host unsafe
Match exec "test -f /tmp/flag"
    User hidden
Host safe
    HostName safe.example
    ProxyCommand nc gateway %h
    ForwardAgent yes
Host dynamic
    HostName %h.example
    IdentityFile "~/keys/%h"
    ProxyJump jump-%h
`,
			wantHosts: []Host{
				{Alias: "unsafe", HostName: "unsafe"},
				{Alias: "safe", HostName: "safe.example", ProxyCommand: "nc gateway %h"},
				{Alias: "dynamic"},
			},
			wantWarnings: []WarningCode{
				WarningUnsupportedMatch,
				WarningUnsupportedDirective,
				WarningDynamicValue,
				WarningDynamicValue,
				WarningDynamicValue,
			},
		},
		{
			name: "invalid first port blocks later fallback",
			config: `
Host bad-port
    Port 70000
Host *
    Port 22
`,
			wantHosts:    []Host{{Alias: "bad-port", HostName: "bad-port"}},
			wantWarnings: []WarningCode{WarningInvalidPort},
		},
		{
			name: "dynamic first value blocks static fallback",
			config: `
Host dynamic-first
    HostName %h.internal
    IdentityFile "~/keys/%h"
    ProxyJump jump-%h
Host *
    HostName fallback.example
    IdentityFile "~/keys/fallback"
    ProxyJump fallback-jump
`,
			wantHosts: []Host{{Alias: "dynamic-first"}},
			wantWarnings: []WarningCode{
				WarningDynamicValue,
				WarningDynamicValue,
				WarningDynamicValue,
			},
		},
		{
			name: "proxy command kept as written and competing with proxy jump",
			config: `
Host tunnel
    HostName ssh.example.com
    ProxyCommand "C:\Program Files (x86)\cloudflared\cloudflared.exe" access ssh --hostname %h
Host equals
    ProxyCommand=nc -X connect -x 'proxy:3128' %h %p
Host jump-first
    ProxyJump gateway
    ProxyCommand nc %h %p
Host command-first
    ProxyCommand nc %h %p
    ProxyJump gateway
Host disabled
    ProxyCommand none
Host *
    ProxyCommand fallback %h
`,
			wantHosts: []Host{
				{Alias: "tunnel", HostName: "ssh.example.com", ProxyCommand: `"C:\Program Files (x86)\cloudflared\cloudflared.exe" access ssh --hostname %h`},
				{Alias: "equals", HostName: "equals", ProxyCommand: "nc -X connect -x 'proxy:3128' %h %p"},
				{Alias: "jump-first", HostName: "jump-first", ProxyJump: []JumpHost{{Host: "gateway"}}},
				{Alias: "command-first", HostName: "command-first", ProxyCommand: "nc %h %p"},
				{Alias: "disabled", HostName: "disabled"},
			},
		},
		{
			name: "malformed quoted value",
			config: `
Host broken
    HostName "unterminated
`,
			wantHosts:    []Host{{Alias: "broken", HostName: "broken"}},
			wantWarnings: []WarningCode{WarningInvalidSyntax},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("HOME", directory)
			if test.setup != nil {
				test.setup(t, directory)
			}
			path := filepath.Join(directory, "config")
			writeTestConfig(t, path, test.config)
			result, err := ParseFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wantHosts := replaceTestHome(test.wantHosts, directory)
			if !reflect.DeepEqual(result.Hosts, wantHosts) {
				t.Fatalf("Hosts = %#v, want %#v", result.Hosts, wantHosts)
			}
			gotWarnings := warningCodes(result.Warnings)
			if !reflect.DeepEqual(gotWarnings, test.wantWarnings) {
				t.Fatalf("warning codes = %v, want %v; warnings = %+v", gotWarnings, test.wantWarnings, result.Warnings)
			}
			for _, warning := range result.Warnings {
				if warning.Path == "" || warning.Line <= 0 || warning.Detail == "" {
					t.Fatalf("incomplete warning: %+v", warning)
				}
			}
		})
	}
}

func TestIncludeDepthIsBounded(t *testing.T) {
	directory := t.TempDir()
	for index := 0; index < maxIncludeDepth+2; index++ {
		name := filepath.Join(directory, includeName(index))
		content := "Host deepest\n    HostName deepest.example\n"
		if index < maxIncludeDepth+1 {
			content = "Include " + includeName(index+1) + "\n"
		}
		writeTestConfig(t, name, content)
	}
	result, err := ParseFile(filepath.Join(directory, includeName(0)))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hosts) != 0 {
		t.Fatalf("Hosts = %#v, want none beyond include depth", result.Hosts)
	}
	if got := warningCodes(result.Warnings); !reflect.DeepEqual(got, []WarningCode{WarningIncludeDepth}) {
		t.Fatalf("warning codes = %v, want [%s]", got, WarningIncludeDepth)
	}
}

func TestParseFileRootErrors(t *testing.T) {
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing selected file was accepted")
	}
	if _, err := ParseFile(t.TempDir()); err == nil {
		t.Fatal("directory selected as config was accepted")
	}
}

func writeTestConfig(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func warningCodes(warnings []Warning) []WarningCode {
	if len(warnings) == 0 {
		return nil
	}
	codes := make([]WarningCode, len(warnings))
	for index, warning := range warnings {
		codes[index] = warning.Code
	}
	return codes
}

func replaceTestHome(hosts []Host, home string) []Host {
	result := make([]Host, len(hosts))
	copy(result, hosts)
	for index := range result {
		if stringsHasTestHome(result[index].IdentityFile) {
			result[index].IdentityFile = filepath.Join(home, result[index].IdentityFile[len("$HOME")+1:])
		}
	}
	return result
}

func stringsHasTestHome(path string) bool {
	return len(path) > len("$HOME") && path[:len("$HOME")] == "$HOME" && os.IsPathSeparator(path[len("$HOME")])
}

func includeName(index int) string {
	return "config-" + string(rune('a'+index))
}

// OpenSSH reads a backslash before anything but a quote, a backslash or a space
// as itself, which is how a Windows path in IdentityFile is written.
func TestBackslashesKeepWindowsPaths(t *testing.T) {
	for _, test := range []struct{ line, want string }{
		{`IdentityFile C:\Users\me\.ssh\id_ed25519`, `C:\Users\me\.ssh\id_ed25519`},
		{`IdentityFile "C:\\Users\\me\\id key"`, `C:\Users\me\id key`},
		{`IdentityFile C:\Users\me\id\ key`, `C:\Users\me\id key`},
		{`IdentityFile 'C:\Users\me\id'`, `C:\Users\me\id`},
	} {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte("Host box\n    "+test.line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Hosts) != 1 || result.Hosts[0].IdentityFile != test.want {
			t.Errorf("%s read as %+v, want %q", test.line, result.Hosts, test.want)
		}
	}
}
