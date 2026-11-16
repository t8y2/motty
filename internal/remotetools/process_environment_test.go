package remotetools

import (
	"bytes"
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

func TestReadProcessEnvironmentClassifiesExitBeforeEnvironmentRead(t *testing.T) {
	runner := &environmentRunner{
		output: []byte("motty-env-before 12345678-1234-1234-1234-123456789abc 100\n"),
		err:    errors.New("exit status 1"),
	}
	_, err := ReadProcessEnvironment(context.Background(), runner,
		Process{PID: 42, StartIdentity: "linux:12345678-1234-1234-1234-123456789abc:100"})
	if !errors.Is(err, ErrProcessChanged) {
		t.Fatalf("err=%v, want ErrProcessChanged instead of leaking the command marker", err)
	}
	if strings.Contains(err.Error(), "motty-env-before") {
		t.Fatalf("internal command marker leaked to the user: %q", err)
	}
}

func TestReadProcessEnvironmentClassifiesUnreadableProcEntry(t *testing.T) {
	runner := &environmentRunner{
		output: []byte(
			"motty-env-before 12345678-1234-1234-1234-123456789abc 100\n" +
				"motty-env-error unreadable\n",
		),
		err: errors.New("exit status 1"),
	}
	_, err := ReadProcessEnvironment(context.Background(), runner,
		Process{PID: 42, StartIdentity: "linux:12345678-1234-1234-1234-123456789abc:100"})
	if !errors.Is(err, ErrProcessEnvironmentUnreadable) {
		t.Fatalf("err=%v, want ErrProcessEnvironmentUnreadable", err)
	}
}

func TestReadProcessEnvironmentDoesNotExposeAnOutputLimitPayload(t *testing.T) {
	output := bytes.Repeat([]byte("secret-process-environment"), maxProcessEnvironmentOutput/25+1)
	output = output[:maxProcessEnvironmentOutput]
	runner := &environmentRunner{output: output, err: errors.New("command output too large")}
	_, err := ReadProcessEnvironment(context.Background(), runner,
		Process{PID: 42, StartIdentity: "linux:12345678-1234-1234-1234-123456789abc:100"})
	if !errors.Is(err, ErrProcessEnvironmentTooLarge) {
		t.Fatalf("err=%v, want ErrProcessEnvironmentTooLarge", err)
	}
	if strings.Contains(err.Error(), "secret-process-environment") {
		t.Fatalf("error exposed the oversized payload: %q", err.Error())
	}
}

func TestParseProcessEnvironmentRejectsMalformedBase64(t *testing.T) {
	if _, err := ParseProcessEnvironment([]byte("not base64")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseProcessEnvironmentRejectsThePartialByteSentinel(t *testing.T) {
	raw := make([]byte, maxProcessEnvironmentBytes+1)
	copy(raw, []byte("A="))
	_, err := ParseProcessEnvironment([]byte(base64.StdEncoding.EncodeToString(raw)))
	if !errors.Is(err, ErrProcessEnvironmentTooLarge) {
		t.Fatalf("err=%v, want ErrProcessEnvironmentTooLarge", err)
	}
}

func TestParseProcessEnvironmentRejectsExcessVariables(t *testing.T) {
	var raw strings.Builder
	for index := 0; index < maxProcessEnvironmentVariables+1; index++ {
		raw.WriteString("V=")
		raw.WriteByte(0)
	}
	_, err := ParseProcessEnvironment([]byte(base64.StdEncoding.EncodeToString([]byte(raw.String()))))
	if !errors.Is(err, ErrProcessEnvironmentTooLarge) {
		t.Fatalf("err=%v, want ErrProcessEnvironmentTooLarge", err)
	}
}

func TestReadProcessCommandReadsTheFullArgvAndChecksIdentity(t *testing.T) {
	const identity = "linux:12345678-1234-1234-1234-123456789abc:432101"
	raw := []byte("python3\x00/srv/application with spaces.py\x00--serve\x00")
	runner := &environmentRunner{output: []byte(
		"motty-command-before 12345678-1234-1234-1234-123456789abc 432101\n" +
			base64.StdEncoding.EncodeToString(raw) +
			"\nmotty-command-after 12345678-1234-1234-1234-123456789abc 432101\n",
	)}
	got, err := ReadProcessCommand(context.Background(), runner, Process{PID: 4321, StartIdentity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if got != "python3 /srv/application with spaces.py --serve" {
		t.Fatalf("command=%q", got)
	}
	if !strings.Contains(runner.command, "pid=4321\n") ||
		!strings.Contains(runner.command, `head -c 262145 < "/proc/$pid/cmdline"`) {
		t.Fatalf("remote command=%q", runner.command)
	}
}

func TestReadProcessCommandRejectsTruncationAndPIDReuse(t *testing.T) {
	const identity = "linux:12345678-1234-1234-1234-123456789abc:432101"
	tooLarge := make([]byte, maxProcessCommandBytes+1)
	runner := &environmentRunner{output: []byte(
		"motty-command-before 12345678-1234-1234-1234-123456789abc 432101\n" +
			base64.StdEncoding.EncodeToString(tooLarge) +
			"\nmotty-command-after 12345678-1234-1234-1234-123456789abc 432101\n",
	)}
	if _, err := ReadProcessCommand(context.Background(), runner,
		Process{PID: 4321, StartIdentity: identity}); !errors.Is(err, ErrProcessCommandTooLarge) {
		t.Fatalf("truncated command err=%v", err)
	}

	runner.output = []byte(
		"motty-command-before 12345678-1234-1234-1234-123456789abc 100\n" +
			"c2xlZXAgMTAK\n" +
			"motty-command-after 12345678-1234-1234-1234-123456789abc 200\n",
	)
	if _, err := ReadProcessCommand(context.Background(), runner,
		Process{PID: 4321, StartIdentity: identity}); !errors.Is(err, ErrProcessChanged) {
		t.Fatalf("reused PID err=%v", err)
	}
}

func TestReadProcessCommandDoesNotExposeAnOutputLimitPayload(t *testing.T) {
	output := bytes.Repeat([]byte("secret-process-command"), maxProcessCommandOutput/21+1)
	output = output[:maxProcessCommandOutput]
	runner := &environmentRunner{output: output, err: errors.New("command output too large")}
	_, err := ReadProcessCommand(context.Background(), runner,
		Process{PID: 4321, StartIdentity: "linux:12345678-1234-1234-1234-123456789abc:432101"})
	if !errors.Is(err, ErrProcessCommandTooLarge) {
		t.Fatalf("err=%v, want ErrProcessCommandTooLarge", err)
	}
	if strings.Contains(err.Error(), "secret-process-command") {
		t.Fatalf("error exposed the oversized payload: %q", err.Error())
	}
}

func TestReadProcessCommandFallsBackOnlyForUnsupportedHosts(t *testing.T) {
	_, err := ReadProcessCommand(
		context.Background(),
		ForSystem(&environmentRunner{}, "windows"),
		Process{PID: 42, StartIdentity: "linux:unused"},
	)
	if !errors.Is(err, ErrProcessCommandUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestReadProcessCommandFallsBackForAnEmptyCommandLine(t *testing.T) {
	const identity = "linux:12345678-1234-1234-1234-123456789abc:432101"
	// A kernel thread's /proc/<pid>/cmdline is readable but empty.
	runner := &environmentRunner{output: []byte(
		"motty-command-before 12345678-1234-1234-1234-123456789abc 432101\n" +
			base64.StdEncoding.EncodeToString([]byte("\x00")) +
			"\nmotty-command-after 12345678-1234-1234-1234-123456789abc 432101\n",
	)}
	_, err := ReadProcessCommand(context.Background(), runner, Process{PID: 4321, StartIdentity: identity})
	if !errors.Is(err, ErrProcessCommandUnsupported) {
		t.Fatalf("err=%v, want ErrProcessCommandUnsupported", err)
	}
}
