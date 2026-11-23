package sessionimport

import (
	"strings"
	"testing"
)

func TestParseXterminalText(t *testing.T) {
	result, err := ParseXterminalText("\ufeffhost=192.0.2.1 port=2222 user=root authType=password password=test@a=b sshProxy=ignored title=中文 测试\r\n\r\nhost=example.com user=deploy title=Second\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 2 || result.PlainSecrets != 1 || len(result.Warnings) != 0 {
		t.Fatalf("unexpected result counts: %+v", result)
	}
	entry := result.Entries[0]
	if entry.Host != "192.0.2.1" || entry.Port != 2222 || entry.User != "root" || entry.Name != "中文 测试" || entry.Password != "test@a=b" || entry.Protocol != SSH {
		t.Fatal("connection fields not preserved")
	}
	if result.Entries[1].Port != 22 {
		t.Fatal("default port not applied")
	}
}

func TestParseXterminalTextInvalidRowsDoNotExposeSecrets(t *testing.T) {
	for _, line := range []string{"", "garbage", "host= port=22", "host=server port=abc", "host=server port=0", "host=server port=65536", "host=server host=other", "host=server authType=key"} {
		t.Run(line, func(t *testing.T) {
			result, err := ParseXterminalText(line + " password=SECRET")
			if err == nil || len(result.Entries) != 0 {
				t.Fatal("invalid connection accepted")
			}
			if strings.Contains(err.Error()+strings.Join(result.Warnings, ""), "SECRET") {
				t.Fatal("secret leaked")
			}
		})
	}
	result, err := ParseXterminalText("host=valid\nhost=bad port=no password=SECRET")
	if err != nil || len(result.Entries) != 1 || len(result.Warnings) != 1 {
		t.Fatal("mixed valid/invalid rows not handled")
	}
}

func TestParseXterminalTextLimits(t *testing.T) {
	if _, err := ParseXterminalText(strings.Repeat("x", MaxFileSize+1)); err == nil {
		t.Fatal("oversized input accepted")
	}
	result, err := ParseXterminalText(strings.Repeat("host=server\n", MaxEntries+1))
	if err != nil || len(result.Entries) != MaxEntries || len(result.Warnings) == 0 {
		t.Fatal("entry limit not enforced")
	}
}
