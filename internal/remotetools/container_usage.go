package remotetools

import (
	"context"
	"strconv"
	"strings"
)

const containerUsageCommand = "docker stats --no-stream --format '{{.ID}}|{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}|{{.NetIO}}|{{.BlockIO}}|{{.PIDs}}' 2>&1"

// ContainerUsage is one container's docker stats reading. Docker reports it for
// running containers only, so a stopped container has no usage entry.
type ContainerUsage struct {
	ID            string
	CPUPercent    float64
	MemoryUsed    string // as docker prints it: "12.5MiB"
	MemoryLimit   string // as docker prints it: "7.6GiB"
	MemoryPercent float64
	NetIO         string // "1.2kB / 648B", receive / send
	BlockIO       string // "0B / 0B", read / write
	PIDs          string // the container's process count
}

// ContainerUsages reads one usage sample of every running container. The list
// is separate from Containers because docker stats samples twice per container
// and fails on its own; a listing must survive without it.
func ContainerUsages(ctx context.Context, runner Runner) ([]ContainerUsage, error) {
	output, err := runner.RunCommand(ctx, containerUsageCommand, maxOutput)
	if err != nil {
		text := strings.TrimSpace(string(output))
		if missingContainerCLI(output) {
			return nil, ErrNoContainerRuntime
		}
		if text == "" {
			return nil, err
		}
		line, _, _ := strings.Cut(text, "\n")
		return nil, &Failure{Message: line, Err: err}
	}
	return ParseContainerUsages(output)
}

// ParseContainerUsages reads the usage command's output.
func ParseContainerUsages(output []byte) ([]ContainerUsage, error) {
	var list []ContainerUsage
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 7)
		if len(parts) != 7 || !containerIDPattern.MatchString(parts[0]) {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		cpu, ok := parseContainerPercent(parts[1])
		if !ok {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		used, limit, found := strings.Cut(parts[2], " / ")
		used, limit = strings.TrimSpace(used), strings.TrimSpace(limit)
		if !found || used == "" || limit == "" {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		memory, ok := parseContainerPercent(parts[3])
		if !ok || !containerIORates(parts[4]) || !containerIORates(parts[5]) {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		if _, err := strconv.Atoi(strings.TrimSpace(parts[6])); err != nil {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		list = append(list, ContainerUsage{
			ID: parts[0], CPUPercent: cpu,
			MemoryUsed: used, MemoryLimit: limit, MemoryPercent: memory,
			NetIO: strings.TrimSpace(parts[4]), BlockIO: strings.TrimSpace(parts[5]),
			PIDs: strings.TrimSpace(parts[6]),
		})
		if len(list) == maxItems {
			break
		}
	}
	return list, nil
}

// containerIORates checks docker's "received / sent" and "read / written"
// pairs, whose zeros it prints as "0B / 0B".
func containerIORates(value string) bool {
	first, second, found := strings.Cut(value, " / ")
	return found && strings.TrimSpace(first) != "" && strings.TrimSpace(second) != ""
}

// parseContainerPercent reads docker's "1.25%" figures, whose % is part of the
// display rather than the number.
func parseContainerPercent(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasSuffix(value, "%") {
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "%")), 64)
	return number, err == nil
}
