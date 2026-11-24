package workbench

import (
	"os"
	"testing"

	"motty/internal/sftpclient"
)

func TestSFTPLinkDestination(t *testing.T) {
	link := func(target string, dir bool) sftpclient.Entry {
		return sftpclient.Entry{Name: "l", Mode: os.ModeSymlink, LinkTarget: target, LinkDir: dir}
	}
	for _, test := range []struct {
		name      string
		directory string
		entry     sftpclient.Entry
		want      string
	}{
		{"absolute folder", "/home/q", link("/media/q/data/", true), "/media/q/data"},
		{"relative folder", "/home/q", link("../shared/docs", true), "/home/shared/docs"},
		{"absolute file shows its folder", "/home/q", link("/etc/hosts", false), "/etc"},
		{"relative file shows its folder", "/home/q", link("sub/a.txt", false), "/home/q/sub"},
	} {
		if got := sftpLinkDestination(test.directory, test.entry); got != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
	if sftpLinkJumpable(sftpclient.Entry{Mode: os.ModeSymlink, LinkTarget: "x", LinkBroken: true}) || sftpLinkJumpable(sftpclient.Entry{LinkTarget: "x"}) {
		t.Error("a broken link or a plain entry offers a jump")
	}
}
