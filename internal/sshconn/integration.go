package sshconn

import (
	"context"
	"errors"
	"path"
	"strings"

	"motty/internal/shellkind"
)

const shellIntegrationProbe = `printf '\036motty-shell:%s\037\n' "$SHELL"`

// shellProbe asks the login shell what it is: its path for the integration
// script, and the family for callers that type commands into it. It runs on a
// command session opened after the interactive shell, never before it.
const shellProbe = shellIntegrationProbe + `
echo "motty-shell:$PSVersionTable$FISH_VERSION"`

// ProbeShell learns the server's login shell over a command session. It must be
// called only after the interactive shell session is open: on the first session
// of a connection it would consume the login message (ADR 0025). A gateway whose
// banner does not name a full server is not asked at all, and a refusal is
// reported to the connection the way every optional probe is.
func (conn *Conn) ProbeShell(ctx context.Context) (path string, kind shellkind.Kind) {
	if conn.config.InteractiveOnly || !standardImplementation(conn.Status().ServerVersion) {
		return "", shellkind.Unknown
	}
	probes := conn.probeState()
	probes.begin()
	data, err := conn.RunCommand(ctx, shellProbe, 4096)
	probes.end(err)
	if err != nil {
		return "", shellkind.Unknown
	}
	kind = shellkind.ParseProbe(string(data))
	_, value, ok := strings.Cut(string(data), "\x1emotty-shell:")
	if !ok {
		return "", kind
	}
	shell, _, ok := strings.Cut(value, "\x1f")
	if !ok {
		return "", kind
	}
	return shell, kind
}

// IntegrationArgs is how a shell of this machine starts with directory and
// prompt reporting: run them with sh. The result is nil for a shell that is not
// a recognized absolute Bash or Zsh path, which then starts as itself.
func IntegrationArgs(shell, token string) []string {
	script := integrationScript(shell, token)
	if script == "" {
		return nil
	}
	return []string{"-c", script, "sh", shell}
}

// IntegrationReadyMarker ends an injected integration script. It is a private
// OSC string: the terminal drops it while the script's echo is hidden, and
// nothing else interprets it.
func IntegrationReadyMarker(token string) []byte {
	return []byte("\x1b]7777;motty-ready;" + token + "\x07")
}

// UploadIntegration writes the shell integration to a private temporary file
// on the server and returns the short line that activates it in the running
// shell. The file goes over a command session, not the pty: a terminal's
// canonical input buffer is a few kilobytes, and a script typed into it would
// be truncated. The activation line is small enough for the pty and is history
// guarded:
//
//   - Bash turns history off before sourcing the file, restores it afterwards
//     and removes the one line Bash recorded before the guard ran.
//   - Zsh pushes an anonymous history list and pops it after the file.
//
// The file starts the shell in directory when it names an existing absolute
// path, and removes itself in the activation line.
func (conn *Conn) UploadIntegration(ctx context.Context, shellPath, token, directory string) (string, error) {
	name := path.Base(shellPath)
	if !validIntegrationToken(token) || (name != "bash" && name != "zsh") {
		return "", errors.New("sshconn: shell does not support integration")
	}
	start := ""
	if validStartDirectory(directory) {
		start = "cd -- " + shellQuote(directory) + " 2>/dev/null\n"
	}
	var body string
	if name == "bash" {
		body = "_motty_runtime=$(mktemp -d \"${TMPDIR:-/tmp}/motty-inject.XXXXXXXX\") || _motty_runtime=\n" +
			start + bashHooks(directoryReportFunction, false) + "\n"
	} else {
		body = start + zshIntegration(directoryReportFunction) + "\n"
	}
	body = strings.ReplaceAll(body, "__MOTTY_INPUT_TOKEN__", token)
	body = strings.ReplaceAll(body, "__MOTTY_SUFFIX__", ";motty="+token)
	// One command: create a private directory, print its path, and write the
	// script from stdin. The path is the only output.
	upload := `d=$(mktemp -d "${TMPDIR:-/tmp}/motty-shell.XXXXXXXX") || exit 1; chmod 700 "$d"; printf '%s\n' "$d"; umask 077; cat > "$d/init"`
	probes := conn.probeState()
	probes.begin()
	output, err := conn.RunCommandInput(ctx, upload, []byte(body), 4096)
	probes.end(err)
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(output))
	if !validStartDirectory(dir) {
		return "", errors.New("sshconn: integration upload returned no directory")
	}
	return integrationActivation(name, token, dir+"/init", dir), nil
}

