//go:build darwin

package systemproxy

import "os/exec"

func Lookup() Settings {
	output, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return Settings{}
	}
	return darwinSettings(string(output))
}
