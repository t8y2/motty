package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

func TestAgentNamesMatchTheTerminalCommand(t *testing.T) {
	cases := []struct {
		args []string
		want codingAgent
	}{
		{[]string{"/bin/zsh"}, agentNone},
		{[]string{"node"}, agentNone},
		{[]string{"mcode-tools"}, agentNone},
		{[]string{"/usr/local/bin/claude"}, agentClaude},
		{[]string{"claude-code"}, agentClaude},
		{[]string{`C:\Tools\codex.exe`}, agentCodex},
		{[]string{"gemini"}, agentGemini},
		{[]string{"dsh"}, agentDeepseek},
		{[]string{"/opt/bin/kimi"}, agentKimi},
		{[]string{"qwen-code"}, agentQwen},
		{[]string{"zcode.cjs"}, agentZhipu},
		{[]string{"mcode"}, agentMinimax},
		{[]string{"/usr/local/bin/pi"}, agentPi},
		{[]string{"opencode"}, agentOpenCode},
		{[]string{"grok-build"}, agentGrok},
		{[]string{"node", "/home/user/.npm/lib/node_modules/@earendil-works/pi-coding-agent/dist/cli.js"}, agentPi},
		{[]string{"node", "/home/user/node_modules/@oh-my-pi/pi-coding-agent/dist/cli.js"}, agentNone},
		{[]string{"node", "/home/user/.npm/lib/node_modules/@qwen-code/qwen-code/dist/index.js"}, agentQwen},
		{[]string{"bash", "-c", "echo claude"}, agentNone},
		{[]string{"vim", "/home/user/code/pi"}, agentNone},
		{[]string{"ssh", "host", "pi"}, agentNone},
		{[]string{"man", "claude"}, agentNone},
		{[]string{"git", "checkout", "opencode"}, agentNone},
		{[]string{"make", "grok"}, agentNone},
		{[]string{"node", "server.js", "claude"}, agentNone},
		{[]string{"python3", "grok.py"}, agentNone},
		{[]string{"env", "FOO=bar", "claude"}, agentClaude},
		{[]string{"sudo", "-E", "env", "A=1", "codex"}, agentCodex},
		{[]string{"node", "--inspect", "/opt/bin/gemini"}, agentGemini},
		{[]string{"node", `C:\Users\a\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\cli.js`}, agentClaude},
		{[]string{"node", `C:\Users\a\AppData\Roaming\npm\node_modules\@google\gemini-cli\dist\index.js`}, agentGemini},
		{[]string{"cmd.exe", "/c", `C:\Users\a\npm\pi.cmd`}, agentPi},
		{[]string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, agentNone},
		{[]string{"node", `C:\Users\a\AppData\Roaming\npm\node_modules\npm\bin\npx-cli.js`, "claude"}, agentClaude},
		{[]string{"npx", "@anthropic-ai/claude-code"}, agentClaude},
		{nil, agentNone},
	}
	for _, test := range cases {
		if got := agentFromArgs(test.args); got != test.want {
			t.Errorf("agentFromArgs(%q) = %d, want %d", test.args, got, test.want)
		}
	}
	if _, ok := agentIcon(agentNone); ok {
		t.Fatal("no agent still has a mark")
	}
	for _, agent := range []codingAgent{agentClaude, agentCodex, agentGemini, agentDeepseek, agentKimi, agentQwen, agentZhipu, agentMinimax, agentPi, agentOpenCode, agentGrok} {
		if icon, ok := agentIcon(agent); !ok || icon.ViewBox == 0 {
			t.Fatalf("agent %d has no icon", agent)
		}
	}
}

// tabBadgeBackground is the color of the rounded square a tab's badge sits on,
// or false when the tab bar paints none.
func tabBadgeBackground(canvas *gfx.Canvas) (gfx.Color, bool) {
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Y < windowBarHeight && rectangle.Bounds.Width >= 16 && rectangle.Bounds.Width <= 24 && rectangle.Radius > 0 {
			return rectangle.Color, true
		}
	}
	return gfx.Color{}, false
}

func TestLocalTabUsesTheAgentMarkInsteadOfTheShellBadge(t *testing.T) {
	state, session := openLocal(t)
	canvas, _ := paintedTexts(state)
	background, ok := tabBadgeBackground(canvas)
	if !ok {
		t.Fatal("a local tab with no agent paints no badge to replace")
	}
	if !tabBadgeColor(canvas, background) {
		t.Fatal("the badge color was not found again")
	}
	session.agent = agentClaude
	canvas, _ = paintedTexts(state)
	if tabBadgeColor(canvas, background) {
		t.Fatal("a tab running an agent still paints the shell badge")
	}
}

