package workbench

import (
	"path"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

// codingAgent is the coding agent in a local terminal's foreground, or none
// while the shell itself is waiting.
type codingAgent uint8

const (
	agentNone codingAgent = iota
	agentClaude
	agentCodex
	agentGemini
	agentDeepseek
	agentKimi
	agentQwen
	agentZhipu
	agentMinimax
	agentPi
	agentOpenCode
	agentGrok
)

// agentProbeGap is the least time between two reads of a tab's foreground
// process. Reading costs a system call, and on Windows a walk of the process
// table, so it follows output rather than every wake. Output inside the gap is
// not lost: one more read is scheduled for when the gap ends, which is also
// what notices a program that exits while its last output is still arriving.
const agentProbeGap = 300 * time.Millisecond

// agentWatch is when a local tab's foreground was last read.
type agentWatch struct {
	probed   bool
	probedAt time.Duration
	// due is set while a read is owed once dueAt has passed.
	due   bool
	dueAt time.Duration
}

// agentCommands are terminal coding agents, keyed by the executable's basename.
// The logo is the provider mark already used in settings where one exists.
var agentCommands = map[string]codingAgent{
	"claude":      agentClaude,
	"claude-code": agentClaude,
	"codex":       agentCodex,
	"gemini":      agentGemini,
	"deepseek":    agentDeepseek,
	"dsh":         agentDeepseek,
	"kimi":        agentKimi,
	"kimi-code":   agentKimi,
	"qwen":        agentQwen,
	"qwen-code":   agentQwen,
	"zcode":       agentZhipu,
	"mcode":       agentMinimax,
	"pi":          agentPi,
	"opencode":    agentOpenCode,
	"opencode2":   agentOpenCode,
	"grok":        agentGrok,
	"grok-build":  agentGrok,
}

func agentIcon(agent codingAgent) (gfx.Icon, bool) {
	switch agent {
	case agentClaude:
		return icons.AiClaude, true
	case agentCodex:
		return icons.AiOpenai, true
	case agentGemini:
		return icons.AiGemini, true
	case agentDeepseek:
		return icons.AiDeepseek, true
	case agentKimi:
		return icons.AiKimi, true
	case agentQwen:
		return icons.AiQwen, true
	case agentZhipu:
		return icons.AiZai, true
	case agentMinimax:
		return icons.AiMinimax, true
	case agentPi:
		return icons.AiPi, true
	case agentOpenCode:
		return icons.AiOpencode, true
	case agentGrok:
		return icons.AiGrok, true
	default:
		return gfx.Icon{}, false
	}
}

// agentName is what a tab calls the coding agent in front of it. These are the
// products' own names, not translated: the mark beside the name is the
// product's.
func agentName(agent codingAgent) string {
	switch agent {
	case agentClaude:
		return "Claude Code"
	case agentCodex:
		return "Codex"
	case agentGemini:
		return "Gemini"
	case agentDeepseek:
		return "DeepSeek"
	case agentKimi:
		return "Kimi"
	case agentQwen:
		return "Qwen"
	case agentZhipu:
		return "ZCode"
	case agentMinimax:
		return "MiniMax"
	case agentPi:
		return "Pi"
	case agentOpenCode:
		return "OpenCode"
	case agentGrok:
		return "Grok"
	default:
		return ""
	}
}

// launchers are programs that run another program named in their arguments:
// language runtimes, shells and wrappers. A foreground argv that starts with one
// is read further; any other program is judged by its own name alone, so
// "vim pi" or "man claude" is not an agent.
var launchers = map[string]bool{
	"node": true, "nodejs": true, "bun": true, "deno": true, "npx": true, "npx-cli": true, "bunx": true,
	"env": true, "sudo": true, "doas": true, "nohup": true, "caffeinate": true,
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true,
	"cmd": true, "powershell": true, "pwsh": true,
}

func isLauncher(arg string) bool {
	base := agentBase(arg)
	return launchers[base] || strings.HasPrefix(base, "python")
}

// agentFromArgs reports the coding agent a foreground argv runs. The program
// itself is checked first. When that is a launcher, the arguments after its
// flags and NAME=value settings are checked in turn, up to the first one that is
// neither an agent nor another launcher. A shell, a bare node runtime, or
// anything unrecognized is agentNone.
func agentFromArgs(args []string) codingAgent {
	for i, arg := range args {
		if i > 0 && isOption(arg) {
			continue
		}
		if agent := agentFromProgram(arg); agent != agentNone {
			return agent
		}
		if i > 0 {
			if agent := agentFromPackagePath(arg); agent != agentNone {
				return agent
			}
		}
		if !isLauncher(arg) {
			return agentNone
		}
	}
	return agentNone
}

// isOption reports whether arg is a flag (-x, --x, /c) or a NAME=value setting,
// as `env` takes: something a launcher reads before the program it runs.
func isOption(arg string) bool {
	if strings.HasPrefix(arg, "-") || len(arg) == 2 && arg[0] == '/' {
		return true
	}
	name, _, found := strings.Cut(arg, "=")
	return found && name != "" && !strings.ContainsAny(name, `/\ `)
}

func agentFromProgram(arg string) codingAgent {
	return agentCommands[agentBase(arg)]
}

// agentBase is the lower-case name of a program path, without the extension
// Windows or a node script adds. Both path separators are understood on every
// platform.
func agentBase(arg string) string {
	arg = strings.Trim(strings.TrimSpace(arg), `"'`)
	arg = strings.ReplaceAll(arg, `\`, "/")
	base := strings.ToLower(path.Base(arg))
	if _, ok := agentCommands[base]; ok {
		return base
	}
	for _, ext := range [...]string{".exe", ".cmd", ".bat", ".ps1", ".js", ".mjs", ".cjs"} {
		if trimmed := strings.TrimSuffix(base, ext); trimmed != base {
			return trimmed
		}
	}
	return base
}

// agentFromPackagePath recognizes a node entrypoint whose own filename (cli.js,
// index.js) says nothing, by the npm package it lives in. Windows npm shims run
// exactly such a file.
func agentFromPackagePath(arg string) codingAgent {
	if strings.HasPrefix(strings.TrimSpace(arg), "-") {
		return agentNone
	}
	value := strings.ToLower(strings.ReplaceAll(arg, `\`, "/"))
	entry := func(pkg string, names ...string) bool {
		if !strings.Contains(value, "/"+pkg+"/") {
			return false
		}
		for _, name := range names {
			if strings.HasSuffix(value, "/"+name) {
				return true
			}
		}
		return false
	}
	switch {
	case entry("@earendil-works/pi-coding-agent", "cli.js", "cli.mjs"):
		return agentPi
	case entry("@qwen-code/qwen-code", "index.js", "index.mjs"):
		return agentQwen
	case entry("@anthropic-ai/claude-code", "cli.js", "cli.mjs"):
		return agentClaude
	case entry("@google/gemini-cli", "index.js", "index.mjs"):
		return agentGemini
	default:
		return agentNone
	}
}

// syncAgentIcon keeps a local tab's mark in step with the program in front.
// It is called on each wake; output is whether the shell sent any since the last
// one. Remote tabs are left on their host badge: their foreground process is
// not on this machine.
func (state *State) syncAgentIcon(session *Session, output, finished bool) {
	watch := &session.agentWatch
	if finished {
		session.agent, *watch = agentNone, agentWatch{}
		return
	}
	if !session.isLocal() || session.term == nil || session.term.shell == nil {
		return
	}
	now := state.now()
	if !output && !(watch.due && now >= watch.dueAt) {
		return
	}
	if watch.probed && now-watch.probedAt < agentProbeGap {
		if !watch.due {
			watch.due, watch.dueAt = true, watch.probedAt+agentProbeGap
			time.AfterFunc(watch.dueAt-now, state.notifyFor(session))
		}
		return
	}
	reader, ok := session.term.shell.(interface{ ForegroundArgs() ([]string, bool) })
	if !ok {
		return
	}
	watch.probed, watch.probedAt, watch.due = true, now, false
	if args, ok := reader.ForegroundArgs(); ok {
		session.agent = agentFromArgs(args)
	}
	if output {
		// The first output of a command (its echo) can arrive before the shell
		// has started the program, and a program that prints nothing sends no
		// more. One more look after the gap catches both.
		watch.due, watch.dueAt = true, now+agentProbeGap
		time.AfterFunc(agentProbeGap, state.notifyFor(session))
	}
}

func (state *State) paintTabBadge(canvas *gfx.Canvas, session *Session, bounds gfx.Rect, fill gfx.Color) {
	if session.isLocal() {
		if icon, ok := agentIcon(session.agent); ok {
			canvas.DrawIcon(icon, insetRect(bounds, 0.08), state.pal.text)
			return
		}
	}
	host := session.host
	if session.dialedProtocol == protocolWSL {
		host.Protocol = protocolWSL
		if conn, ok := session.conn.(*localConn); ok {
			host.localShell = conn.shell
		}
	}
	state.paintHostCatalogIcon(canvas, host, bounds, fill)
}
