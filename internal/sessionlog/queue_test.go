package sessionlog

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func awaitAck(t *testing.T, ack <-chan error) error {
	t.Helper()
	select {
	case err, ok := <-ack:
		if !ok {
			t.Fatal("ack channel closed without a result")
		}
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a queue acknowledgment")
		return nil
	}
}

func TestQueueOrdersStartBeforeFinish(t *testing.T) {
	store, _ := openTestStore(t)
	queue := NewQueue(store)
	t.Cleanup(func() { _ = queue.Close() })
	base := testBase()
	id := newUniqueID(t)

	acks := []<-chan error{
		queue.Submit(Operation{Kind: OpStart, Record: sampleRecord(id, base)}),
		queue.Submit(Operation{Kind: OpAppendEvent, Event: Event{RecordID: id, Kind: EventConnected, At: base.Add(time.Second)}}),
		queue.Submit(Operation{Kind: OpFinish, ID: id, Status: StatusEnded, EndedAt: base.Add(time.Minute), Reason: ReasonUserClosed}),
	}
	for i, ack := range acks {
		if err := awaitAck(t, ack); err != nil {
			t.Fatalf("queued operation %d: %v", i, err)
		}
	}

	got := findRecord(t, store, id)
	if got.Status != StatusEnded || !got.EndedAt.Equal(base.Add(time.Minute)) || got.Reason != ReasonUserClosed {
		t.Fatalf("record after queued start/event/finish = %+v", got)
	}
	events, err := store.Events(id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 3 || events[0].Kind != EventStarted || events[1].Kind != EventConnected || events[2].Kind != EventFinished {
		t.Fatalf("events after queued writes = %+v", events)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Queue.Close: %v", err)
	}
}

func TestQueueKeepsPerCallerOrder(t *testing.T) {
	store, _ := openTestStore(t)
	queue := NewQueue(store)
	t.Cleanup(func() { _ = queue.Close() })
	base := testBase()

	var acks []<-chan error
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("ordered-%02d", i)
		acks = append(acks,
			queue.Submit(Operation{Kind: OpStart, Record: sampleRecord(id, base.Add(time.Duration(i)*time.Millisecond))}),
			queue.Submit(Operation{Kind: OpFinish, ID: id, Status: StatusEnded, EndedAt: base.Add(time.Hour), Reason: ReasonUserClosed}),
		)
	}
	for i, ack := range acks {
		if err := awaitAck(t, ack); err != nil {
			t.Fatalf("queued operation %d: %v", i, err)
		}
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Queue.Close: %v", err)
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("ordered-%02d", i)
		if got := findRecord(t, store, id); got.Status != StatusEnded {
			t.Fatalf("%q status = %v, want ended: its finish preceded its start", id, got.Status)
		}
	}
}

