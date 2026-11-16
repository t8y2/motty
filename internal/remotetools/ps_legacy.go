package remotetools

import (
	"sort"
	"strconv"
	"strings"
)

const legacyProcessCommand = "LC_ALL=C ps aux 2>&1"

func unsupportedPSOptions(output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "ps:") && (strings.Contains(text, "invalid option") || strings.Contains(text, "unrecognized option") ||
		strings.Contains(text, "unknown option") || strings.Contains(text, "illegal option"))
}

// ps aux keeps real CPU and memory but has no portable elapsed-time field.
func parseLegacyProcesses(output []byte, key SortKey) ([]Process, error) {
	return parseLegacyProcessRows(output, key, maxProcesses, maxCommand)
}

func parseLegacyProcessRows(output []byte, key SortKey, rowLimit, commandLimit int) ([]Process, error) {
	var list []Process
	var firstBad string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "USER ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			if firstBad == "" {
				firstBad = line
			}
			continue
		}
		pid, pidErr := strconv.Atoi(fields[1])
		cpu, cpuErr := strconv.ParseFloat(fields[2], 64)
		memory, memErr := strconv.ParseFloat(fields[3], 64)
		if pidErr != nil || cpuErr != nil || memErr != nil || pid <= 0 {
			if firstBad == "" {
				firstBad = line
			}
			continue
		}
		command := strings.Join(fields[10:], " ")
		if strings.Contains(command, legacyProcessCommand) || ownPipeline(command) {
			continue
		}
		if len(command) > commandLimit {
			command = strings.ToValidUTF8(command[:commandLimit], "") + "…"
		}
		list = append(list, Process{PID: pid, User: fields[0], CPU: cpu, Memory: memory, Command: command})
	}
	if len(list) == 0 {
		if firstBad != "" {
			return nil, &Failure{Message: firstBad, Err: ErrUnsupported}
		}
		return nil, ErrUnsupported
	}
	sort.SliceStable(list, func(a, b int) bool {
		if key == SortMemory {
			return list[a].Memory > list[b].Memory
		}
		return list[a].CPU > list[b].CPU
	})
	if len(list) > rowLimit {
		list = list[:rowLimit]
	}
	return list, nil
}
