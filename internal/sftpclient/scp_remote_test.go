package sftpclient

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"motty/internal/scpclient"
)

// commandChannel is one command's far end: no output, a recorded stderr and the
// exit status the server gave it.
type commandChannel struct {
	stderr string
	wait   error
}

func (c *commandChannel) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *commandChannel) Write(p []byte) (int, error) { return len(p), nil }
func (c *commandChannel) CloseWrite() error           { return nil }
func (c *commandChannel) Wait() error                 { return c.wait }
func (c *commandChannel) Stderr() string              { return c.stderr }
func (c *commandChannel) Close() error                { return nil }

// localizedMissingServer answers a removal of a missing path the way a server
// whose session locale is not English does: only a command that pins LC_ALL=C
// prints the English words scpclient maps to os.ErrNotExist.
func localizedMissingServer() (scpclient.Start, *[]string) {
	var ran []string
	start := func(command string) (scpclient.Channel, error) {
		ran = append(ran, command)
		channel := &commandChannel{wait: errors.New("exit status 1")}
		english, localized := "rm: cannot remove '/data/gone': No such file or directory",
			"rm: 无法删除 '/data/gone': 没有那个文件或目录"
		if strings.Contains(command, "rmdir") {
			english, localized = "rmdir: failed to remove '/data/gone': No such file or directory",
				"rmdir: 删除 '/data/gone' 失败: 没有那个文件或目录"
		}
		if strings.HasPrefix(command, "LC_ALL=C ") {
			channel.stderr = english
		} else {
			channel.stderr = localized
		}
		return channel, nil
	}
	return start, &ran
}

// The forced removal treats a path that is already gone as removed, and that
// promise covers the SCP backend too: its removal commands pin the locale, so a
// missing path is recognized even when the server would answer in another one.
func TestSCPRemovalRecognizesAMissingPathInAnyServerLocale(t *testing.T) {
	start, ran := localizedMissingServer()
	remote := NewSCP(start)
	fileRemover, ok := remote.(remoteFileRemover)
	if !ok {
		t.Fatal("the SCP backend cannot remove files")
	}
	directoryRemover, ok := remote.(remoteDirectoryRemover)
	if !ok {
		t.Fatal("the SCP backend cannot remove directories")
	}

	if err := fileRemover.Remove("/data/gone"); err == nil || !IsPathNotFound(err) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SCP Remove of a missing path = %v", err)
	}
	if err := directoryRemover.RemoveDirectory("/data/gone"); err == nil || !IsPathNotFound(err) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SCP RemoveDirectory of a missing path = %v", err)
	}
	for _, command := range *ran {
		if !strings.HasPrefix(command, "LC_ALL=C ") {
			t.Fatalf("removal command %q does not pin the locale", command)
		}
	}
}

// Pinning the locale must not turn a real failure into a missing path: a
// refused removal is still a failure the user has to see.
func TestSCPRemovalKeepsARealFailure(t *testing.T) {
	remote := NewSCP(func(command string) (scpclient.Channel, error) {
		return &commandChannel{stderr: "rm: cannot remove '/data/x': Permission denied", wait: errors.New("exit status 1")}, nil
	})
	remover, ok := remote.(remoteFileRemover)
	if !ok {
		t.Fatal("the SCP backend cannot remove files")
	}
	err := remover.Remove("/data/x")
	if err == nil || IsPathNotFound(err) {
		t.Fatalf("SCP Remove of a refused path = %v", err)
	}
}

// outputChannel is a command whose standard output is fixed text.
type outputChannel struct {
	commandChannel
	out io.Reader
}

func (c *outputChannel) Read(p []byte) (int, error) { return c.out.Read(p) }

// A server that prints a line of its own ahead of pwd's answer is told apart
// from one that printed nothing: the first is a channel the file panel can
// replace with a connection of its own, the second is just an unusable server.
func TestSCPWorkingDirectoryNamesTextAheadOfTheAnswer(t *testing.T) {
	answer := func(output string) error {
		remote := NewSCP(func(string) (scpclient.Channel, error) {
			return &outputChannel{out: strings.NewReader(output)}, nil
		})
		_, err := remote.Getwd()
		return err
	}
	err := answer("fail to get port from linx_config\n/root\n")
	if !IsChannelNoise(err) || !strings.Contains(err.Error(), "linx_config") {
		t.Fatalf("noisy pwd output: %v", err)
	}
	if err := answer(""); err == nil || IsChannelNoise(err) {
		t.Fatalf("empty pwd output: %v", err)
	}
	if err := answer("/root\n"); err != nil {
		t.Fatalf("clean pwd output: %v", err)
	}
}
