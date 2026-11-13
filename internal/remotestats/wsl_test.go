package remotestats

import (
	"context"
	"slices"
	"testing"
)

func TestWSLCommandsRunInsideTheDistribution(t *testing.T) {
	got := wslArgs("Ubuntu-22.04", "cat /proc/stat")
	want := []string{"-d", "Ubuntu-22.04", "--exec", "/bin/sh", "-c", "cat /proc/stat"}
	if !slices.Equal(got, want) {
		t.Fatalf("wsl.exe arguments = %q, want %q", got, want)
	}
}

func TestAWSLRunnerWithoutItsProgramIsRefused(t *testing.T) {
	for name, runner := range map[string]WSL{
		"no executable":   {Distro: "Ubuntu"},
		"no distribution": {Exe: "wsl.exe"},
	} {
		if err := runner.StreamCommand(context.Background(), "echo hi", 64, func([]byte) bool { return true }); err == nil {
			t.Errorf("%s was streamed rather than refused", name)
		}
		if _, err := runner.RunCommand(context.Background(), "echo hi", 64); err == nil {
			t.Errorf("%s was run rather than refused", name)
		}
	}
}
