package workbench

import (
	"os/exec"
	"testing"
)

func TestLocalProxyCommandsRestorePreviousShellEnvironment(t *testing.T) {
	for _, shell := range []string{"sh", "bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s is unavailable", shell)
			}
			start := localProxyEnableCommand(43217)
			script := "http_proxy='before value'; export http_proxy; unset https_proxy HTTP_PROXY HTTPS_PROXY; no_proxy=local; export no_proxy; NO_PROXY=LOCAL; export NO_PROXY\n" + start + "\n" + start + "\n" + localProxyDisableCommand() + `
test "$http_proxy" = 'before value'
test "${https_proxy+x}${HTTP_PROXY+x}${HTTPS_PROXY+x}" = ''
test "$no_proxy" = local
test "$NO_PROXY" = LOCAL
test "${MOTTY_PROXY_ACTIVE+x}" = ''
`
			if output, err := exec.Command(path, "-ec", script).CombinedOutput(); err != nil {
				t.Fatalf("shell restoration: %v: %s", err, output)
			}
		})
	}
}

func TestLocalProxyCommandsDoNotOverwriteForeignBackup(t *testing.T) {
	script := "MOTTY_PROXY_ACTIVE=other; http_proxy=original\n" + localProxyEnableCommand(43217) + "\ntest \"$http_proxy\" = original\n"
	if output, err := exec.Command("sh", "-ec", script).CombinedOutput(); err != nil {
		t.Fatalf("foreign backup: %v: %s", err, output)
	}
}

func TestLocalProxyCommandsDoNotOverwriteStrayBackups(t *testing.T) {
	for _, assignment := range []string{
		"MOTTY_PROXY_https_proxy_SET=x",
		"MOTTY_PROXY_HTTP_PROXY_VALUE=old",
	} {
		t.Run(assignment, func(t *testing.T) {
			script := assignment + "; http_proxy=original\n" + localProxyEnableCommand(43217) + "\ntest \"$http_proxy\" = original\n"
			if output, err := exec.Command("sh", "-ec", script).CombinedOutput(); err != nil {
				t.Fatalf("stray backup: %v: %s", err, output)
			}
		})
	}
}
