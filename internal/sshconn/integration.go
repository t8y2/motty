package sshconn

import (
	"context"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"motty/internal/shellkind"
)

const shellIntegrationProbe = `printf '\036motty-shell:%s\037' "$SHELL"`

func (conn *Conn) shellIntegrationCommand(token, directory string) string {
	ctx, cancel := context.WithTimeout(conn.ctx, 2*time.Second)
	defer cancel()
	data, err := conn.RunCommand(ctx, shellIntegrationProbe, 4096)
	if err != nil {
		return ""
	}
	_, value, ok := strings.Cut(string(data), "\x1emotty-shell:")
	if !ok {
		return ""
	}
	shell, _, ok := strings.Cut(value, "\x1f")
	if !ok {
		return ""
	}
	return integrationCommandIn(shell, directory, token)
}

// defaultShellKind asks the server's default shell what it is, once per client:
// a command channel runs in the same shell an interactive session starts. A host
// that does not answer in time stays Unknown, and nothing is cached, so the next
// shell asks again.
func (conn *Conn) defaultShellKind(client *ssh.Client) shellkind.Kind {
	conn.kindMu.Lock()
	defer conn.kindMu.Unlock()
	if conn.kindClient == client {
		return conn.kind
	}
	ctx, cancel := context.WithTimeout(conn.ctx, 2*time.Second)
	defer cancel()
	data, err := conn.RunCommand(ctx, shellkind.Probe, 4096)
	if err != nil {
		return shellkind.Unknown
	}
	conn.kindClient, conn.kind = client, shellkind.ParseProbe(string(data))
	return conn.kind
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
	force := "0"
	if forceTrap {
		force = "1"
	}
	return bashIntegrationHead + inputEnvironmentFunction + homeReportFunction + commandFinishedFunction + report + strings.ReplaceAll(bashIntegrationTail, "__MOTTY_FORCE__", force)
}

const bashIntegrationHead = `_motty_runtime=$MOTTY_SHELL_DIR
unset MOTTY_SHELL_DIR
[[ -r /etc/profile ]] && source /etc/profile
for _motty_profile in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
    if [[ -r $_motty_profile ]]; then source "$_motty_profile"; break; fi
done
# The login profiles only reach ~/.bashrc when they source it themselves; a
# server whose interactive setup lives there must still get it.
for _motty_bashrc in /etc/bash.bashrc "$HOME/.bashrc"; do
    if [[ -r $_motty_bashrc ]]; then source "$_motty_bashrc"; fi
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
