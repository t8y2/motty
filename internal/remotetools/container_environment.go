package remotetools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const maxContainerEnvironmentOutput = 1 << 20

// ReadContainerEnvironment reads the environment configured for a container.
// Docker stores the values in Config.Env, so stopped containers remain
// inspectable and no shell runs inside the container.
func ReadContainerEnvironment(ctx context.Context, runner Runner, id string) ([]ProcessEnvironmentVariable, error) {
	if !containerIDPattern.MatchString(id) {
		return nil, fmt.Errorf("remotetools: %q is not a container id", id)
	}
	output, err := runner.RunCommand(ctx,
		"docker inspect --format '{{json .Config.Env}}' "+id+" 2>&1",
		maxContainerEnvironmentOutput,
	)
	if err != nil {
		if missingDockerCLI(output) {
			return nil, ErrNoDocker
		}
		// RunCommand returns the retained prefix together with its error when
		// the peer wrote beyond the limit. Never copy that potentially large
		// JSON payload into an error that the UI paints.
		if len(output) >= maxContainerEnvironmentOutput {
			return nil, ErrProcessEnvironmentTooLarge
		}
		message := strings.TrimSpace(string(output))
		if line, _, found := strings.Cut(message, "\n"); found {
			message = line
		}
		if message != "" {
			return nil, &Failure{Message: message, Err: err}
		}
		return nil, err
	}
	return ParseContainerEnvironment(output)
}

// ParseContainerEnvironment bounds and parses Docker's JSON array of NAME=value
// strings. An invalid entry is omitted; malformed JSON or a non-array is
// reported as ErrMalformed.
func ParseContainerEnvironment(output []byte) ([]ProcessEnvironmentVariable, error) {
	if len(output) > maxContainerEnvironmentOutput {
		return nil, ErrProcessEnvironmentTooLarge
	}
	var entries []string
	if err := json.Unmarshal(output, &entries); err != nil {
		return nil, ErrMalformed
	}
	result := make([]ProcessEnvironmentVariable, 0, min(maxProcessEnvironmentVariables, len(entries)))
	total := 0
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		size := len(name) + len(value)
		if total+size > maxProcessEnvironmentBytes || len(result) >= maxProcessEnvironmentVariables {
			return nil, ErrProcessEnvironmentTooLarge
		}
		total += size
		result = append(result, ProcessEnvironmentVariable{
			Name:  strings.ToValidUTF8(name, "�"),
			Value: strings.ToValidUTF8(value, "�"),
		})
	}
	return result, nil
}
