package remotetools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ErrNoCompose means Docker is there but has no compose plugin.
var ErrNoCompose = fmt.Errorf("remotetools: docker compose is not installed")
var ErrComposeListUnsupported = fmt.Errorf("remotetools: this docker compose cannot list projects as JSON")

// ComposeProject is one project `docker compose ls` reports.
type ComposeProject struct {
	Name        string
	Status      string // as compose prints it: "running(2), exited(1)"
	ConfigFiles string // comma separated paths
}

const composeListCommand = "docker compose ls -a --format json 2>&1"

// Compose lists every compose project, including stopped ones.
func Compose(ctx context.Context, runner Runner) ([]ComposeProject, error) {
	output, err := runner.RunCommand(ctx, composeListCommand, maxOutput)
	if err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(text, "'compose' is not a docker command") || strings.Contains(text, "unknown command \"compose\"") {
			return nil, ErrNoCompose
		}
		if strings.Contains(text, "unknown shorthand flag") || strings.Contains(text, "unknown flag: --format") ||
			strings.Contains(text, "unknown command \"ls\"") {
			return nil, ErrComposeListUnsupported
		}
		if missingDockerCLI(output) {
			return nil, ErrNoDocker
		}
		if text == "" {
			return nil, err
		}
		line, _, _ := strings.Cut(text, "\n")
		return nil, &Failure{Message: line, Err: err}
	}
	return ParseCompose(output)
}

// ParseCompose reads the JSON array the listing prints.
func ParseCompose(output []byte) ([]ComposeProject, error) {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return nil, nil
	}
	if text == "null" {
		return nil, &Failure{Message: text, Err: ErrMalformed}
	}
	var raw []struct {
		Name        string `json:"Name"`
		Status      string `json:"Status"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		line, _, _ := strings.Cut(text, "\n")
		return nil, &Failure{Message: line, Err: ErrMalformed}
	}
	list := make([]ComposeProject, 0, len(raw))
	for _, project := range raw {
		list = append(list, ComposeProject{Name: project.Name, Status: project.Status, ConfigFiles: project.ConfigFiles})
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Name < list[b].Name })
	if len(list) > maxItems {
		list = list[:maxItems]
	}
	return list, nil
}

// Compose normalizes project names to this alphabet.
var projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// ComposeAction is something done to a compose project.
type ComposeAction uint8

// The actions offered.
const (
	ComposeStart ComposeAction = iota
	ComposeStop
	ComposeRestart
	ComposeDown
)

func (action ComposeAction) verb() string {
	switch action {
	case ComposeStop:
		return "stop"
	case ComposeRestart:
		return "restart"
	case ComposeDown:
		return "down"
	}
	return "start"
}

// DoCompose runs an action on one project, named by its project name.
func DoCompose(ctx context.Context, runner Runner, action ComposeAction, name string) error {
	if !projectPattern.MatchString(name) {
		return fmt.Errorf("remotetools: %q is not a compose project name", name)
	}
	return run(ctx, runner, "docker compose -p "+name+" "+action.verb()+" 2>&1")
}

// Niceness limits, as nice(1) defines them.
const (
	MinNice = -20
	MaxNice = 19
)

// Renice sets a process's niceness to nice. Lowering it below the current value
// needs root; the server's refusal comes back as the error. The command is
// "renice N -p PID" without -n: that form is an absolute value on Linux and on
// BSD and macOS alike, while -n means an absolute value on Linux but an
// increment on the BSDs.
func Renice(ctx context.Context, runner Runner, pid, nice int) error {
	if pid <= 1 {
		return fmt.Errorf("remotetools: refusing to renice process %d", pid)
	}
	if nice < MinNice || nice > MaxNice {
		return fmt.Errorf("remotetools: niceness %d is out of range", nice)
	}
	return run(ctx, runner, "renice "+strconv.Itoa(nice)+" -p "+strconv.Itoa(pid)+" 2>&1")
}
