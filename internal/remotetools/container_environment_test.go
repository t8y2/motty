package remotetools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReadContainerEnvironmentUsesInspectAndPreservesValues(t *testing.T) {
	raw, err := json.Marshal([]string{"LANG=zh_CN.UTF-8", "CONNECTION=alpha=beta", "EMPTY=", "malformed"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &environmentRunner{output: raw}
	id := "0f1e2d3c4b5a"
	variables, err := ReadContainerEnvironment(context.Background(), runner, id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.command, "docker inspect --format '{{json .Config.Env}}' "+id) {
		t.Fatalf("command=%q", runner.command)
	}
	if len(variables) != 3 ||
		variables[0] != (ProcessEnvironmentVariable{Name: "LANG", Value: "zh_CN.UTF-8"}) ||
		variables[1] != (ProcessEnvironmentVariable{Name: "CONNECTION", Value: "alpha=beta"}) ||
		variables[2] != (ProcessEnvironmentVariable{Name: "EMPTY", Value: ""}) {
		t.Fatalf("variables=%+v", variables)
	}
}

func TestReadContainerEnvironmentRejectsUnsafeIDsBeforeRunningACommand(t *testing.T) {
	runner := &environmentRunner{}
	_, err := ReadContainerEnvironment(context.Background(), runner, "web;rm -rf /")
	if err == nil || runner.command != "" {
		t.Fatalf("err=%v command=%q", err, runner.command)
	}
}

func TestReadContainerEnvironmentClassifiesDockerAndMalformedOutput(t *testing.T) {
	missing := &environmentRunner{output: []byte("sh: docker: not found"), err: errors.New("exit status 127")}
	if _, err := ReadContainerEnvironment(context.Background(), missing, "0f1e2d3c4b5a"); !errors.Is(err, ErrNoDocker) {
		t.Fatalf("missing docker err=%v", err)
	}
	refused := &environmentRunner{output: []byte("permission denied\nsecond line"), err: errors.New("exit status 1")}
	_, err := ReadContainerEnvironment(context.Background(), refused, "0f1e2d3c4b5a")
	var failure *Failure
	if !errors.As(err, &failure) || failure.Message != "permission denied" {
		t.Fatalf("refused err=%v", err)
	}
	if _, err := ParseContainerEnvironment([]byte(`{"Env":["A=B"]}`)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("object output err=%v", err)
	}
}

func TestParseContainerEnvironmentReportsExcessVariables(t *testing.T) {
	entries := make([]string, maxProcessEnvironmentVariables+20)
	for index := range entries {
		entries[index] = "VALUE=" + strings.Repeat("x", 8)
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseContainerEnvironment(raw); !errors.Is(err, ErrProcessEnvironmentTooLarge) {
		t.Fatalf("err=%v, want ErrProcessEnvironmentTooLarge", err)
	}
}

func TestParseContainerEnvironmentReportsExcessBytes(t *testing.T) {
	raw, err := json.Marshal([]string{"VALUE=" + strings.Repeat("x", maxProcessEnvironmentBytes)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseContainerEnvironment(raw); !errors.Is(err, ErrProcessEnvironmentTooLarge) {
		t.Fatalf("err=%v, want ErrProcessEnvironmentTooLarge", err)
	}
}

func TestReadContainerEnvironmentDoesNotExposeAnOutputLimitPayload(t *testing.T) {
	output := bytes.Repeat([]byte("secret-environment-json"), maxContainerEnvironmentOutput/22+1)
	output = output[:maxContainerEnvironmentOutput]
	runner := &environmentRunner{output: output, err: errors.New("command output too large")}
	_, err := ReadContainerEnvironment(context.Background(), runner, "0f1e2d3c4b5a")
	if !errors.Is(err, ErrProcessEnvironmentTooLarge) {
		t.Fatalf("err=%v, want ErrProcessEnvironmentTooLarge", err)
	}
	if strings.Contains(err.Error(), "secret-environment-json") {
		t.Fatalf("error exposed the oversized payload: %q", err.Error())
	}
}
