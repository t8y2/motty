package remotetools

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

type environmentRunner struct {
	command string
	output  []byte
	err     error
}

func (runner *environmentRunner) RunCommand(_ context.Context, command string, _ int) ([]byte, error) {
	runner.command = command
	return runner.output, runner.err
}

func TestReadProcessEnvironmentUsesTheSelectedPIDAndPreservesValues(t *testing.T) {
	raw := []byte("LANG=zh_CN.UTF-8\x00CONNECTION=alpha=beta\x00EMPTY=\x00malformed\x00")
	identity := "linux:12345678-1234-1234-1234-123456789abc:432101"
	runner := &environmentRunner{output: []byte(
		"motty-env-before 12345678-1234-1234-1234-123456789abc 432101\n" +
			base64.StdEncoding.EncodeToString(raw) +
			"\nmotty-env-after 12345678-1234-1234-1234-123456789abc 432101\n",
	)}
	variables, err := ReadProcessEnvironment(context.Background(), runner, Process{PID: 4321, StartIdentity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.command, "pid=4321\n") ||
		!strings.Contains(runner.command, `[ -r "/proc/$pid/environ" ]`) ||
		len(variables) != 3 ||
		variables[0].Name != "LANG" || variables[0].Value != "zh_CN.UTF-8" ||
		variables[1].Name != "CONNECTION" || variables[1].Value != "alpha=beta" ||
		variables[2].Name != "EMPTY" || variables[2].Value != "" {
		t.Fatalf("command=%q variables=%+v", runner.command, variables)
	}
}

func TestReadProcessEnvironmentRejectsUnsupportedHosts(t *testing.T) {
	_, err := ReadProcessEnvironment(context.Background(), ForSystem(&environmentRunner{}, "windows"), Process{PID: 42})
	if !errors.Is(err, ErrProcessEnvironmentUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestReadProcessEnvironmentRejectsPIDReuse(t *testing.T) {
	runner := &environmentRunner{output: []byte(
		"motty-env-before 12345678-1234-1234-1234-123456789abc 100\n" +
			"TEFHPUxDT0FHRT14Cg==\n" +
			"motty-env-after 12345678-1234-1234-1234-123456789abc 200\n",
	)}
	_, err := ReadProcessEnvironment(context.Background(), runner,
		Process{PID: 42, StartIdentity: "linux:12345678-1234-1234-1234-123456789abc:100"})
	if !errors.Is(err, ErrProcessChanged) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseProcessEnvironmentRejectsMalformedBase64(t *testing.T) {
	if _, err := ParseProcessEnvironment([]byte("not base64")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err=%v", err)
	}
}
