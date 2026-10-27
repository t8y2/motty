package workbench

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"
)

// This opt-in check executes the actual product commands through OpenSSH.
// Test content, archives, comparisons and cleanup all stay on the server.
func TestArchiveCommandsOverSSH(t *testing.T) {
	endpoints := os.Getenv("MOTTY_TEST_SSH_ENDPOINTS")
	if endpoints == "" {
		t.Skip("set MOTTY_TEST_SSH_ENDPOINTS, MOTTY_TEST_SSH_USER, MOTTY_TEST_SSH_KEY and MOTTY_TEST_SSH_KNOWN_HOSTS")
	}
	sshPath := os.Getenv("CODEX_SSH_PATH")
	if sshPath == "" {
		sshPath = "ssh"
	}
	user, key, knownHosts := os.Getenv("MOTTY_TEST_SSH_USER"), os.Getenv("MOTTY_TEST_SSH_KEY"), os.Getenv("MOTTY_TEST_SSH_KNOWN_HOSTS")
	if user == "" || key == "" || knownHosts == "" {
		t.Fatal("all SSH test credentials and known hosts must be explicitly configured")
	}
	for index, endpoint := range strings.Split(endpoints, ",") {
		t.Run(endpoint, func(t *testing.T) {
			host, port, err := net.SplitHostPort(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			root := fmt.Sprintf("/tmp/motty-archive-%d-%d", time.Now().UnixNano(), index)
			source := path.Join(root, "-目录 it's here; $literal")
			target, zipTarget := path.Join(root, "tar output.tar.gz"), path.Join(root, "中文 output.zip")
			extracted, zipExtracted := path.Join(root, "tar unpacked"), path.Join(root, "zip unpacked")
			file := path.Join(source, "中文.txt")
			var script strings.Builder
			script.WriteString("set -eu\nmkdir -- " + shellQuote(root) + "\ntrap " + shellQuote("rm -rf -- "+shellQuote(root)) + " 0\n")
			script.WriteString("mkdir -- " + shellQuote(source) + "\nmkdir -- " + shellQuote(path.Join(source, "empty")) + "\nprintf '%s\\n' 'server-side archive contents' > " + shellQuote(file) + "\n")
			script.WriteString(compressCommand(source, target) + "\n")
			script.WriteString(extractCommand(archiveTar, target, extracted) + "\n")
			script.WriteString("cmp -- " + shellQuote(file) + " " + shellQuote(path.Join(extracted, path.Base(source), "中文.txt")) + "\ntest -d " + shellQuote(path.Join(extracted, path.Base(source), "empty")) + "\n")
			script.WriteString("before=$(cksum -- " + shellQuote(target) + ")\nset +e\noutput=$( " + compressCommand(source, target) + ")\nresult=$?\nset -e\ntest \"$result\" -eq 17\ntest \"$before\" = \"$(cksum -- " + shellQuote(target) + ")\"\n")
			symlink := path.Join(root, "existing.tar.gz")
			script.WriteString("ln -s -- nowhere " + shellQuote(symlink) + "\nset +e\noutput=$( " + compressCommand(source, symlink) + ")\nresult=$?\nset -e\ntest \"$result\" -eq 17\ntest -L " + shellQuote(symlink) + "\n")
			script.WriteString("if command -v zip >/dev/null 2>&1 && command -v unzip >/dev/null 2>&1; then\n")
			script.WriteString(compressCommand(source, zipTarget) + "\n" + extractCommand(archiveZip, zipTarget, zipExtracted) + "\n")
			script.WriteString("cmp -- " + shellQuote(file) + " " + shellQuote(path.Join(zipExtracted, path.Base(source), "中文.txt")) + "\n")
			badZip, badTarget := path.Join(root, "bad.zip"), path.Join(root, "bad extraction")
			script.WriteString("printf '%s' not-a-zip > " + shellQuote(badZip) + "\nset +e\noutput=$( " + extractCommand(archiveZip, badZip, badTarget) + ")\nresult=$?\nset -e\ntest \"$result\" -ne 0\ntest ! -e " + shellQuote(badTarget) + "\necho 'ZIP roundtrip and corrupt-archive check passed'\n")
			script.WriteString("else\nif ! command -v unzip >/dev/null 2>&1; then\nset +e\noutput=$( " + extractCommand(archiveZip, zipTarget, zipExtracted) + ")\nresult=$?\nset -e\ntest \"$result\" -eq 127\ncase \"$output\" in *" + archiveMissingTool + "unzip*) :;; *) exit 91;; esac\ntest ! -e " + shellQuote(zipExtracted) + "\nfi\n")
			script.WriteString("if ! command -v zip >/dev/null 2>&1; then\nset +e\noutput=$( " + compressCommand(source, zipTarget) + ")\nresult=$?\nset -e\ntest \"$result\" -eq 127\ncase \"$output\" in *" + archiveMissingTool + "zip*) :;; *) exit 92;; esac\ntest ! -e " + shellQuote(zipTarget) + "\nfi\necho 'Missing ZIP tools rejected without creating output'\nfi\n")
			script.WriteString("test -z \"$(find " + shellQuote(root) + " -maxdepth 1 -name '.motty-compress.*' -print)\"\necho 'Server-only archive checks passed'\n")
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, sshPath, "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "UserKnownHostsFile="+knownHosts, "-i", key, "-p", port, user+"@"+host, "sh", "-s")
			command.Stdin = strings.NewReader(script.String())
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("server archive checks: %v\n%s", err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
		})
	}
}