// integrationActivation is the short, history-guarded line written into the
// running shell. The ready marker is on the last line that is still under the
// history guard, so restoring history can never record it.
func integrationActivation(name, token, file, dir string) string {
	ready := "printf '\\033]7777;motty-ready;" + token + "\\007'"
	// One line, read while history is off: sourcing, cleanup, dropping the one
	// guard line the shell recorded, restoring the user's history, and the ready
	// marker. Bash may prompt between physical lines, so keeping the install on
	// one line leaves at most one prompt before the marker.
	install := ". " + shellQuote(file) + "; rm -rf -- " + shellQuote(dir)
	switch name {
	case "bash":
		// The first line turns history off; the shell records it before it
		// runs. The last entry is deleted only when it is that line, so a shell
		// that never recorded it (HISTCONTROL=ignorespace) keeps its history.
		prune := `_motty_hist=$(HISTTIMEFORMAT= history 1); case $_motty_hist in *"_motty_hist_on="*) history -d "$(printf '%s\n' "$_motty_hist" | awk '{print $1}')" 2>/dev/null ;; esac; set -o history; unset _motty_hist`
		return " _motty_hist_on=; if [[ -o history ]]; then _motty_hist_on=1; set +o history; fi\n" +
			install + "; if [[ -n ${_motty_hist_on-} ]]; then " + prune + "; fi; unset _motty_hist_on; " + ready + "\n"
	case "zsh":
		// The leading space lets a shell with HIST_IGNORE_SPACE keep the one
		// line it records out of its history; Zsh offers no entry deletion.
		return " fc -p 2>/dev/null\n" + install + "; " + ready + "; fc -P 2>/dev/null\n"
	}
	return ""
}

// Only recognized Unix shells opt into the startup shim. The probe and shim
// never write commands to an already running interactive program.
func integrationCommand(shell string, tokens ...string) string {
	return integrationCommandIn(shell, "", tokens...)
}

