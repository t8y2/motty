package workbench

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

func TestAboutBuildMetadataAndMissingInformation(t *testing.T) {
	for _, test := range []struct {
		name  string
		info  *debug.BuildInfo
		build string
	}{
		{name: "no build info"},
		{name: "no VCS settings", info: &debug.BuildInfo{}},
		{name: "release", info: &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "429c5f54ea0b283e40c8dcfd7bc0d19300ba1fc8"},
			{Key: "vcs.time", Value: "2026-10-28T07:55:15Z"},
		}}, build: "429c5f5 · 2026-10-28"},
		{name: "revision without a date", info: &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "123456789"},
			{Key: "vcs.time", Value: "invalid"},
		}}, build: "1234567"},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := readAboutFacts(test.info)
			if facts.build != test.build {
				t.Fatalf("build=%q, want %q", facts.build, test.build)
			}
			for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
				want := test.build
				if want == "" {
					want = i18n.For(language).About.BuildUnavailable
				}
				if got := facts.buildText(i18n.For(language).About); got != want {
					t.Fatalf("build text=%q, want %q", got, want)
				}
			}
			if test.build != "" && !strings.Contains(facts.copyText, "build: "+test.build+"\n") {
				t.Fatal("copied information lost the build identity")
			}
		})
	}
}

func TestAboutShowsTheBuildAndOpensTheCommunityLinks(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	var opened []string
	state.options.OpenURL = func(url string) error { opened = append(opened, url); return nil }

	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAbout)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}

	about := state.messages.About
	if state.settingsCategoryName(settingsAbout) != about.Title {
		t.Fatalf("the category is named %q", state.settingsCategoryName(settingsAbout))
	}
	facts := aboutFacts()
	tools := state.messages.Tools
	for _, want := range []string{about.Tagline, about.InfoTitle, about.UpdateTitle, tools.GeneralUpdateStartup, tools.GeneralUpdate, about.CommunityTitle, about.ProjectTitle, about.QQ, about.WeChat, about.Discord, about.Viva, facts.platform, facts.runtime} {
		if !contains(texts, want) {
			t.Errorf("the About page does not paint %q", want)
		}
	}
	for _, id := range []gfx.ID{hitAboutCopyInfo, hitSettingsUpdateStartToggle, hitSettingsUpdateCheck, hitAboutQQ, hitAboutWeChat, hitAboutDiscord, hitAboutIssues, hitAboutRepo, hitAboutDocs, hitAboutViva} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Errorf("the About page does not paint %d", id)
		}
	}

	state.Click(hitAboutCopyInfo)
	for _, want := range []string{"Motty " + facts.version, "platform: " + facts.platform, "runtime: " + facts.runtime} {
		if !strings.Contains(copied, want) {
			t.Errorf("the copied version info %q does not name %q", copied, want)
		}
	}
	if state.status != about.Copied {
		t.Errorf("copy status = %q", state.status)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	if hit, ok := hitByID(&canvas, hitAboutCopyInfo); !ok || hit.Label != about.CopiedAction {
		t.Errorf("the copy button says %q after copying, want %q", hit.Label, about.CopiedAction)
	}
	// A new visit to the page asks again with the plain label.
	state.selectSettingsCategory(settingsGeneral)
	state.selectSettingsCategory(settingsAbout)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	if hit, ok := hitByID(&canvas, hitAboutCopyInfo); !ok || hit.Label != about.CopyAction {
		t.Errorf("the copy button says %q on a new visit, want %q", hit.Label, about.CopyAction)
	}

	for _, link := range []struct {
		id  gfx.ID
		url string
	}{
		{hitAboutQQ, aboutQQURL},
		{hitAboutWeChat, aboutWeChatURL},
		{hitAboutDiscord, aboutDiscordURL},
		{hitAboutIssues, aboutIssuesURL},
		{hitAboutRepo, aboutRepoURL},
		{hitAboutDocs, aboutDocsURL},
		{hitAboutViva, aboutVivaURL},
	} {
		opened = nil
		state.Click(link.id)
		if len(opened) != 1 || opened[0] != link.url {
			t.Errorf("%d opened %v, want %q", link.id, opened, link.url)
		}
	}
}
