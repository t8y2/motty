package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/completion"
	"motty/internal/i18n"
)

func TestCompletionDescriptionsFollowLanguageWithoutChangingInsertion(t *testing.T) {
	w := newCompletionWorld(t)
	catalog, err := completion.NewCatalog("test", "v1", "", "MIT", []*completion.Node{{Token: "tool", Desc: "Manage tools", Flags: []completion.Flag{{Long: "mode", Desc: "Select mode", Value: true, Values: []completion.Value{{Text: "fast", Desc: "Skip slow checks"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := completion.EncodeDescriptions(map[string]string{"Manage tools": "管理工具", "Select mode": "选择模式", "Skip slow checks": "跳过较慢的检查"})
	if err != nil {
		t.Fatal(err)
	}
	table, err := completion.DecodeDescriptions(asset)
	if err != nil {
		t.Fatal(err)
	}
	w.state.completion.catalog, w.state.completion.descriptions = catalog, table
	for _, test := range []struct {
		line, text, insert, chinese, english string
	}{
		{"too", "tool", "l", "管理工具", "Manage tools"},
		{"tool --mo", "--mode", "de", "选择模式", "Select mode"},
		{"tool --mode f", "fast", "ast", "跳过较慢的检查", "Skip slow checks"},
	} {
		for _, language := range []i18n.Language{i18n.Chinese, i18n.English, i18n.TraditionalChinese, i18n.Chinese} {
			w.state.setLanguage(language)
			candidates, _ := w.state.completionCandidates(w.session, test.line)
			if len(candidates) != 1 {
				t.Fatalf("%s candidates = %v", test.line, candidates)
			}
			candidate := candidates[0]
			want := test.english
			if language == i18n.Chinese {
				want = test.chinese
			}
			if candidate.Desc != want || candidate.Text != test.text || candidate.Insert != test.insert || !candidate.FromCatalog {
				t.Fatalf("%s %s = %+v, want desc %q", language, test.line, candidate, want)
			}
		}
	}
	if got := completion.Complete(catalog, "too").Candidates[0].Desc; got != "Manage tools" {
		t.Fatalf("shared English catalog was mutated: %q", got)
	}
}

func TestOpenCompletionListRefreshesItsLanguage(t *testing.T) {
	w := newCompletionWorld(t)
	asset, err := completion.EncodeDescriptions(map[string]string{"output the last part of files": "输出文件的末尾部分"})
	if err != nil {
		t.Fatal(err)
	}
	w.state.completion.descriptions, err = completion.DecodeDescriptions(asset)
	if err != nil {
		t.Fatal(err)
	}
	w.state.setLanguage(i18n.English)
	w.type_("tai")
	w.state.openCompletions()
	assertDescription := func(want string) {
		t.Helper()
		for _, candidate := range w.state.completion.list.candidates {
			if candidate.Text == "tail" {
				if candidate.Desc != want {
					t.Fatalf("tail description = %q, want %q", candidate.Desc, want)
				}
				return
			}
		}
		t.Fatal("tail candidate is missing")
	}
	assertDescription("output the last part of files")
	w.state.setLanguage(i18n.Chinese)
	assertDescription("输出文件的末尾部分")
	w.state.setLanguage(i18n.English)
	assertDescription("output the last part of files")
	before := w.shell.typed()
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"l" {
		t.Fatalf("acceptance typed %q, want %q", got, before+"l")
	}
}

func TestCompletionLocalizationLeavesHistoryAndPathsAlone(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.setLanguage(i18n.Chinese)
	asset, err := completion.EncodeDescriptions(map[string]string{"Show version": "显示版本"})
	if err != nil {
		t.Fatal(err)
	}
	w.state.completion.descriptions, err = completion.DecodeDescriptions(asset)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []completion.Candidate{
		{Text: "history", Desc: "Show version", Kind: completion.KindCommand},
		{Text: "path/", Desc: "Show version", Kind: completion.KindPath},
		{Text: "custom", Desc: "New upstream description", FromCatalog: true},
	} {
		if got := w.state.localizeCompletionCandidate(candidate); got != candidate {
			t.Fatalf("unrelated/missing description changed: %+v", got)
		}
	}
}

func TestChineseDescriptionsLoadOnlyWhenCompletionNeedsThem(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.setLanguage(i18n.English)
	w.state.adoptCompletion()
	if w.state.completion.descriptionsStarted {
		t.Fatal("English completion loaded the Chinese table")
	}
	w.state.setCompletionMode(completionOff)
	w.state.setLanguage(i18n.Chinese)
	w.state.adoptCompletion()
	if w.state.completion.descriptionsStarted {
		t.Fatal("disabled completion loaded the Chinese table")
	}
	w.type_("tai")
	w.state.openCompletions()
	w.state.Wake()
	if !w.state.completion.descriptionsStarted {
		t.Fatal("manual Chinese completion did not start its table worker")
	}
	w.state.setLanguage(i18n.English)
	pumpUntil(t, w.state, "Chinese description worker", func() bool {
		return w.state.completion.descriptions != nil
	})
	for _, candidate := range w.state.completion.list.candidates {
		if candidate.Text == "tail" && candidate.Desc != "output the last part of files" {
			t.Fatalf("a late Chinese load replaced English: %+v", candidate)
		}
	}
	w.state.setLanguage(i18n.Chinese)
	for _, candidate := range w.state.completion.list.candidates {
		if candidate.Text == "tail" && candidate.Desc == "output the last part of files" {
			t.Fatal("switching back did not use the loaded Chinese descriptions")
		}
	}
}