// integrationCommandIn is integrationCommand for a shell that starts in
// directory. The path is the script's second argument, quoted like the shell's,
// never part of the script: it cannot be taken for code. A directory that is not
// absolute or holds control characters, or one the server no longer has, leaves
// the shell where it starts anyway.
func integrationCommandIn(shell, directory string, tokens ...string) string {
	script := integrationScript(shell, tokens...)
	if script == "" {
		return ""
	}
	command := "sh -c " + shellQuote(script) + " sh " + shellQuote(shell)
	if validStartDirectory(directory) {
		command += " " + shellQuote(directory)
	}
	return command
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func validStartDirectory(directory string) bool {
	return strings.HasPrefix(directory, "/") && len(directory) <= 4096 &&
		strings.IndexFunc(directory, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

// integrationScript is the sh program that starts shell, taken as its first
// argument, with reporting; empty when the shell or token is not accepted.
func integrationScript(shell string, tokens ...string) string {
	return integrationScriptWith(shell, false, tokens...)
}

// integrationScriptWith is integrationScript, optionally taking the DEBUG-trap
// path for command-start reports that only Bash before 4.4 needs, so that it can
// be tested on a current Bash.
func integrationScriptWith(shell string, forceTrap bool, tokens ...string) string {
	suffix := ""
	if len(tokens) > 0 && tokens[0] != "" {
		if !validIntegrationToken(tokens[0]) {
			return ""
		}
		suffix = ";motty=" + tokens[0]
	}
	if !strings.HasPrefix(shell, "/") || strings.IndexFunc(shell, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return ""
	}
	name := path.Base(shell)
	if name != "bash" && name != "zsh" {
		return ""
	}
	return startScript(name, directoryReportFunction, suffix, forceTrap)
}

func validIntegrationToken(token string) bool {
	return len(token) <= 128 && strings.IndexFunc(token, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}

// startScript starts the shell in $1 ("bash" or "zsh") with a private startup
// that sources the user's own files and then reports through report.
func startScript(name, report, suffix string, forceTrap bool) string {
	script := `if [ -n "${2-}" ]; then cd -- "$2" 2>/dev/null; fi
_motty_umask=$(umask)
umask 077
_motty_dir=$(mktemp -d "${TMPDIR:-/tmp}/motty-shell.XXXXXXXX") || { umask "$_motty_umask"; exec "$1" -l; }
trap 'rm -rf -- "$_motty_dir"' 0
trap 'exit 129' HUP
trap 'exit 143' TERM
`
	if name == "bash" {
		script += "cat > \"$_motty_dir/bashrc\" <<'MOTTY_BASH'\n" + bashIntegration(report, forceTrap) + "\nMOTTY_BASH\n" +
			`umask "$_motty_umask"
MOTTY_SHELL_DIR=$_motty_dir "$1" --rcfile "$_motty_dir/bashrc" -i
`
	} else {
		script += `cat > "$_motty_dir/.zshenv" <<'MOTTY_ENV'
_motty_runtime=$ZDOTDIR
_motty_user_zdotdir=$MOTTY_USER_ZDOTDIR
_motty_user_zdotdir_set=$MOTTY_USER_ZDOTDIR_SET
unset MOTTY_USER_ZDOTDIR MOTTY_USER_ZDOTDIR_SET
ZDOTDIR=$_motty_user_zdotdir
[[ -r "$ZDOTDIR/.zshenv" ]] && source "$ZDOTDIR/.zshenv"
_motty_user_zdotdir=${ZDOTDIR-$HOME}
ZDOTDIR=$_motty_runtime
MOTTY_ENV
for _motty_file in .zprofile .zshrc .zlogin; do
cat > "$_motty_dir/$_motty_file" <<'MOTTY_START'
ZDOTDIR=$_motty_user_zdotdir
# /etc/zshrc can derive HISTFILE from the private ZDOTDIR. Repair that default
# before the user's startup files choose their own history path.
if [[ ${HISTFILE-} == "$_motty_runtime/.zsh_history" ]]; then
    HISTFILE=$ZDOTDIR/.zsh_history
fi
MOTTY_START
printf '[[ -r "$ZDOTDIR/%s" ]] && source "$ZDOTDIR/%s"\n' "$_motty_file" "$_motty_file" >> "$_motty_dir/$_motty_file"
if [ "$_motty_file" != .zlogin ]; then
cat >> "$_motty_dir/$_motty_file" <<'MOTTY_NEXT'
_motty_user_zdotdir=${ZDOTDIR-$HOME}
ZDOTDIR=$_motty_runtime
MOTTY_NEXT
fi
done
cat >> "$_motty_dir/.zlogin" <<'MOTTY_ZSH'
` + zshIntegration(report) + `
if [[ -z $_motty_user_zdotdir_set && $ZDOTDIR == $HOME ]]; then unset ZDOTDIR; fi
rm -rf -- "$_motty_runtime"
unset _motty_runtime _motty_user_zdotdir _motty_user_zdotdir_set
MOTTY_ZSH
umask "$_motty_umask"
MOTTY_USER_ZDOTDIR=${ZDOTDIR-$HOME} MOTTY_USER_ZDOTDIR_SET=${ZDOTDIR+x} ZDOTDIR=$_motty_dir "$1" -l
`
	}
	script += "_motty_status=$?\nexit \"$_motty_status\"\n"
	script = strings.ReplaceAll(script, "__MOTTY_INPUT_TOKEN__", strings.TrimPrefix(suffix, ";motty="))
	return strings.ReplaceAll(script, "__MOTTY_SUFFIX__", suffix)
}

// WSLIntegrationScript is the sh program a WSL distribution runs to start its
// user's login shell with folder and prompt reporting. The folder it reports
// is the Windows path of the shell's folder, which wslpath makes of the Linux
// one, so a Windows program can open it: Windows Terminal's OSC 9;9, with the
// prompt markers bound to token. Before starting the shell it changes into the
// folder named by the environment variable startDirectory, a Linux path or a
// Windows one, when there is one and it exists. The result is empty for a
// token or variable name that is not accepted.
func WSLIntegrationScript(token, startDirectory string) string {
	if token == "" || !validIntegrationToken(token) || !validVariableName(startDirectory) {
		return ""
	}
	suffix := ";motty=" + token
	return `_motty_cd=${` + startDirectory + `-}
unset ` + startDirectory + `
if [ -n "$_motty_cd" ]; then
    case $_motty_cd in
        /*) ;;
        *) _motty_cd=$(wslpath -u "$_motty_cd" 2>/dev/null) ;;
    esac
    if [ -n "$_motty_cd" ]; then cd -- "$_motty_cd" 2>/dev/null; fi
fi
unset _motty_cd
_motty_shell=$(getent passwd "$(id -u)" 2>/dev/null | cut -d: -f7)
[ -x "$_motty_shell" ] || _motty_shell=${SHELL-}
[ -x "$_motty_shell" ] || _motty_shell=/bin/sh
set -- "$_motty_shell"
case ${_motty_shell##*/} in
bash)
` + startScript("bash", wslReportFunction, suffix, false) + `;;
zsh)
` + startScript("zsh", wslReportFunction, suffix, false) + `;;
*)
    exec "$_motty_shell" -l
    ;;
esac
`
}

func validVariableName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'A' && c <= 'Z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// wslReportFunction reports where the prompt is as a Windows path. wslpath is a
// process, so it runs only when the folder has changed. A path with a control
// character is not reported at all: it could end the report early and have the
// rest read as terminal output.
const wslReportFunction = `__motty_report() {
    local _motty_status=$?
    __motty_input_report
    if [[ $PWD != "${_motty_wsl_pwd-}" ]]; then
        _motty_wsl_pwd=$PWD
        _motty_wsl_dir=$(wslpath -w "$PWD" 2>/dev/null)
        case $_motty_wsl_dir in
            *[[:cntrl:]]*) _motty_wsl_dir= ;;
        esac
    fi
    if [[ ${_motty_wsl_home_source-} != "$HOME" ]]; then
        _motty_wsl_home_source=$HOME
        _motty_wsl_home=$(wslpath -w "$HOME" 2>/dev/null)
    fi
    printf '\033]133;A__MOTTY_SUFFIX__\007'
    __motty_home_report "${_motty_wsl_home-}"
    if [[ -n ${_motty_wsl_dir-} ]]; then
        printf '\033]9;9;%s\007' "$_motty_wsl_dir"
    fi
    case $PS1 in
        *"$_motty_prompt_marker") ;;
        *) PS1=$PS1$_motty_prompt_marker ;;
    esac
    return "$_motty_status"
}`

// Share the byte encoder between cwd and token-bound home metadata, including
// Windows paths produced by wslpath. Neither value is evaluated as shell code.
const homeReportFunction = `__motty_encode_path() {
    local LC_ALL=C _motty_char _motty_hex _motty_i
    for ((_motty_i=0; _motty_i<${#1}; _motty_i++)); do
        _motty_char=${1:_motty_i:1}
        case $_motty_char in
            [a-zA-Z0-9/._~-]) _motty_path=$_motty_path$_motty_char ;;
            *) printf -v _motty_hex '%%%02X' "'$_motty_char"; _motty_path=$_motty_path%${_motty_hex: -2} ;;
        esac
    done
}
__motty_home_report() {
    [[ -n '__MOTTY_INPUT_TOKEN__' ]] || return 0
    local _motty_path=
    __motty_encode_path "$1"
    printf '\033]777;motty-home;__MOTTY_INPUT_TOKEN__;%s\007' "$_motty_path"
}
`

// Encode bytes, not characters: file URI reserved characters and non-ASCII
// paths must round-trip through the terminal parser without shell evaluation.
const directoryReportFunction = `__motty_report() {
    local _motty_status=$?
    __motty_input_report
    local _motty_path=
    __motty_encode_path "$PWD"
    printf '\033]133;A__MOTTY_SUFFIX__\007'
    __motty_home_report "$HOME"
    printf '\033]7;file://%s%s\007' "${HOSTNAME:-${HOST:-}}" "$_motty_path"
    case $PS1 in
        *"$_motty_prompt_marker") ;;
        *) PS1=$PS1$_motty_prompt_marker ;;
    esac
    return "$_motty_status"
}`

func bashIntegration(report string, forceTrap bool) string {
	return bashIntegrationHead + bashHooks(report, forceTrap)
}

// bashHooks is the part of the integration that installs the reporting into the
// current shell, without starting or sourcing anything. The startup wrapper
// puts it in a private rcfile; an SSH shell receives it by injection after it
// is already running (ADR 0025).
func bashHooks(report string, forceTrap bool) string {
	force := "0"
	if forceTrap {
		force = "1"
	}
	return inputEnvironmentFunction + homeReportFunction + commandFinishedFunction + report + strings.ReplaceAll(bashIntegrationTail, "__MOTTY_FORCE__", force)
}

const bashIntegrationHead = `_motty_runtime=$MOTTY_SHELL_DIR
unset MOTTY_SHELL_DIR
# A login shell reads /etc/profile and the first of these login profiles, and
# ~/.bashrc only when that profile sources it. Sourcing ~/.bashrc here as well
# would run the user's interactive startup twice.
[[ -r /etc/profile ]] && source /etc/profile
for _motty_profile in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
    if [[ -r $_motty_profile ]]; then source "$_motty_profile"; break; fi
done
`

// Run before user prompt hooks, which may replace the command's exit status.
// Return that status unchanged so the user's first hook still sees it.
const commandFinishedFunction = `__motty_command_finished() {
    local _motty_status=$?
    printf '\033]133;D;%s__MOTTY_SUFFIX__\007' "$_motty_status"
    return "$_motty_status"
}
`

const bashIntegrationTail = `
_motty_prompt_marker='\[\e]133;B__MOTTY_SUFFIX__\a\]'
case $(declare -p PROMPT_COMMAND 2>/dev/null) in
    'declare -a '*) PROMPT_COMMAND=(__motty_command_finished "${PROMPT_COMMAND[@]}" __motty_report) ;;
    *) PROMPT_COMMAND=__motty_command_finished$'\n'${PROMPT_COMMAND:+$PROMPT_COMMAND$'\n'}__motty_report ;;
esac
if (( __MOTTY_FORCE__ == 0 && (BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 4)) )); then
    PS0=$'\e]133;C__MOTTY_SUFFIX__\a'${PS0-}
    printf '\033]133;C__MOTTY_SUFFIX__\007'
else
    # No PS0 before Bash 4.4: report the start of a command line from a DEBUG
    # trap, which runs before every simple command. PROMPT_COMMAND ends by
    # arming it, and the first command after that reports once and disarms.
    # An empty Enter reruns PROMPT_COMMAND, whose first entry disarms again so
    # that its own commands never count. An array PROMPT_COMMAND (Bash 5.1+) is
    # not taken here; those versions have PS0.
    case $(declare -p PROMPT_COMMAND 2>/dev/null) in
        'declare -a '*) ;;
        *)
            __motty_disarm() { local _motty_status=$?; _motty_armed=0; return "$_motty_status"; }
            __motty_arm() { _motty_armed=1; }
            __motty_debug() {
                [[ ${_motty_armed-} == 1 && $BASH_COMMAND != __motty_disarm && -z ${COMP_POINT-} ]] || return 0
                _motty_armed=0
                printf '\033]133;C__MOTTY_SUFFIX__\007'
            }
            _motty_armed=0
            PROMPT_COMMAND=__motty_disarm$'\n'${PROMPT_COMMAND:+$PROMPT_COMMAND$'\n'}__motty_arm
            # Keep the user's own DEBUG trap and run it first, so that it sees
            # the same $?, $_ and BASH_COMMAND as without us. trap -p is read
            # through a file because a command substitution is a subshell.
            trap -p DEBUG > "$_motty_runtime/debug-trap"
            _motty_trap=$(<"$_motty_runtime/debug-trap")
            __motty_unquote() { eval "set -- ${1#trap -- }"; _motty_trap=$1; }
            if [[ -n $_motty_trap ]]; then
                __motty_unquote "$_motty_trap"
                trap -- "$_motty_trap"$'\n''__motty_debug' DEBUG
            else
                trap -- '__motty_debug' DEBUG
            fi
            unset -f __motty_unquote
            unset _motty_trap
            printf '\033]133;C__MOTTY_SUFFIX__\007'
            ;;
    esac
fi
rm -rf -- "$_motty_runtime"
unset _motty_runtime _motty_profile`

func zshIntegration(report string) string {
	return inputEnvironmentFunction + homeReportFunction + commandFinishedFunction + report + zshIntegrationTail
}

const zshIntegrationTail = `
_motty_prompt_marker=$'%{\e]133;B__MOTTY_SUFFIX__\a%}'
typeset -ga precmd_functions
if (( $+functions[precmd] )); then
    # Copy the hook with its function flags, under a name the wrapper can call:
    # assigning the body alone loses flags and changes the $0 a hook sees.
    functions -c precmd __motty_user_precmd
    precmd() { __motty_command_finished; __motty_user_precmd; }
else
    precmd() { __motty_command_finished; return 0; }
fi
precmd_functions+=(__motty_report)
__motty_command_start() { printf '\033]133;C__MOTTY_SUFFIX__\007'; }
typeset -ga preexec_functions
preexec_functions=(__motty_command_start "${preexec_functions[@]}")
__motty_command_start`
