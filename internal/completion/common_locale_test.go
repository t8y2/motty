package completion

import (
	"strings"
	"testing"
	"unicode"
)

func TestCommonDescriptionsCoverAllSixtyFiveRootsInBothChineseVariants(t *testing.T) {
	groups := []string{
		"cat ls cd cp mv rm mkdir chmod chown grep sed awk find head tail sort uniq cut wc tee xargs du df tar gzip",
		"ssh scp rsync curl wget ping ip ss netstat ps kill top systemctl journalctl apt dnf yum",
		"git docker kubectl make gcc go cargo rustc java javac npm yarn pnpm node python pip brew",
		"pwsh powershell cmd dir Get-Command Get-ChildItem",
	}
	for _, language := range []string{"zh-CN", "zh-TW"} {
		for _, group := range groups {
			for _, name := range strings.Fields(group) {
				description := LocalizedDescription(language, name, "")
				if description == "" || !strings.ContainsFunc(description, func(value rune) bool { return unicode.Is(unicode.Han, value) }) || strings.ContainsAny(description, "\r\n\x1b") {
					t.Errorf("%s %s has no safe Chinese description: %q", language, name, description)
				}
			}
		}
	}
	for _, test := range []struct{ root, token string }{
		{"cat", "-n"}, {"cat", "-e"}, {"tail", "-f"}, {"tail", "-n"},
		{"ls", "-a"}, {"ip", "addr"}, {"cmd", "/C"}, {"Get-Command", "-Name"},
	} {
		for _, language := range []string{"zh-CN", "zh-TW"} {
			if got := LocalizedDescription(language, test.root, test.token); got == "" {
				t.Errorf("%s %s %s lacks a translated option", language, test.root, test.token)
			}
		}
	}
	if LocalizedDescription("en", "cat", "") != "" || LocalizedDescription("zh-CN", "unknown", "") != "" {
		t.Fatal("unsupported translation was invented")
	}
}

func TestCommonPriorityKeepsTheAuditedCommandsAheadOfUnrelatedRoots(t *testing.T) {
	if CommonPriority("cat") >= CommonPriority("cargo") || CommonPriority("cargo") >= CommonPriority("calibre") {
		t.Fatal("audited common-command priority was lost")
	}
}

func TestCommonSupplementalSyntaxHasChineseDescriptions(t *testing.T) {
	catalog, err := Common()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range catalog.Roots() {
		for _, language := range []string{"zh-CN", "zh-TW"} {
			for _, flag := range root.Flags {
				for _, token := range []string{flag.Token(), flag.ShortToken()} {
					if token != "" && LocalizedDescription(language, root.Token, token) == "" {
						t.Errorf("%s %s %s lacks localized syntax", language, root.Token, token)
					}
				}
			}
			for _, child := range root.Children {
				if LocalizedDescription(language, root.Token, child.Token) == "" {
					t.Errorf("%s %s %s lacks localized subcommand", language, root.Token, child.Token)
				}
			}
		}
	}
}
