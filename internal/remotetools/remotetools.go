// Package remotetools lists and acts on a server's processes and Docker
// containers over an authenticated connection. Every command is fixed text;
// what varies is a number or a validated container name, never free text, so
// nothing the server prints or the user types can become shell syntax.
package remotetools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Runner executes one bounded command and returns its standard output. A
// non-zero exit comes back as an error together with whatever was printed.
type Runner interface {
	RunCommand(ctx context.Context, command string, maxOutputBytes int) ([]byte, error)
}

// Errors callers act on; test with errors.Is.
var (
	// ErrUnsupported means the server's ps does not understand the options used.
	ErrUnsupported = errors.New("remotetools: this server's ps is not supported")
	// ErrNoDocker means Docker is not installed on the server.
	ErrNoDocker = errors.New("remotetools: docker is not installed")
	// ErrNoShell means none of the supported interactive shells could start.
	ErrNoShell = errors.New("remotetools: no supported shell inside this container")
	// ErrMalformed means the output was not what the command prints.
	ErrMalformed = errors.New("remotetools: unexpected output")
)

// Failure is a command the server refused, with what it said.
type Failure struct {
	Message string
	Err     error
}

func (failure *Failure) Error() string { return failure.Message }
func (failure *Failure) Unwrap() error { return failure.Err }

const (
	maxOutput    = 1 << 20
	maxProcesses = 60
	maxCommand   = 240
	maxItems     = 200
)

// SortKey orders the process list.
type SortKey uint8

// The orders a process list may have.
const (
	SortCPU SortKey = iota
	SortMemory
)

// Process is one line of the process table.
type Process struct {
	PID     int
	User    string
	CPU     float64 // percent of one CPU
	Memory  float64 // percent of memory
	Elapsed string  // running time as ps prints it
	Command string
}

// Signal is a signal that may be sent to a process.
type Signal uint8

// The signals offered.
const (
	Terminate Signal = iota
	Kill
	Hangup
)

func (signal Signal) name() string {
	switch signal {
	case Kill:
		return "KILL"
	case Hangup:
		return "HUP"
	}
	return "TERM"
}

func processCommand(key SortKey) string {
	column := "3"
	if key == SortMemory {
		column = "4"
	}
	return "LC_ALL=C ps -eo pid=,user=,pcpu=,pmem=,etime=,args= 2>&1 | LC_ALL=C sort -k" + column + "," + column + "nr | head -n " +
		strconv.Itoa(maxProcesses+8) + " | cut -c1-" + strconv.Itoa(maxCommand+60)
}

// ownPipeline reports whether a command line is the listing's own: the shell
// that runs it and the stages of its pipe would otherwise head the list.
func ownPipeline(command string) bool {
	return strings.Contains(command, "pid=,user=,pcpu=") || strings.HasPrefix(command, "sort -k") ||
		strings.HasPrefix(command, "head -n ") || strings.HasPrefix(command, "cut -c1-")
}

// Processes lists the busiest processes, most first.
func Processes(ctx context.Context, runner Runner, key SortKey) ([]Process, error) {
	output, err := runner.RunCommand(ctx, processCommand(key), maxOutput)
	if unsupportedPSOptions(output) {
		output, err = runner.RunCommand(ctx, legacyProcessCommand, maxOutput)
		if err != nil {
			if unsupportedPSOptions(output) {
				line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
				return nil, &Failure{Message: line, Err: ErrUnsupported}
			}
			return nil, err
		}
		return parseLegacyProcesses(output, key)
	}
	if err != nil {
		if text := strings.TrimSpace(string(output)); text != "" {
			line, _, _ := strings.Cut(text, "\n")
			return nil, &Failure{Message: line, Err: err}
		}
		return nil, err
	}
	list, parseErr := ParseProcesses(output, key)
	if parseErr != nil {
		return nil, parseErr
	}
	return list, nil
}

// ParseProcesses reads the listing command's output.
func ParseProcesses(output []byte, key SortKey) ([]Process, error) {
	var list []Process
	var firstBad string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		process, ok := parseProcess(line)
		if !ok {
			if firstBad == "" {
				firstBad = line
			}
			continue
		}
		if ownPipeline(process.Command) {
			continue
		}
		list = append(list, process)
	}
	if len(list) == 0 {
		if firstBad != "" {
			return nil, &Failure{Message: firstBad, Err: ErrUnsupported}
		}
		return nil, ErrMalformed
	}
	sort.SliceStable(list, func(a, b int) bool {
		if key == SortMemory {
			return list[a].Memory > list[b].Memory
		}
		return list[a].CPU > list[b].CPU
	})
	if len(list) > maxProcesses {
		list = list[:maxProcesses]
	}
	return list, nil
}

func parseProcess(line string) (Process, bool) {
	rest := line
	var fields [5]string
	for index := range fields {
		rest = strings.TrimLeft(rest, " \t")
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			if index < len(fields)-1 || rest == "" {
				return Process{}, false
			}
			fields[index], rest = rest, ""
			continue
		}
		fields[index], rest = rest[:end], rest[end:]
	}
	pid, pidErr := strconv.Atoi(fields[0])
	cpu, cpuErr := strconv.ParseFloat(fields[2], 64)
	memory, memErr := strconv.ParseFloat(fields[3], 64)
	if pidErr != nil || cpuErr != nil || memErr != nil || pid <= 0 {
		return Process{}, false
	}
	command := strings.TrimSpace(rest)
	if len(command) > maxCommand {
		command = strings.ToValidUTF8(command[:maxCommand], "") + "…"
	}
	return Process{PID: pid, User: fields[1], CPU: cpu, Memory: memory, Elapsed: fields[4], Command: command}, true
}

