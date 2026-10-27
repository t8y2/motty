package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

type concurrentSink struct {
	data        bytes.Buffer
	used        bool
	concurrency int
}

func (sink *concurrentSink) Write(chunk []byte) (int, error) { return sink.data.Write(chunk) }

func (sink *concurrentSink) ReadFromWithConcurrency(source io.Reader, concurrency int) (int64, error) {
	sink.used = true
	sink.concurrency = concurrency
	return io.Copy(&sink.data, source)
}

type refusedSink struct {
	data bytes.Buffer
	used bool
}

func (sink *refusedSink) Write(chunk []byte) (int, error) { return sink.data.Write(chunk) }

func (sink *refusedSink) ReadFromWithConcurrency(io.Reader, int) (int64, error) {
	sink.used = true
	return 0, errors.New("pipelining refused")
}

func TestUploadCopyPipelinesWithReadFromConcurrency(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	payload := bytes.Repeat([]byte("pipelined "), 4096)
	sink := &concurrentSink{}
	written, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err != nil || written != int64(len(payload)) || !bytes.Equal(sink.data.Bytes(), payload) {
		t.Fatalf("written=%d sink=%d err=%v", written, sink.data.Len(), err)
	}
	if !sink.used || sink.concurrency != uploadConcurrency {
		t.Fatalf("used=%v concurrency=%d, want %d", sink.used, sink.concurrency, uploadConcurrency)
	}
}

func TestUploadCopyFallsBackToSequentialWrites(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	payload := []byte("sequential payload")
	var sink bytes.Buffer
	written, err := worker.uploadCopy(&sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err != nil || written != int64(len(payload)) || !bytes.Equal(sink.Bytes(), payload) {
		t.Fatalf("written=%d sink=%q err=%v", written, sink.String(), err)
	}
}

func TestUploadCopyRetriesSequentiallyWhenReadFromFailsBeforeReading(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	payload := []byte("fallback payload")
	sink := &refusedSink{}
	written, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err != nil || written != int64(len(payload)) || !bytes.Equal(sink.data.Bytes(), payload) {
		t.Fatalf("written=%d sink=%q err=%v", written, sink.data.String(), err)
	}
	if !sink.used {
		t.Fatal("the pipelined path was not tried")
	}
}

// A server may consume the source into request buffers before acknowledging any
// write. Progress must wait for confirmation, then advance between batches.
type delayedUploadAck struct {
	read chan int64
	ack  chan struct{}
	err  error
}

func (*delayedUploadAck) Write([]byte) (int, error) { panic("sequential fallback was not expected") }

func (sink *delayedUploadAck) ReadFromWithConcurrency(source io.Reader, concurrency int) (int64, error) {
	n, err := io.Copy(io.Discard, source)
	if n > 0 {
		sink.read <- n
		<-sink.ack
	}
	if sink.err != nil {
		return n, sink.err
	}
	return n, err
}

func TestUploadProgressWaitsForRemoteAcknowledgement(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write failure"}[fail], func(t *testing.T) {
			worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
			defer worker.Close()
			payload := bytes.Repeat([]byte("x"), int(2*uploadProgressBatch+1))
			worker.mu.Lock()
			worker.transfers = append(worker.transfers, transferRecord{snapshot: TransferSnapshot{ID: 1, State: TransferRunning, BytesTotal: int64(len(payload))}, rateAt: worker.clock()})
			worker.mu.Unlock()
			sink := &delayedUploadAck{read: make(chan int64, 4), ack: make(chan struct{}, 4)}
			if fail {
				sink.err = errors.New("remote write failed")
			}
			defer close(sink.ack)
			done := make(chan error, 1)
			go func() { _, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 1, 0, 0, 0); done <- err }()
			waitRead := func() {
				t.Helper()
				select {
				case <-sink.read:
				case <-time.After(time.Second):
					t.Fatal("upload did not reach pending acknowledgement")
				}
			}
			waitRead()
			if got := worker.Transfers(nil)[0].BytesDone; got != 0 {
				t.Fatalf("read-ahead reported as uploaded: %d", got)
			}
			sink.ack <- struct{}{}
			if !fail {
				waitRead()
				if got := worker.Transfers(nil)[0].BytesDone; got != uploadProgressBatch {
					t.Fatalf("intermediate confirmed bytes=%d, want %d", got, uploadProgressBatch)
				}
				sink.ack <- struct{}{}
				waitRead()
				if got := worker.Transfers(nil)[0].BytesDone; got != 2*uploadProgressBatch {
					t.Fatalf("second confirmed bytes=%d", got)
				}
				sink.ack <- struct{}{}
			}
			select {
			case err := <-done:
				if fail != (err != nil) {
					t.Fatalf("err=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("upload did not finish")
			}
			want := int64(len(payload))
			if fail {
				want = 0
			}
			if got := worker.Transfers(nil)[0].BytesDone; got != want {
				t.Fatalf("confirmed bytes=%d, want %d", got, want)
			}
		})
	}
}
