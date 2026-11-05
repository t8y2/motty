package workbench

import (
	"strings"
	"testing"
	"time"

	"motty/internal/cmdhistory"
	"motty/internal/i18n"
	"motty/internal/shellkind"
	"motty/internal/sshconn"
)

type completionCmdShell struct{ *fakeShell }

func (completionCmdShell) Kind() shellkind.Kind { return shellkind.Cmd }

func hasCompletionCandidate(candidates []string, wanted string) bool {
	for _, name := range candidates {
		if name == wanted {
			return true
		}
	}
	return false
}

func completionCandidateNames(w *completionWorld, line string) []string {
	var names []string
	for _, candidate := range w.state.completionCandidates(w.session, line) {
		names = append(names, candidate.Text)
	}
	return names
}

func TestCompletionPlatformScopesDirAndBSDTailSyntax(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	if hasCompletionCandidate(completionCandidateNames(w, "dir /"), "/A") {
		t.Fatal("POSIX dir offered CMD /A")
	}
	w.session.term.shell = completionCmdShell{w.shell}
	if !hasCompletionCandidate(completionCandidateNames(w, "dir /"), "/A") ||
		hasCompletionCandidate(completionCandidateNames(w, "dir --"), "--all") {
		t.Fatal("CMD dir used Unix options or lost /A")
	}
	w.session.term.shell = completionPowerShell{w.shell}
	if hasCompletionCandidate(completionCandidateNames(w, "dir /"), "/A") ||
		hasCompletionCandidate(completionCandidateNames(w, "dir --"), "--all") {
		t.Fatal("PowerShell dir used CMD or Unix syntax")
	}
	w.session.term.shell = w.shell
	w.session.host.System = sshconn.SystemWindows
	if hasCompletionCandidate(completionCandidateNames(w, "dir /"), "/A") ||
		hasCompletionCandidate(completionCandidateNames(w, "dir --"), "--all") {
		t.Fatal("an unknown Windows shell guessed dir's dialect")
	}
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.session.host.System = sshconn.SystemDarwin
	if hasCompletionCandidate(completionCandidateNames(w, "tail --"), "--follow") ||
		!hasCompletionCandidate(completionCandidateNames(w, "tail -"), "-f") {
		t.Fatal("BSD tail used an unverified GNU long flag or lost the common short flag")
	}
	w.session.host.System = sshconn.SystemLinux
	if !hasCompletionCandidate(completionCandidateNames(w, "tail --"), "--follow") {
		t.Fatal("Linux tail lost its public catalog options")
	}
}

func TestCompletionCommonChineseDetailsAndStableRootPriority(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.setLanguage(i18n.Chinese)
	w.type_("ca")
	candidates := w.state.completionCandidates(w.session, "ca")
	if len(candidates) == 0 || candidates[0].Text != "cat" || !strings.Contains(candidates[0].Desc, "文件") {
		t.Fatalf("common Chinese root was not prioritized: %+v", candidates)
	}
	w.state.setLanguage(i18n.TraditionalChinese)
	options := w.state.completionCandidates(w.session, "cat -")
	found := false
	for _, candidate := range options {
		if candidate.Text == "-n" {
			found = strings.Contains(candidate.Desc, "行")
		}
	}
	if !found {
		t.Fatalf("traditional Chinese cat option missing: %+v", options)
	}
}

func TestCompletionHistoryAndInstalledCommandsRemainAheadOfCommonRoots(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("ca")
	w.state.completion.probeEnabled = true
	w.state.completion.probe = completionProbe{
		sessionID: w.session.ID, owner: w.session.term, conn: w.session.conn,
		expiresAt: w.clock + completionProbeCache, available: map[string]bool{"cargo": true},
	}
	candidates := w.state.completionCandidates(w.session, "ca")
	if len(candidates) == 0 || candidates[0].Text != "cargo" {
		t.Fatalf("installed command ranked below static common command: %+v", candidates)
	}
	w.state.history.on = true
	w.state.history.ghost = map[string][]cmdhistory.Entry{
		historyKey(w.session): {{Command: "cat custom", Used: time.Now(), Count: 2}},
	}
	candidates = w.state.completionCandidates(w.session, "ca")
	if len(candidates) < 2 || candidates[0].Text != "cat custom" || candidates[1].Text != "cargo" {
		t.Fatalf("history or availability ordering changed: %+v", candidates)
	}
}
