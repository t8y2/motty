package remotetools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseContainerUsagesReadsDockerStatsFigures(t *testing.T) {
	output := "" +
		"0f1e2d3c4b5a|1.25%|12.5MiB / 7.6GiB|0.16%|1.2kB / 648B|0B / 0B|5\n" +
		"c3c3c3c3c3c3|0.00%|0B / 7.6GiB|0.00%|0B / 0B|0B / 0B|1\n"
	usages, err := ParseContainerUsages([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	if len(usages) != 2 {
		t.Fatalf("got %d usages, want 2", len(usages))
	}
	first := usages[0]
	if first.ID != "0f1e2d3c4b5a" || first.CPUPercent != 1.25 ||
		first.MemoryUsed != "12.5MiB" || first.MemoryLimit != "7.6GiB" || first.MemoryPercent != 0.16 ||
		first.NetIO != "1.2kB / 648B" || first.BlockIO != "0B / 0B" || first.PIDs != "5" {
		t.Fatalf("usage = %+v", first)
	}
	if usages[1].CPUPercent != 0 || usages[1].MemoryUsed != "0B" {
		t.Fatalf("zero usage = %+v", usages[1])
	}
}

func TestParseContainerUsagesRejectsMalformedLines(t *testing.T) {
	for _, line := range []string{
		"0f1e2d3c4b5a|1.25%|12.5MiB / 7.6GiB|0.16%|1.2kB / 648B|0B / 0B",      // no PIDs
		"0f1e2d3c4b5a|1.25|12.5MiB / 7.6GiB|0.16%|1.2kB / 648B|0B / 0B|5",     // CPU without its percent sign
		"0f1e2d3c4b5a|1.25%|12.5MiB|0.16%|1.2kB / 648B|0B / 0B|5",             // usage without a limit
		"0f1e2d3c4b5a|1.25%|12.5MiB / 7.6GiB|0.16%|1.2kB|0B / 0B|5",           // network I/O without its pair
		"0f1e2d3c4b5a|1.25%|12.5MiB / 7.6GiB|0.16%|1.2kB / 648B|0B / 0B|many", // PIDs not a number
		"web|1.25%|12.5MiB / 7.6GiB|0.16%|1.2kB / 648B|0B / 0B|5",             // not a container ID
	} {
		if _, err := ParseContainerUsages([]byte(line + "\n")); !errors.Is(err, ErrMalformed) {
			t.Errorf("line %q: err = %v, want ErrMalformed", line, err)
		}
	}
}

func TestContainerUsagesReportsAMissingDockerCLI(t *testing.T) {
	runner := &fixedRunner{output: "sh: 1: docker: not found", err: errors.New("exit status 127")}
	if _, err := ContainerUsages(context.Background(), runner); !errors.Is(err, ErrNoContainerRuntime) {
		t.Fatalf("err = %v, want ErrNoContainerRuntime", err)
	}
	if !strings.HasPrefix(runner.asked, "docker stats --no-stream ") {
		t.Fatalf("asked %q", runner.asked)
	}
}