func TestLocalTabFollowsTheForegroundAgent(t *testing.T) {
	state, session := openLocal(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	quoted := "'" + strings.ReplaceAll(script, "'", `'\''`) + "'"
	if !session.term.shell.Write([]byte("exec " + quoted + "\n")) {
		t.Fatal("the shell refused the command")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if session.agent == agentClaude {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tab agent = %d, want claude", session.agent)
}

func tabBadgeColor(canvas *gfx.Canvas, color gfx.Color) bool {
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Y >= windowBarHeight || rectangle.Bounds.Width < 16 || rectangle.Bounds.Width > 24 {
			continue
		}
		if rectangle.Color == color {
			return true
		}
	}
	return false
}

func TestRemoteTabKeepsItsHostBadge(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.System = sshconn.SystemUbuntu
	})
	state.Wake()
	state.sessions[0].agent = agentClaude
	canvas, _ := paintedTexts(state)
	ubuntu, ok := systemBadgeArt(sshconn.SystemUbuntu, state.pal.dark)
	if !ok || !tabBadgeColor(canvas, ubuntu.background) {
		t.Fatal("a remote tab painted an agent mark over its host badge")
	}
}

// foregroundShell reports a chosen foreground argv, as the local shell does.
type foregroundShell struct {
	Shell
	args atomic.Pointer[[]string]
	asks atomic.Int32
}

func (shell *foregroundShell) ForegroundArgs() ([]string, bool) {
	shell.asks.Add(1)
	args := shell.args.Load()
	if args == nil {
		return nil, false
	}
	return *args, true
}

func TestAgentMarkGoesWhenTheAgentEndsInsideTheProbeGap(t *testing.T) {
	state, session := openLocal(t)
	var wakes atomic.Int32
	session.relay = newNotifyRelay(func() { wakes.Add(1) })
	var clock atomic.Int64
	clock.Store(int64(time.Second))
	state.options.Now = func() time.Duration { return time.Duration(clock.Load()) }
	fake := &foregroundShell{Shell: session.term.shell}
	running := []string{"claude"}
	fake.args.Store(&running)
	session.term.shell = fake
	t.Cleanup(func() { session.term.shell = fake.Shell })

	state.syncAgentIcon(session, true, false)
	if session.agent != agentClaude {
		t.Fatalf("agent = %d, want claude", session.agent)
	}
	// The agent ends and its last output wakes us inside the gap.
	prompt := []string{"/bin/sh"}
	fake.args.Store(&prompt)
	clock.Add(int64(agentProbeGap / 5))
	asks := fake.asks.Load()
	state.syncAgentIcon(session, true, false)
	if fake.asks.Load() != asks || session.agent != agentClaude {
		t.Fatal("the foreground was read again inside the gap")
	}
	deadline := time.Now().Add(2 * time.Second)
	for wakes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no wake-up was scheduled for the end of the gap")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The wake-up that comes carries no output; it is still honored.
	clock.Add(int64(agentProbeGap))
	state.syncAgentIcon(session, false, false)
	if session.agent != agentNone {
		t.Fatalf("agent = %d after it ended, want none", session.agent)
	}
}

func TestQuietWakesDoNotReadTheForeground(t *testing.T) {
	state, session := openLocal(t)
	fake := &foregroundShell{Shell: session.term.shell}
	session.term.shell = fake
	t.Cleanup(func() { session.term.shell = fake.Shell })
	for range 50 {
		state.syncAgentIcon(session, false, false)
	}
	if asks := fake.asks.Load(); asks != 0 {
		t.Fatalf("%d reads on wakes with no output", asks)
	}
}

func TestAFailedReadKeepsTheMarkAndClearsTheOwedRead(t *testing.T) {
	state, session := openLocal(t)
	var clock atomic.Int64
	clock.Store(int64(time.Second))
	state.options.Now = func() time.Duration { return time.Duration(clock.Load()) }
	session.relay = newNotifyRelay(func() {})
	fake := &foregroundShell{Shell: session.term.shell}
	running := []string{"claude"}
	fake.args.Store(&running)
	session.term.shell = fake
	t.Cleanup(func() { session.term.shell = fake.Shell })
	state.syncAgentIcon(session, true, false)
	clock.Add(int64(agentProbeGap / 5))
	state.syncAgentIcon(session, true, false) // owes a read
	fake.args.Store(nil)                      // and that read fails
	clock.Add(int64(agentProbeGap))
	state.syncAgentIcon(session, false, false)
	if session.agent != agentClaude || session.agentWatch.due {
		t.Fatalf("agent = %d due = %v, want claude and nothing owed", session.agent, session.agentWatch.due)
	}
}

func TestAnAgentThatPrintsNothingIsFoundByTheTrailingRead(t *testing.T) {
	state, session := openLocal(t)
	var clock atomic.Int64
	clock.Store(int64(time.Second))
	state.options.Now = func() time.Duration { return time.Duration(clock.Load()) }
	session.relay = newNotifyRelay(func() {})
	fake := &foregroundShell{Shell: session.term.shell}
	prompt := []string{"/bin/sh"}
	fake.args.Store(&prompt)
	session.term.shell = fake
	t.Cleanup(func() { session.term.shell = fake.Shell })

	// The echo of the command arrives before the shell has started it.
	state.syncAgentIcon(session, true, false)
	if session.agent != agentNone {
		t.Fatalf("agent = %d before the program started", session.agent)
	}
	running := []string{"claude"}
	fake.args.Store(&running)
	clock.Add(int64(agentProbeGap))
	state.syncAgentIcon(session, false, false) // the wake the trailing read asked for
	if session.agent != agentClaude {
		t.Fatalf("agent = %d, want claude", session.agent)
	}
}
