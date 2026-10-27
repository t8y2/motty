package remotetools

import "strings"

func missingDockerCLI(output []byte) bool {
	text := strings.ToLower(strings.TrimSpace(string(output)))
	return strings.Contains(text, "docker: command not found") || strings.Contains(text, "docker: not found") ||
		strings.Contains(text, "docker: no such file or directory") || strings.Contains(text, "exec: \"docker\": executable file not found")
}
