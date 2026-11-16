package cli

import (
	"slices"
	"testing"
)

// A CLI installed by Homebrew runs through `#!/usr/bin/env node`, so the PATH a
// child process receives must be the user's login PATH, not launchd's minimal
// one. The override still wins, and an empty login PATH keeps the inherited one.
func TestChildEnvUsesLoginPATHAndKeepsOverrides(t *testing.T) {
	base := []string{"PATH=/usr/bin:/bin", "HOME=/home/u"}
	got := childEnv(base, []string{"PATH=/custom/bin"}, "/opt/homebrew/bin:/usr/bin")
	if !slices.Contains(got, "HOME=/home/u") {
		t.Fatal("inherited environment dropped")
	}
	if !slices.Contains(got, "PATH=/custom/bin") || slices.Contains(got, "PATH=/opt/homebrew/bin:/usr/bin") {
		t.Fatalf("explicit PATH override did not win: %v", got)
	}
	got = childEnv(base, nil, "/opt/homebrew/bin:/usr/bin")
	if !slices.Contains(got, "PATH=/opt/homebrew/bin:/usr/bin") || slices.Contains(got, "PATH=/usr/bin:/bin") {
		t.Fatalf("login PATH not applied: %v", got)
	}
	got = childEnv(base, nil, "")
	if !slices.Contains(got, "PATH=/usr/bin:/bin") {
		t.Fatalf("empty login PATH replaced the inherited one: %v", got)
	}
}