func TestQueueAcksAreBufferedAndClosedEvenIfIgnored(t *testing.T) {
	store, _ := openTestStore(t)
	queue := NewQueue(store)
	t.Cleanup(func() { _ = queue.Close() })
	base := testBase()
	id := newUniqueID(t)

	if err := awaitAck(t, queue.Submit(Operation{Kind: OpStart, Record: sampleRecord(id, base)})); err != nil {
		t.Fatalf("start: %v", err)
	}
	ignored := queue.Submit(Operation{Kind: OpFinish, ID: id, Status: StatusEnded, EndedAt: base.Add(time.Minute), Reason: ReasonUserClosed})
	// The writer must not block on an acknowledgment nobody reads.
	if err := queue.Close(); err != nil {
		t.Fatalf("Queue.Close with an ignored ack: %v", err)
	}

	select {
	case err, ok := <-ignored:
		if !ok {
			t.Fatal("ignored ack closed without a result")
		}
		if err != nil {
			t.Fatalf("ignored ack error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ignored ack was never delivered")
	}
	if _, ok := <-ignored; ok {
		t.Fatal("ack channel delivered more than one result")
	}
	if got := findRecord(t, store, id); got.Status != StatusEnded {
		t.Fatalf("drained record = %+v, want ended", got)
	}
}

func TestQueueCloseDrainsPendingWrites(t *testing.T) {
	store, _ := openTestStore(t)
	queue := NewQueue(store)
	base := testBase()
	id := newUniqueID(t)

	// No acknowledgment is read before Close: Close drains what is pending.
	queue.Submit(Operation{Kind: OpStart, Record: sampleRecord(id, base)})
	queue.Submit(Operation{Kind: OpAppendEvent, Event: Event{RecordID: id, Kind: EventConnected, At: base.Add(time.Second)}})
	queue.Submit(Operation{Kind: OpAppendEvent, Event: Event{RecordID: id, Kind: EventShellOpened, At: base.Add(2 * time.Second)}})
	if err := queue.Close(); err != nil {
		t.Fatalf("Queue.Close: %v", err)
	}
	if got := findRecord(t, store, id); got.Status != StatusActive {
		t.Fatalf("record after draining Close = %+v, want active", got)
	}
	events, err := store.Events(id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 3 || events[0].Kind != EventStarted || events[2].Kind != EventShellOpened {
		t.Fatalf("drained events = %+v", events)
	}
}

func TestQueueConcurrentSubmissionsAreAllApplied(t *testing.T) {
	store, _ := openTestStore(t)
	queue := NewQueue(store)
	t.Cleanup(func() { _ = queue.Close() })
	base := testBase()
	const count = 40
	acks := make([]<-chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-queue-%02d", i)
			acks[i] = queue.Submit(Operation{Kind: OpStart, Record: sampleRecord(id, base.Add(time.Duration(i)*time.Millisecond))})
		}(i)
	}
	wg.Wait()
	for i, ack := range acks {
		if err := awaitAck(t, ack); err != nil {
			t.Fatalf("submission %d: %v", i, err)
		}
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Queue.Close: %v", err)
	}
	if got := countRows(t, store, ""); got != count {
		t.Fatalf("records after concurrent submissions = %d, want %d", got, count)
	}
}

func TestQueueSubmitAfterCloseReturnsClosedError(t *testing.T) {
	store, _ := openTestStore(t)
	queue := NewQueue(store)
	if err := queue.Close(); err != nil {
		t.Fatalf("Queue.Close: %v", err)
	}
	ack := queue.Submit(Operation{Kind: OpClear})
	select {
	case err, ok := <-ack:
		if !ok {
			t.Fatal("post-Close ack closed without a result")
		}
		if !errors.Is(err, ErrQueueClosed) {
			t.Fatalf("post-Close ack error = %v, want ErrQueueClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("post-Close Submit did not return a result")
	}
	if _, ok := <-ack; ok {
		t.Fatal("post-Close ack channel delivered more than one result")
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("second Queue.Close: %v", err)
	}
}

func TestQueueReportsWriteErrors(t *testing.T) {
	store, _ := openTestStore(t)
	queue := NewQueue(store)
	bad := Record{ID: newUniqueID(t), RunID: "run-1", Host: "", Protocol: ProtocolSSH, StartedAt: testBase()}
	if err := awaitAck(t, queue.Submit(Operation{Kind: OpStart, Record: bad})); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("queued invalid start error = %v, want ErrInvalidRecord", err)
	}
	if err := awaitAck(t, queue.Submit(Operation{Kind: OperationKind(99)})); err == nil {
		t.Fatal("queued unknown operation reported no error")
	}
	if err := queue.Close(); err == nil {
		t.Fatal("Queue.Close did not report the failed writes")
	}
}

func TestQueueWriteFailureCannotDeadlockFullSubmitAndClose(t *testing.T) {
	store, _ := openTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	queue := NewQueue(store)
	queue.Submit(Operation{Kind: OpStart}) // writer waits for the closed store's mutex
	deadline := time.Now().Add(time.Second)
	for len(queue.ops) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(queue.ops) != 0 {
		store.mu.Unlock()
		t.Fatal("writer did not start the first operation")
	}
	for i := 0; i < queueBuffer; i++ {
		queue.Submit(Operation{Kind: OpStart})
	}
	submitted := make(chan struct{})
	go func() {
		queue.Submit(Operation{Kind: OpStart})
		close(submitted)
	}()
	for time.Now().Before(deadline) {
		if !queue.mu.TryLock() {
			break
		}
		queue.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	store.mu.Unlock()
	select {
	case <-submitted:
	case <-time.After(time.Second):
		t.Fatal("Submit is deadlocked behind the writer's error path")
	}
	closed := make(chan error, 1)
	go func() { closed <- queue.Close() }()
	select {
	case err := <-closed:
		if !errors.Is(err, ErrClosed) && !errors.Is(err, ErrQueueFull) {
			t.Fatalf("Close error = %v, want the failed or refused write", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close is deadlocked after a full queue and write failure")
	}
}

func TestQueueFullDoesNotBlockTheWindowThread(t *testing.T) {
	store, _ := openTestStore(t)
	store.mu.Lock()
	queue := NewQueue(store)
	queue.Submit(Operation{Kind: OpStart})
	deadline := time.Now().Add(time.Second)
	for len(queue.ops) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(queue.ops) != 0 {
		store.mu.Unlock()
		t.Fatal("writer did not start")
	}
	for i := 0; i < queueBuffer; i++ {
		queue.Submit(Operation{Kind: OpStart})
	}
	result := make(chan (<-chan error), 1)
	go func() { result <- queue.Submit(Operation{Kind: OpStart}) }()
	select {
	case ack := <-result:
		if err := awaitAck(t, ack); !errors.Is(err, ErrQueueFull) {
			store.mu.Unlock()
			t.Fatalf("full queue result = %v, want ErrQueueFull", err)
		}
	case <-time.After(100 * time.Millisecond):
		store.mu.Unlock()
		t.Fatal("a full journal queue blocked a connection callback")
	}
	store.mu.Unlock()
	if err := queue.Close(); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("Close error = %v, want dropped write reported", err)
	}
}

func TestQueueWaitOrdersWritesAfterAnotherWindow(t *testing.T) {
	store, _ := openTestStore(t)
	first := NewQueue(store)
	second := NewQueue(store)
	defer first.Close()
	defer second.Close()
	gate := make(chan struct{})
	id := newUniqueID(t)
	second.Submit(Operation{Kind: OpWait, Wait: gate})
	finished := second.Submit(Operation{Kind: OpFinish, ID: id, Status: StatusEnded,
		EndedAt: testBase().Add(time.Minute), Reason: ReasonUserClosed})
	select {
	case <-finished:
		t.Fatal("destination finalized before the source window's writes landed")
	case <-time.After(20 * time.Millisecond):
	}
	start := first.Submit(Operation{Kind: OpStart, Record: sampleRecord(id, testBase())})
	if err := awaitAck(t, start); err != nil {
		t.Fatal(err)
	}
	close(gate)
	if err := awaitAck(t, finished); err != nil {
		t.Fatalf("ordered finish: %v", err)
	}
	if got := findRecord(t, store, id); got.Status != StatusEnded {
		t.Fatalf("moved session status = %v", got.Status)
	}
}

func TestQueueFenceStillTracksAcceptedWritesWhenTheQueueIsFull(t *testing.T) {
	store, _ := openTestStore(t)
	store.mu.Lock()
	queue := NewQueue(store)
	queue.Submit(Operation{Kind: OpStart, Record: sampleRecord("first", testBase())})
	deadline := time.Now().Add(time.Second)
	for len(queue.ops) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(queue.ops) != 0 {
		store.mu.Unlock()
		t.Fatal("writer did not start")
	}
	for i := 0; i < queueBuffer; i++ {
		queue.Submit(Operation{Kind: OpStart, Record: sampleRecord(fmt.Sprintf("item-%d", i), testBase())})
	}
	fence := queue.Fence()
	select {
	case <-fence:
		store.mu.Unlock()
		t.Fatal("fence closed before prior writes completed")
	default:
	}
	if err := awaitAck(t, queue.Submit(Operation{Kind: OpStart})); !errors.Is(err, ErrQueueFull) {
		store.mu.Unlock()
		t.Fatalf("full queue = %v", err)
	}
	store.mu.Unlock()
	select {
	case <-fence:
	case <-time.After(5 * time.Second):
		t.Fatal("fence never completed after accepted writes")
	}
	queue.Close()
}
