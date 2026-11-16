package workbench

import (
	"errors"
	"testing"

	"motty/internal/ai/capture"
	"motty/internal/ai/gate"
)

func testAgentJob(t *testing.T, owner string) *agentJob {
	t.Helper()
	tap := capture.NewStreamTap(agentJobRetainWindow)
	tap.RetainWindow(agentJobRetainWindow)
	return &agentJob{
		id: "job", owner: owner, entry: &agentEntry{host: Host{CatalogID: "host"}},
		cancel: func() {}, tap: tap, state: gate.JobRunning, exitCode: -1,
	}
}

func TestAgentJobPagesItsOutputByOffset(t *testing.T) {
	job := testAgentJob(t, "client")
	_, _ = jobOutput{job}.Write([]byte("one\ntwo\n"))

	first := job.status(0, 4)
	if first.Data != "one\n" || first.Start != 0 || first.Next != 4 || first.Total != 8 {
		t.Fatalf("first page = %+v", first)
	}
	second := job.status(first.Next, 4)
	if second.Data != "two\n" || second.Start != 4 || second.Next != 8 {
		t.Fatalf("second page = %+v", second)
	}
}

func TestAgentJobStopCancelsOnlyARunningJob(t *testing.T) {
	job := testAgentJob(t, "client")
	if !job.stop() {
		t.Fatal("a running job was not stopped")
	}
	if status := job.status(0, 4); status.State != gate.JobCancelled {
		t.Fatalf("state after stop = %v", status.State)
	}
	if job.stop() {
		t.Fatal("a cancelled job was stopped again")
	}
}

func TestAgentJobBelongsToItsClientSession(t *testing.T) {
	job := testAgentJob(t, "client")
	backend := &agentBackend{jobs: map[string]*agentJob{job.id: job}}
	if _, err := backend.job("someone-else", job.id); !errors.Is(err, gate.ErrNoJob) {
		t.Fatalf("another client's job = %v", err)
	}
	if found, err := backend.job("client", job.id); err != nil || found != job {
		t.Fatalf("own job = %v, %v", found, err)
	}
}
