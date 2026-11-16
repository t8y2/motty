package remotetools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestProcessSnapshotIncludesLowLoadRowsBeyondTopSixty(t *testing.T) {
	var output strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&output, "%d app %.1f 0.1 01:00 service-%d\n", i, float64(101-i), i)
	}
	output.WriteString("9999 oracle 0.0 0.0 02:00 audit-needle --stay-idle\n")
	runner := &fixedRunner{output: output.String()}
	got, err := ReadProcessSnapshot(context.Background(), runner, SortCPU)
	if err != nil || len(got.Processes) != 101 || got.Truncated {
		t.Fatalf("snapshot=%+v err=%v", got, err)
	}
	if got.Processes[100].PID != 9999 {
		t.Fatal("idle process beyond top60 was not collected")
	}
	if strings.Contains(runner.asked, "head -n 68") {
		t.Fatal("snapshot still uses top60 query")
	}
	legacy, err := ParseProcesses([]byte(output.String()), SortCPU)
	if err != nil || len(legacy) != 60 {
		t.Fatal("legacy parser's top60 contract changed")
	}
}

func TestProcessSnapshotLabelsBoundedOutput(t *testing.T) {
	var output strings.Builder
	for i := 1; i <= 5001; i++ {
		fmt.Fprintf(&output, "%d root 0.0 0.0 00:00 service\n", i)
	}
	got, err := ReadProcessSnapshot(context.Background(), &fixedRunner{output: output.String()}, SortMemory)
	if err != nil || len(got.Processes) != 5000 || !got.Truncated {
		t.Fatalf("rows=%d truncated=%v err=%v", len(got.Processes), got.Truncated, err)
	}
}

func TestProcessSnapshotLegacyFallbackDoesNotLoseIdleRows(t *testing.T) {
	output := "USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND\n"
	for i := 1; i <= 80; i++ {
		output += fmt.Sprintf("root %d 0.1 0.2 10 2 ? S 08:00 0:01 service-%d\n", i, i)
	}
	runner := &scriptedRunner{results: []scriptedResult{{output: "ps: invalid option -- e\n"}, {output: output}}}
	got, err := ReadProcessSnapshot(context.Background(), runner, SortCPU)
	if err != nil || len(got.Processes) != 80 {
		t.Fatalf("rows=%d err=%v", len(got.Processes), err)
	}
}

func TestProcessSnapshotLongCommandsAndErrorsRemainBounded(t *testing.T) {
	got, err := ReadProcessSnapshot(context.Background(), &fixedRunner{output: "99 user 0.1 0.2 00:00 " + strings.Repeat("x", 1100) + "\n"}, SortCPU)
	if err != nil || !got.CommandsTruncated || len(got.Processes) != 1 || len(got.Processes[0].Command) > 1027 {
		t.Fatalf("snapshot=%+v err=%v", got, err)
	}
	_, err = ReadProcessSnapshot(context.Background(), &fixedRunner{output: "permission denied\n", err: fmt.Errorf("exit status 1")}, SortCPU)
	if err == nil {
		t.Fatal("failed command became a successful snapshot")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("snapshot discarded the server's diagnostic: %v", err)
	}
}

func TestProcessSnapshotDoesNotReportAPartialRowAsTheFailure(t *testing.T) {
	runner := &fixedRunner{output: "99 root 0.1 0.2 00:00 service-1\n", err: fmt.Errorf("connection lost")}
	_, err := ReadProcessSnapshot(context.Background(), runner, SortCPU)
	if err == nil {
		t.Fatal("a failed read became a successful snapshot")
	}
	if strings.Contains(err.Error(), "service-1") {
		t.Fatalf("a partial process row became the error: %v", err)
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Fatalf("the snapshot swallowed the transport error: %v", err)
	}
}
