package sftpclient

import (
	"os"
	"sync"
	"testing"
)

type identityLinkRemote struct {
	*fakeRemote
	mu      sync.Mutex
	calls   int
	started chan int
	gates   [2]chan struct{}
}

func (r *identityLinkRemote) ReadLink(string) (string, error) {
	r.mu.Lock()
	i := r.calls
	r.calls++
	r.mu.Unlock()
	r.started <- i
	<-r.gates[i]
	return "target", nil
}
func (r *identityLinkRemote) Stat(string) (os.FileInfo, error) { return fakeInfo{name: "target"}, nil }
func (r *identityLinkRemote) Symlink(string, string) error     { return nil }

func TestListingIdentitySurvivesRacingFollowUps(t *testing.T) {
	r := &identityLinkRemote{fakeRemote: &fakeRemote{infos: []os.FileInfo{fakeInfo{name: "link", mode: os.ModeSymlink}}, closed: make(chan struct{})}, started: make(chan int, 2), gates: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
	w := NewWorker(func() (Remote, error) { return r, nil }, nil)
	var once [2]sync.Once
	release := func(i int) { once[i].Do(func() { close(r.gates[i]) }) }
	t.Cleanup(func() { release(0); release(1); w.Close(); <-w.Done() })
	if !w.ListWithID("/same", 41) {
		t.Fatal("list rejected")
	}
	first := waitResult(t, w)
	if first.ListID != 41 || first.TransferID != 0 || first.Background || !first.LinksPending {
		t.Fatalf("first=%+v", first)
	}
	<-r.started
	if !w.PrefetchWithID("/same", 42) {
		t.Fatal("prefetch rejected")
	}
	second := waitResult(t, w)
	if second.ListID != 42 || !second.Background || second.Path != first.Path {
		t.Fatalf("second=%+v", second)
	}
	<-r.started
	release(1)
	follow := waitResult(t, w)
	if !follow.FollowUp || follow.ListID != 42 || !follow.Background {
		t.Fatalf("newer follow=%+v", follow)
	}
	release(0)
	follow = waitResult(t, w)
	if !follow.FollowUp || follow.ListID != 41 || follow.Background {
		t.Fatalf("older follow=%+v", follow)
	}
}

func TestListingIdentityLegacyCallsRemainZero(t *testing.T) {
	r := &fakeRemote{closed: make(chan struct{})}
	w := NewWorker(func() (Remote, error) { return r, nil }, nil)
	t.Cleanup(func() { w.Close(); <-w.Done() })
	for _, background := range []bool{false, true} {
		if background {
			w.Prefetch("/same")
		} else {
			w.List("/same")
		}
		result := waitResult(t, w)
		if result.ListID != 0 || result.TransferID != 0 || result.Background != background || result.Path != "/same" || result.Err != nil {
			t.Fatalf("legacy result=%+v", result)
		}
	}
}
