package sshconfig

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConnectionOptionsFollowOpenSSHPrecedence(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config")
	writeTestConfig(t, path, `
Host specific disabled inherited
Host specific
    ServerAliveInterval 1m30s
    ServerAliveCountMax 5
    IdentitiesOnly yes
    IdentityFile /keys/specific
Host disabled
    ServerAliveInterval 0
    ServerAliveCountMax 0
    IdentitiesOnly no
Host *
    ServerAliveInterval 15
    ServerAliveCountMax 3
    IdentitiesOnly yes
    IdentityFile /keys/shared
`)
	result, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 || len(result.Hosts) != 3 {
		t.Fatalf("result = %+v", result)
	}
	for index, expected := range []struct {
		interval, count int
		only            bool
		paths           []string
	}{
		{90, 5, true, []string{"/keys/specific", "/keys/shared"}},
		{0, 0, false, []string{"/keys/shared"}},
		{15, 3, true, []string{"/keys/shared"}},
	} {
		options := result.Hosts[index].Options
		if options == nil || *options.ServerAliveInterval != expected.interval || *options.ServerAliveCountMax != expected.count ||
			*options.IdentitiesOnly != expected.only || !reflect.DeepEqual(options.IdentityFiles, expected.paths) {
			t.Fatalf("host %s options = %+v", result.Hosts[index].Alias, options)
		}
	}
}

func TestInvalidConnectionOptionsWarnWithoutUsingLaterDefaults(t *testing.T) {
	for _, directive := range []string{
		"ServerAliveInterval -1", "ServerAliveInterval 2147483648", "ServerAliveInterval 999999999w",
		"ServerAliveInterval exec", "ServerAliveCountMax -1", "ServerAliveCountMax 1m", "IdentitiesOnly maybe",
	} {
		t.Run(directive, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			name, _, _ := strings.Cut(directive, " ")
			fallback := "0"
			if name == "IdentitiesOnly" {
				fallback = "no"
			}
			writeTestConfig(t, path, "Host test\n    "+directive+"\nHost *\n    "+name+" "+fallback+"\n")
			result, err := ParseFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Warnings) != 1 || result.Warnings[0].Code != WarningInvalidSyntax || result.Hosts[0].Options != nil {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func FuzzServerAliveInterval(fuzz *testing.F) {
	for _, seed := range []string{"0", "15", "1m30s", "2h", "2147483648", "-1", "1x"} {
		fuzz.Add(seed)
	}
	fuzz.Fuzz(func(t *testing.T, value string) {
		seconds, err := parseNonnegativeOption(value, true)
		if err == nil && (seconds < 0 || seconds > 2147483647) {
			t.Fatalf("accepted interval outside OpenSSH's range: %q = %d", value, seconds)
		}
	})
}
