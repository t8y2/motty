package remotetools

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// ForSystem applies the remote host's command syntax, independently of the
// client's OS. Unknown systems retain the existing portable Unix fallback.
// Only the package's fixed-text commands may pass through it: a Docker command
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
	if strings.HasPrefix(command, "docker ") {
		args := strings.TrimPrefix(command, "docker ")
		if r.system == "windows" {
			command = encodePowerShell(windowsDockerPrelude + "\n& $docker " + args + " | ForEach-Object { [Console]::WriteLine([string]$_) }; exit $LASTEXITCODE")
		} else if r.system == "darwin" {
			command = "if command -v docker >/dev/null 2>&1; then docker " + args +
				"; elif [ -x /usr/local/bin/docker ]; then /usr/local/bin/docker " + args +
				"; elif [ -x /opt/homebrew/bin/docker ]; then /opt/homebrew/bin/docker " + args +
				"; elif [ -x /Applications/Docker.app/Contents/Resources/bin/docker ]; then /Applications/Docker.app/Contents/Resources/bin/docker " + args +
				// The marker must reach standard output: a command runner
				// returns stdout only, so a stderr marker would be lost and the
				// missing CLI would surface as a bare exit status instead.
				"; else printf 'motty-docker-missing\\n'; exit 127; fi"
		}
	}
	return r.Runner.RunCommand(ctx, command, limit)
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

const windowsDockerPrelude = `$ErrorActionPreference='Continue'
$ProgressPreference='SilentlyContinue'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$OutputEncoding=[Console]::OutputEncoding
$docker=$null
$found=Get-Command docker.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($found) { $docker=$found.Source }
if (!$docker) {
  foreach ($root in @($env:ProgramW6432,$env:ProgramFiles)) {
    if ($root) { $path=Join-Path $root 'Docker\Docker\resources\bin\docker.exe'; if (Test-Path -LiteralPath $path -PathType Leaf) { $docker=$path; break } }
  }
}
if (!$docker) { [Console]::WriteLine('motty-docker-missing'); exit 127 }
`
