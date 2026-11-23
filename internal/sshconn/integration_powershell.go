package sshconn

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// PowerShellActivation is the line that installs directory and prompt reporting
// in a PowerShell that is already running, ending in the Enter that runs it. It
// is the PowerShell counterpart of what UploadIntegration returns for Bash and
// Zsh, with the same ready marker and echo tag (see injection in the workbench),
// but it needs no file on the server: a script file is subject to the server's
// execution policy (Windows client editions default to Restricted), while a line
// typed into the shell is not. The line is therefore the whole script, on one
// line, in ASCII.
//
// The shell's prompt function is wrapped, not replaced, so the user's own
// prompt (and one a profile installed later) still draws. Each prompt reports
// OSC 133 A and B bound to token, the folder as OSC 7 and the home folder as the
// token-bound private report. A Windows path "C:\a b" is reported as the path an
// SFTP server of that host names it, "/C:/a%20b", which is the form the file
// panel already follows; a folder that has no such name (a UNC share, a
// non-file-system location) is not reported at all. PowerShell on a POSIX host
// reports its path as it is. Command starts are not reported: PowerShell has no
// hook that runs before every command line, so the shell never counts as idle
// for typing a command into it, and the file panel does not drive the terminal.
//
// Unlike Bash, PowerShell's line editor records the line as it is submitted and
// offers no way to take that entry back, so it stays in the shell's history. A
// shell in constrained language mode cannot redefine its prompt; it is left as
// it was and only the ready marker is printed.
//
// The echo of the line is hidden from the screen, but the console behind the
// pty is not: it draws the long line over several rows, and describes every
// later redraw to the terminal with absolute rows. Its cursor therefore ends up
// below where the screen's is. After the ready marker the script puts both back
// on row, the screen row the prompt was on when the line was typed, and erases
// from there: the new prompt is then drawn where the old one was, and the two
// agree on every row after it. A negative row leaves them as they are.
//
// directory is the folder the shell starts in when it is an absolute path of the
// server's SFTP form; a folder that does not exist leaves the shell where it is.
func PowerShellActivation(token, directory string, row int) string {
	if !validIntegrationToken(token) || token == "" {
		return ""
	}
	start := ""
	if folder, ok := powerShellStartFolder(directory); ok {
		// The folder travels Base64-encoded: no quoting of the path, however
		// unusual its name, can end the string early or run as code.
		encoded := base64.StdEncoding.EncodeToString([]byte(folder))
		start = "Set-Location -LiteralPath ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" + encoded + "'))) -ErrorAction SilentlyContinue; "
	}
	script := strings.ReplaceAll(powerShellIntegration, "__MOTTY_START__", start)
	rewind := ""
	if row >= 0 && row < 10000 {
		rewind = ", [char]27, '[" + strconv.Itoa(row+1) + ";1H', [char]27, '[J'"
	}
	script = strings.ReplaceAll(script, "__MOTTY_REWIND__", rewind)
	script = strings.ReplaceAll(script, "__MOTTY_TOKEN__", token)
	return script + "\r"
}

// powerShellStartFolder turns an SFTP-form absolute path into the one PowerShell
// takes: a Windows drive path loses the leading slash its server names it with.
func powerShellStartFolder(directory string) (string, bool) {
	if !validStartDirectory(directory) {
		return "", false
	}
	if len(directory) >= 3 && directory[2] == ':' && isASCIILetter(directory[1]) && (len(directory) == 3 || directory[3] == '/') {
		directory = directory[1:]
		if len(directory) == 2 {
			directory += "/"
		}
	}
	return directory, true
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// powerShellIntegration is the activation script. The comment first on the line
// is the echo tag the terminal matches (see IntegrationEchoTag); the ready
// marker is built at run time, so the echoed line never contains it.
const powerShellIntegration = `<# : motty-__MOTTY_TOKEN__ #> ` +
	`if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage') { ` +
	`__MOTTY_START__` +
	`$global:__MottyUri = { param($p) $u = $null; ` +
	`if ($p -match '^[A-Za-z]:[\\/]') { $u = '/' + $p.Replace('\', '/') } elseif ($p.StartsWith('/')) { $u = $p }; ` +
	`if ($u) { [Uri]::EscapeDataString($u).Replace('%2F', '/').Replace('%3A', ':') } }; ` +
	`$global:__MottyOriginalPrompt = $function:prompt; ` +
	`function global:prompt { ` +
	`$failed = -not $global:?; ` +
	`if ($failed) { Write-Error 'motty-prompt-status' -ErrorAction Ignore }; ` +
	`$original = $global:__MottyOriginalPrompt.Invoke(); ` +
	`$esc = [char]27; $bel = [char]7; ` +
	`$report = "$esc]133;A;motty=__MOTTY_TOKEN__$bel"; ` +
	`$home_ = & $global:__MottyUri $HOME; ` +
	`if ($home_) { $report += "$esc]777;motty-home;__MOTTY_TOKEN__;$home_$bel" }; ` +
	`if ($pwd.Provider.Name -eq 'FileSystem') { ` +
	`$folder = & $global:__MottyUri $pwd.ProviderPath; ` +
	`if ($folder) { $report += "$esc]7;file://${env:COMPUTERNAME}$folder$bel" } }; ` +
	`return "$report$original$esc]133;B;motty=__MOTTY_TOKEN__$bel" } ` +
	`}; ` +
	`[Console]::Out.Write([string]::Concat([char]27, ']7777;motty-ready;__MOTTY_TOKEN__', [char]7__MOTTY_REWIND__))`