// SendSignal signals a process. Process 1 and negative or zero numbers, which
// would reach a process group, are refused.
func SendSignal(ctx context.Context, runner Runner, pid int, signal Signal) error {
	if pid <= 1 {
		return fmt.Errorf("remotetools: refusing to signal process %d", pid)
	}
	return run(ctx, runner, "kill -"+signal.name()+" "+strconv.Itoa(pid)+" 2>&1")
}

func run(ctx context.Context, runner Runner, command string) error {
	output, err := runner.RunCommand(ctx, command, maxOutput)
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	if line, _, found := strings.Cut(message, "\n"); found {
		message = line
	}
	return &Failure{Message: message, Err: err}
}

// Container is one line of the container table.
type Container struct {
	ID     string
	Name   string
	Image  string
	State  string // running, exited, paused, ...
	Status string // the human line: "Up 2 hours"
}

// Running reports whether the container is up.
func (container Container) Running() bool { return container.State == "running" }

const containerCommand = "docker ps -a --format '{{.ID}}|{{.Names}}|{{.Image}}|{{.State}}|{{.Status}}' 2>&1"
const legacyContainerCommand = "docker ps -a --format '{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}' 2>&1"

// Containers lists every container, running ones first.
func Containers(ctx context.Context, runner Runner) ([]Container, error) {
	output, err := runner.RunCommand(ctx, containerCommand, maxOutput)
	if err != nil && strings.Contains(string(output), "<.State>") && strings.Contains(string(output), "can't evaluate field State") {
		output, err = runner.RunCommand(ctx, legacyContainerCommand, maxOutput)
		if err == nil {
			return parseLegacyContainers(output)
		}
	}
	if err != nil {
		text := strings.TrimSpace(string(output))
		if missingDockerCLI(output) {
			return nil, ErrNoDocker
		}
		if text == "" {
			return nil, err
		}
		line, _, _ := strings.Cut(text, "\n")
		return nil, &Failure{Message: line, Err: err}
	}
	return ParseContainers(output)
}

func stateFromStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.HasPrefix(status, "up"):
		if strings.Contains(status, "(paused)") {
			return "paused"
		}
		return "running"
	case strings.HasPrefix(status, "exited"):
		return "exited"
	case strings.HasPrefix(status, "created"):
		return "created"
	case strings.HasPrefix(status, "restarting"):
		return "restarting"
	case strings.HasPrefix(status, "dead"):
		return "dead"
	case strings.HasPrefix(status, "removing"):
		return "removing"
	default:
		return "unknown"
	}
}

func parseLegacyContainers(output []byte) ([]Container, error) {
	var list []Container
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 || !validName(parts[0]) {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		list = append(list, Container{ID: parts[0], Name: parts[1], Image: parts[2], State: stateFromStatus(parts[3]), Status: parts[3]})
		if len(list) == maxItems {
			break
		}
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Running() && !list[b].Running() })
	return list, nil
}

// ParseContainers reads the listing command's output.
func ParseContainers(output []byte) ([]Container, error) {
	var list []Container
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 || !validName(parts[0]) {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		list = append(list, Container{ID: parts[0], Name: parts[1], Image: parts[2], State: parts[3], Status: parts[4]})
		if len(list) == maxItems {
			break
		}
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Running() && !list[b].Running() })
	return list, nil
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

func validName(value string) bool { return namePattern.MatchString(value) }

// Action is something done to a container.
type Action uint8

// The actions offered.
const (
	Start Action = iota
	Stop
	Restart
	Remove
)

func (action Action) verb() string {
	switch action {
	case Stop:
		return "stop"
	case Restart:
		return "restart"
	case Remove:
		return "rm"
	}
	return "start"
}

// Do runs an action on one container, named by ID.
func Do(ctx context.Context, runner Runner, action Action, id string) error {
	if !validName(id) {
		return fmt.Errorf("remotetools: %q is not a container id", id)
	}
	return run(ctx, runner, "docker "+action.verb()+" "+id+" 2>&1")
}

// LogsCommand is the command line that follows a container's log, for the user
// to run in their own terminal.
func LogsCommand(id string) (string, bool) {
	if !validName(id) {
		return "", false
	}
	return "docker logs -f --tail 200 " + id, true
}

// ShellCommand probes a fixed set of shells before opening a PTY. A shell's
// later exit status belongs to the user and never triggers another candidate.
func ShellCommand(ctx context.Context, runner Runner, id string) (string, error) {
	if !containerIDPattern.MatchString(id) {
		return "", fmt.Errorf("remotetools: %q is not a container id", id)
	}
	if len(id) != 64 {
		output, err := runner.RunCommand(ctx, "docker inspect --format '{{.Id}}' "+id+" 2>&1", maxOutput)
		if err != nil {
			message, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
			if message != "" {
				return "", &Failure{Message: message, Err: err}
			}
			return "", err
		}
		id = strings.TrimSpace(string(output))
		if len(id) != 64 || !containerIDPattern.MatchString(id) {
			return "", ErrMalformed
		}
	}
	for _, shell := range [...]string{"sh", "bash", "ash"} {
		output, err := runner.RunCommand(ctx, "docker exec "+id+" "+shell+" -c : 2>&1", maxOutput)
		if err == nil {
			return "docker exec -it " + id + " " + shell, nil
		}
		message := strings.ToLower(string(output))
		if strings.Contains(message, "exec: \""+shell+"\"") &&
			(strings.Contains(message, "executable file not found") || strings.Contains(message, "no such file or directory")) {
			continue
		}
		if line, _, found := strings.Cut(strings.TrimSpace(string(output)), "\n"); found {
			return "", &Failure{Message: line, Err: err}
		} else if line != "" {
			return "", &Failure{Message: line, Err: err}
		}
		return "", err
	}
	return "", ErrNoShell
}
