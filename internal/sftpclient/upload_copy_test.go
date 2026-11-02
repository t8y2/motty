package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"sync"
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

// ---- Tests for Scheme 2: uploadPipelinedWriterAt ----

type memoryWriterAt struct {
	mu     sync.Mutex
	data   []byte
	errAt  int64
	err    error
	writes []int64
}

func newMemoryWriterAt(capacity int) *memoryWriterAt {
	return &memoryWriterAt{
		data:  make([]byte, capacity),
		errAt: -1,
	}
}

func (m *memoryWriterAt) Write(chunk []byte) (int, error) {
	panic("unexpected sequential write call")
}

func (m *memoryWriterAt) WriteAt(p []byte, off int64) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, off)
	if m.errAt >= 0 && off == m.errAt {
		return 0, m.err
	}
	end := off + int64(len(p))
	if end > int64(len(m.data)) {
		m.data = append(m.data, make([]byte, end-int64(len(m.data)))...)
	}
	copy(m.data[off:end], p)
	return len(p), nil
}

func TestUploadPipelinedWriterAt_EmptyAndSmallFiles(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 1. 空文件 (0 字节)
	sink0 := newMemoryWriterAt(0)
	written0, err0 := worker.uploadCopy(sink0, bytes.NewReader(nil), 0, 0, 0, 0, 0)
	if err0 != nil || written0 != 0 {
		t.Fatalf("empty file: written=%d err=%v", written0, err0)
	}

	// 2. 小文件 (< 32 KiB)
	smallPayload := []byte("hello sftp sliding window")
	sinkSmall := newMemoryWriterAt(len(smallPayload))
	writtenSmall, errSmall := worker.uploadCopy(sinkSmall, bytes.NewReader(smallPayload), 0, 0, 0, 0, 0)
	if errSmall != nil || writtenSmall != int64(len(smallPayload)) {
		t.Fatalf("small file: written=%d err=%v", writtenSmall, errSmall)
	}
	if !bytes.Equal(sinkSmall.data[:writtenSmall], smallPayload) {
		t.Fatalf("small file data mismatch: got %q", sinkSmall.data[:writtenSmall])
	}
}

func TestUploadPipelinedWriterAt_LargeFileFullWindow(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 80 个分片 (80 * 32 KiB = 2,621,440 字节)，超过 64 分片并发窗口
	totalBytes := 80 * 32 * 1024
	payload := make([]byte, totalBytes)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	sink := newMemoryWriterAt(totalBytes)
	written, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err != nil || written != int64(totalBytes) {
		t.Fatalf("large file: written=%d want=%d err=%v", written, totalBytes, err)
	}
	if !bytes.Equal(sink.data[:totalBytes], payload) {
		t.Fatal("large file data corruption")
	}
}

type delayedChunkWriterAt struct {
	mu           sync.Mutex
	data         []byte
	delaySeq0    chan struct{}
	seq0Written  chan struct{}
	otherWritten chan struct{}
}

func (d *delayedChunkWriterAt) Write([]byte) (int, error) { panic("unexpected sequential write") }

func (d *delayedChunkWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off == 0 {
		// 延迟第一个分片的 ACK
		<-d.delaySeq0
		d.mu.Lock()
		copy(d.data[off:], p)
		d.mu.Unlock()
		close(d.seq0Written)
		return len(p), nil
	}
	d.mu.Lock()
	end := off + int64(len(p))
	if end > int64(len(d.data)) {
		d.data = append(d.data, make([]byte, end-int64(len(d.data)))...)
	}
	copy(d.data[off:end], p)
	d.mu.Unlock()
	select {
	case d.otherWritten <- struct{}{}:
	default:
	}
	return len(p), nil
}

func TestUploadPipelinedWriterAt_ReorderedACKPreservesContiguousProgress(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 构造 3 个分片
	payload := bytes.Repeat([]byte("A"), 3*32*1024)
	sink := &delayedChunkWriterAt{
		data:         make([]byte, len(payload)),
		delaySeq0:    make(chan struct{}),
		seq0Written:  make(chan struct{}),
		otherWritten: make(chan struct{}, 10),
	}

	worker.mu.Lock()
	worker.transfers = append(worker.transfers, transferRecord{
		snapshot: TransferSnapshot{ID: 99, State: TransferRunning, BytesTotal: int64(len(payload))},
		rateAt:   worker.clock(),
	})
	worker.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 99, 0, 0, 0)
		done <- err
	}()

	// 等待分片 1 或分片 2 写入并返回
	select {
	case <-sink.otherWritten:
	case <-time.After(time.Second):
		t.Fatal("subsequent chunks did not arrive")
	}

	// 此时分片 0 尚未 ACK，进度必须严格为 0（不能跳跃汇报）
	if got := worker.Transfers(nil)[0].BytesDone; got != 0 {
		t.Fatalf("progress reported ahead of unacknowledged prefix: %d", got)
	}

	// 释放分片 0
	close(sink.delaySeq0)
	select {
	case <-sink.seq0Written:
	case <-time.After(time.Second):
		t.Fatal("chunk 0 did not finish")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("upload did not finish")
	}

	// 全部分片均确认后，最终进度为完整大小
	if got := worker.Transfers(nil)[0].BytesDone; got != int64(len(payload)) {
		t.Fatalf("final confirmed bytes = %d, want %d", got, len(payload))
	}
}

func TestUploadPipelinedWriterAt_WriteErrorAlignsContiguousBytes(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 4 个分片，第 2 个分片 (offset = 2 * 32KB = 64KB) 出错
	payload := bytes.Repeat([]byte("B"), 4*32*1024)
	sink := newMemoryWriterAt(len(payload))
	sink.errAt = 64 * 1024
	sink.err = errors.New("remote disk I/O error")

	written, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 连续成功写入的字节数必须在出错 offset 之前停止
	if written > sink.errAt {
		t.Fatalf("written=%d exceeded error boundary=%d", written, sink.errAt)
	}
}

func TestUploadPipelinedWriterAt_CancelInterruptsStream(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	payload := bytes.Repeat([]byte("C"), 100*32*1024) // 3.2 MB
	sink := newMemoryWriterAt(len(payload))

	worker.mu.Lock()
	worker.transfers = append(worker.transfers, transferRecord{
		snapshot: TransferSnapshot{ID: 101, State: TransferRunning, BytesTotal: int64(len(payload))},
		rateAt:   worker.clock(),
	})
	worker.mu.Unlock()

	// 立即取消
	worker.CancelTransfer(101)

	_, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 101, 0, 0, 0)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("expected ErrCanceled, got %v", err)
	}
}
