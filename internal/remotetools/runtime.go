package remotetools

import (
	"context"
	"strings"
)

// Runtime is the container engine CLI a server provides. Docker and Podman
// accept the same verbs and templates for everything this package runs, so
// commands are written once with the "docker" verb and the runner substitutes
// the engine the server has.
type Runtime uint8

// The engines, in the order a server is probed: Docker wins when both exist.
const (
	RuntimeDocker Runtime = iota
	RuntimePodman
)

// CLI is the program name the user types in a terminal.
func (runtime Runtime) CLI() string {
	if runtime == RuntimePodman {
		return "podman"
	}
	return "docker"
}

// runtimeProbeCommand is the fixed command the system runner answers with the
// name of the engine it would run, or the missing marker.
const runtimeProbeCommand = "motty-container-runtime"

// DetectRuntime reports which engine the server runs commands with. A runner
// that does not know the server's system has only the portable Docker command,
// so it answers Docker.
func DetectRuntime(ctx context.Context, runner Runner) (Runtime, error) {
	if _, ok := runner.(*systemRunner); !ok {
		return RuntimeDocker, nil
	}
	output, err := runner.RunCommand(ctx, runtimeProbeCommand, 256)
	text := strings.TrimSpace(string(output))
	switch {
	case err == nil && text == "podman":
		return RuntimePodman, nil
	case err == nil && text == "docker":
		return RuntimeDocker, nil
	case missingContainerCLI(output):
		return RuntimeDocker, ErrNoContainerRuntime
	}
	if line, _, _ := strings.Cut(text, "\n"); line != "" && err != nil {
		return RuntimeDocker, &Failure{Message: line, Err: err}
	}
	if err != nil {
		return RuntimeDocker, err
	}
	return RuntimeDocker, ErrMalformed
}
