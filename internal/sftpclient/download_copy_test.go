package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type writerToSource struct {
	data []byte
	used bool
}

func (source *writerToSource) WriteTo(destination io.Writer) (int64, error) {
	source.used = true
	written, err := destination.Write(source.data)
	return int64(written), err
}

func (source *writerToSource) Read([]byte) (int, error) { return 0, io.EOF }

type plainSource struct {
	data   []byte
	offset int
}

func (source *plainSource) Read(buffer []byte) (int, error) {
	if source.offset == len(source.data) {
		return 0, io.EOF
	}
	read := copy(buffer, source.data[source.offset:])
	source.offset += read
	return read, nil
}

// A source that advertises WriteTo but refuses before moving a byte (an SFTP
// server that will not stat an open file) must fall back to the sequential copy
// with the data intact.
type refusedWriterTo struct {
	data []byte
	used bool
	read bool
}

func (source *refusedWriterTo) WriteTo(io.Writer) (int64, error) {
	source.used = true
	return 0, errors.New("server does not stat open files")
}

func (source *refusedWriterTo) Read(buffer []byte) (int, error) {
	source.read = true
	if len(source.data) == 0 {
		return 0, io.EOF
	}
	read := copy(buffer, source.data)
	source.data = source.data[read:]
	return read, nil
}

func TestDownloadCopyPipelinesWithWriterTo(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	source := &writerToSource{data: []byte("pipelined payload")}
	var destination bytes.Buffer
	written, err := worker.downloadCopy(&destination, source, 0, 0, 0, 0, 0)
	if err != nil || written != int64(len(source.data)) || destination.String() != string(source.data) {
		t.Fatalf("written=%d destination=%q err=%v", written, destination.String(), err)
	}
	if !source.used {
		t.Fatal("the writer-to path was not used")
	}
}

func TestDownloadCopyFallsBackToSequentialReads(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	source := &plainSource{data: []byte("sequential payload")}
	var destination bytes.Buffer
	written, err := worker.downloadCopy(&destination, source, 0, 0, 0, 0, 0)
	if err != nil || written != int64(len(source.data)) || destination.String() != string(source.data) {
		t.Fatalf("written=%d destination=%q err=%v", written, destination.String(), err)
	}
}

func TestDownloadCopyRetriesSequentiallyWhenWriteToFailsBeforeWriting(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	source := &refusedWriterTo{data: []byte("fallback payload")}
	var destination bytes.Buffer
	written, err := worker.downloadCopy(&destination, source, 0, 0, 0, 0, 0)
	if err != nil || written != int64(len("fallback payload")) || destination.String() != "fallback payload" {
		t.Fatalf("written=%d destination=%q err=%v", written, destination.String(), err)
	}
	if !source.used || !source.read {
		t.Fatalf("writer-to=%v sequential=%v, want both", source.used, source.read)
	}
}

type failedDownloadWriter struct{ err error }

func (writer failedDownloadWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestDownloadCopyDoesNotHideDestinationFailureWithFallback(t *testing.T) {
	for _, writeErr := range []error{errors.New("disk full"), nil} {
		worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
		defer worker.Close()
		source := &writerToSource{data: []byte("must not be discarded")}
		written, err := worker.downloadCopy(failedDownloadWriter{writeErr}, source, 0, 0, 0, 0, 0)
		want := writeErr
		if want == nil {
			want = io.ErrShortWrite
		}
		if written != 0 || !errors.Is(err, want) {
			t.Fatalf("written=%d err=%v, want %v", written, err, want)
		}
	}
}
