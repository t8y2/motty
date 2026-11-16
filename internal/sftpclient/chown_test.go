package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// chownRemote serves the account files the worker resolves names against and
// records the ids it was asked to set.
type chownRemote struct {
	fakeRemote
	passwd, group []byte
	chownErr      error
	calls         []chownCall
}

type chownCall struct {
	path     string
	uid, gid uint32
}

func (remote *chownRemote) Open(path string) (io.ReadCloser, error) {
	switch path {
	case "/etc/passwd":
		return io.NopCloser(bytes.NewReader(remote.passwd)), nil
	case "/etc/group":
		return io.NopCloser(bytes.NewReader(remote.group)), nil
	}
	return remote.fakeRemote.Open(path)
}

func (remote *chownRemote) Chown(path string, uid, gid uint32) error {
	remote.calls = append(remote.calls, chownCall{path: path, uid: uid, gid: gid})
	return remote.chownErr
}

func TestWorkerChownResolvesAccountNamesAndAcceptsNumericIDs(t *testing.T) {
	remote := &chownRemote{
		fakeRemote: fakeRemote{closed: make(chan struct{})},
		passwd:     []byte("deploy:x:1001:1001::/home/deploy:/bin/sh\n"),
		group:      []byte("staff:x:50:\n"),
	}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	result := requireQueuedResult(t, worker.Chown("app.conf", "deploy", "staff"), worker, OperationChown)
	if result.Err != nil {
		t.Fatalf("chown by name = %+v", result)
	}
	if result.Owner != "deploy" || result.Group != "staff" {
		t.Fatalf("the result does not echo what was asked for: %+v", result)
	}
	if len(remote.calls) != 1 || remote.calls[0] != (chownCall{path: "app.conf", uid: 1001, gid: 50}) {
		t.Fatalf("the resolved ids were not set: %+v", remote.calls)
	}

	// A number is the id itself and needs no account file.
	result = requireQueuedResult(t, worker.Chown("root.conf", "0", "0"), worker, OperationChown)
	if result.Err != nil {
		t.Fatalf("chown by id = %+v", result)
	}
	if len(remote.calls) != 2 || remote.calls[1] != (chownCall{path: "root.conf", uid: 0, gid: 0}) {
		t.Fatalf("numeric ids were not passed through: %+v", remote.calls)
	}
}

func TestWorkerChownRefusesANameTheAccountFilesDoNotList(t *testing.T) {
	remote := &chownRemote{
		fakeRemote: fakeRemote{closed: make(chan struct{})},
		passwd:     []byte("deploy:x:1001:1001::/home/deploy:/bin/sh\n"),
		group:      []byte("staff:x:50:\n"),
	}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	result := requireQueuedResult(t, worker.Chown("app.conf", "nobody", "staff"), worker, OperationChown)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "no user named") {
		t.Fatalf("an unknown user was not refused: %+v", result)
	}
	if len(remote.calls) != 0 {
		t.Fatalf("an unresolved request reached the remote: %+v", remote.calls)
	}
}

func TestWorkerChownReportsTheServerRefusal(t *testing.T) {
	refused := errors.New("permission denied")
	remote := &chownRemote{
		fakeRemote: fakeRemote{closed: make(chan struct{})},
		passwd:     []byte("deploy:x:1001:1001::/home/deploy:/bin/sh\n"),
		group:      []byte("staff:x:50:\n"),
		chownErr:   refused,
	}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	result := requireQueuedResult(t, worker.Chown("app.conf", "deploy", "staff"), worker, OperationChown)
	if !errors.Is(result.Err, refused) {
		t.Fatalf("the server's refusal was lost: %+v", result)
	}
}
