package workbench

import (
	"testing"

	"motty/internal/completion"
	"motty/internal/i18n"
)

func TestCompletionDescriptionOnlyTranslatesSelectedCommands(t *testing.T) {
	world := newCompletionWorld(t)
	world.state.setLanguage(i18n.Chinese)
	const english = "show the working tree status"
	command := completion.Candidate{Text: "status", Insert: "tus", Desc: english, Kind: completion.KindCommand, FromCatalog: true}
	want := command
	want.Desc = "查看 Git 工作区的当前状态"
	if got := world.state.localizeCompletionCandidate(command); got != want {
		t.Fatalf("localized candidate = %+v, want %+v", got, want)
	}
	for _, test := range []struct {
		line, text, english, chinese string
	}{
		{"git clo", "clone", "Clone a repository into a new directory", "将仓库克隆到新目录"},
		{"git di", "diff", "Show changes between commits", "显示提交之间的差异"},
		{"git fe", "fetch", "Download objects and refs from another repository", "从其他仓库下载对象和引用"},
		{"git lo", "log", "Show commit logs", "显示提交日志"},
		{"git stat", "status", "show the working tree status", "查看 Git 工作区的当前状态"},
	} {
		candidates, _ := world.state.completionCandidates(world.session, test.line)
		source := completion.Complete(world.state.completion.catalog, test.line)
		found := false
		for _, candidate := range candidates {
			if candidate.Text != test.text {
				continue
			}
			found = true
			if candidate.Desc != test.chinese || candidate.Insert != test.text[len(test.line)-len("git "):] {
				t.Errorf("%s = %+v, want description %q and unchanged insertion", test.line, candidate, test.chinese)
			}
			for _, original := range source.Candidates {
				if original.Text == test.text && (original.Desc != test.english || original.Insert != candidate.Insert) {
					t.Errorf("%s changed catalog candidate %+v to %+v", test.line, original, candidate)
				}
			}
		}
		if !found {
			t.Errorf("%s did not offer %q", test.line, test.text)
		}
	}

	for _, candidate := range []completion.Candidate{
		{Desc: "show the working tree status.", Kind: completion.KindCommand, FromCatalog: true},
		{Desc: "Squash commits together", Kind: completion.KindCommand, FromCatalog: true},
		{Desc: english, Kind: completion.KindCommand},
		{Desc: english, Kind: completion.KindPath},
		{Desc: english, Kind: completion.KindOption, FromCatalog: true},
		{Desc: english, Kind: completion.KindValue, FromCatalog: true},
		{Desc: "show the working tree status today", Kind: completion.KindCommand, FromCatalog: true},
		{Desc: "Reset current HEAD to the specified state", Kind: completion.KindCommand, FromCatalog: true},
	} {
		if got := world.state.localizeCompletionCandidate(candidate); got != candidate {
			t.Errorf("localized %+v as %+v", candidate, got)
		}
	}
	for _, language := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		world.state.setLanguage(language)
		if got := world.state.localizeCompletionCandidate(command); got != command {
			t.Errorf("%s changed %+v to %+v", language, command, got)
		}
	}
}
