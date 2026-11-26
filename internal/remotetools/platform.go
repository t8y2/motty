package remotetools

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// ForSystem applies the remote host's command syntax, independently of the
// client's OS. Windows gets PowerShell; every other system is a POSIX shell,
// whatever distribution the host names, so Docker and Podman are both reached.
// Only the package's fixed-text commands may pass through it: a container command
// is spliced into a PowerShell script and a POSIX shell chain, so a caller that
// put user text in it would turn that text into remote shell syntax.
func ForSystem(runner Runner, system string) Runner {
	return &systemRunner{Runner: runner, system: system}
}

type systemRunner struct {
	Runner
	system string
}

func runnerSystem(runner Runner) string {
	if r, ok := runner.(*systemRunner); ok {
		return r.system
	}
	return ""
}

func (r *systemRunner) RunCommand(ctx context.Context, command string, limit int) ([]byte, error) {
	args, isEngine := strings.CutPrefix(command, "docker ")
	if command == runtimeProbeCommand {
		args, isEngine = "", true
	}
	if isEngine {
		probe := command == runtimeProbeCommand
		switch r.system {
		case "windows":
			invoke := "& $docker " + args + " | ForEach-Object { [Console]::WriteLine([string]$_) }; exit $LASTEXITCODE"
			if probe {
				invoke = "[Console]::WriteLine($engine); exit 0"
			}
			command = encodePowerShell(windowsContainerPrelude + "\n" + invoke)
		default:
			command = unixContainerCommand(r.system, args, probe)
		}
	}
	return r.Runner.RunCommand(ctx, command, limit)
}

// unixContainerCommand runs args with the first engine the server has: Docker,
// then Podman, each by name on PATH and then at the places a login shell over
// SSH often lacks (Homebrew and the Docker Desktop bundle on macOS). A probe
// prints the engine's name instead of running it.
func unixContainerCommand(system, args string, probe bool) string {
	type engine struct {
		name  string
		paths []string
	}
	engines := []engine{{"docker", []string{"/usr/local/bin/docker"}}, {"podman", []string{"/usr/local/bin/podman"}}}
	if system == "darwin" {
		engines[0].paths = append(engines[0].paths, "/opt/homebrew/bin/docker", "/Applications/Docker.app/Contents/Resources/bin/docker")
		engines[1].paths = append(engines[1].paths, "/opt/homebrew/bin/podman")
	}
	var chain strings.Builder
	keyword := "if"
	emit := func(condition, program, name string) {
		chain.WriteString(keyword + " " + condition + "; then ")
		if probe {
			chain.WriteString("printf '" + name + "\\n'; ")
		} else {
			chain.WriteString(program + " " + args + "; ")
		}
		keyword = "elif"
	}
	for _, e := range engines {
		emit("command -v "+e.name+" >/dev/null 2>&1", e.name, e.name)
		for _, path := range e.paths {
			emit("[ -x "+path+" ]", path, e.name)
		}
	}
	// The marker must reach standard output: a command runner returns stdout
	// only, so a stderr marker would be lost and the missing CLI would surface
	// as a bare exit status instead.
	chain.WriteString("else printf 'motty-docker-missing\\n'; exit 127; fi")
	return chain.String()
}

// EncodedCommand survives both cmd.exe and PowerShell OpenSSH default shells.
// Callers pass only fixed scripts and already validated Docker arguments.
func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(data[i*2:], u)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(data)
}

const windowsContainerPrelude = `$ErrorActionPreference='Continue'
$ProgressPreference='SilentlyContinue'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$OutputEncoding=[Console]::OutputEncoding
$docker=$null
$engine=$null
foreach ($name in @('docker','podman')) {
  $found=Get-Command ($name+'.exe') -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($found) { $docker=$found.Source; $engine=$name; break }
}
if (!$docker) {
  foreach ($root in @($env:ProgramW6432,$env:ProgramFiles)) {
    if ($root) {
      $path=Join-Path $root 'Docker\Docker\resources\bin\docker.exe'
      if (Test-Path -LiteralPath $path -PathType Leaf) { $docker=$path; $engine='docker'; break }
      $path=Join-Path $root 'RedHat\Podman\podman.exe'
      if (Test-Path -LiteralPath $path -PathType Leaf) { $docker=$path; $engine='podman'; break }
    }
  }
}
if (!$docker) { [Console]::WriteLine('motty-docker-missing'); exit 127 }
`
