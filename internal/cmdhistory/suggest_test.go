package cmdhistory

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A lead shared only as far as the middle of a character is not something the
// user could type, so it must not come back torn.
func TestCommonPrefixStopsOnAWholeCharacter(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"nothing to share", []string{"git status", "ls -l"}, ""},
		{"plain ascii", []string{"git status", "git stash"}, "git sta"},
		{"one string", []string{"git status"}, "git status"},
		{"no strings", nil, ""},
		{"identical", []string{"git status", "git status"}, "git status"},
		// "路" and "请" share their first lead byte (0xE8) but not a character.
		{"shared lead byte only", []string{"路由", "请求"}, ""},
		// "路径" and "路线" share 路 (3 bytes) and then differ.
		{"whole characters only", []string{"路径状态", "路线状态"}, "路"},
		{"multibyte tail kept whole", []string{"docker 镜像", "docker 镜"}, "docker 镜"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CommonPrefix(c.in)
			if got != c.want {
				t.Fatalf("CommonPrefix(%q) = %q, want %q", c.in, got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("CommonPrefix(%q) = %q is not valid UTF-8", c.in, got)
			}
		})
	}
}

// SuggestAll is the suffix view of SuggestAllFull: one guard, one match loop,
// one cap, so the inline ghost and the list cannot drift apart.
func TestSuggestAllAgreesWithSuggestAllFull(t *testing.T) {
	entries := []Entry{
		{Command: "git status --short", Used: time.Unix(300, 0)},
		{Command: "git stash list", Used: time.Unix(200, 0)},
		{Command: "ip route get 8.8.8.8", Used: time.Unix(100, 0)},
	}
	const line = "git st"
	full := SuggestAllFull(entries, line, 5)
	suffixes := SuggestAll(entries, line, 5)
	if len(full) != len(suffixes) {
		t.Fatalf("SuggestAllFull gave %d matches, SuggestAll gave %d", len(full), len(suffixes))
	}
	for index, entry := range full {
		if want := strings.TrimPrefix(entry.Command, line); suffixes[index] != want {
			t.Fatalf("match %d = %q, want %q", index, suffixes[index], want)
		}
	}
	// The guards stay on one path: a secret, a newline, and an empty line give
	// nothing from either view.
	for _, blocked := range []string{"", "git\nstatus", "mysql -u root -pSecret"} {
		if got := SuggestAllFull(entries, blocked, 5); len(got) != 0 {
			t.Fatalf("SuggestAllFull(%q) = %v, want nothing", blocked, got)
		}
		if got := SuggestAll(entries, blocked, 5); len(got) != 0 {
			t.Fatalf("SuggestAll(%q) = %v, want nothing", blocked, got)
		}
	}
	if got := SuggestAllFull(entries, line, 0); len(got) != 0 {
		t.Fatalf("a zero cap gave %d matches", len(got))
	}
}
