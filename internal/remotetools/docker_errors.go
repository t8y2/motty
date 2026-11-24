package remotetools

import "strings"

// missingContainerCLI reports that neither engine could be started. A server
// with only one of them prints that engine's own "not found" line, so both
// names are recognized.
func missingContainerCLI(output []byte) bool {
	text := strings.ToLower(strings.TrimSpace(string(output)))
	if strings.Contains(text, "motty-docker-missing") {
		return true
	}
	for _, name := range [...]string{"docker", "podman"} {
		if strings.Contains(text, name+": command not found") || strings.Contains(text, name+": not found") ||
			strings.Contains(text, name+": no such file or directory") || strings.Contains(text, "exec: \""+name+"\": executable file not found") {
			return true
		}
	}
	return false
}
