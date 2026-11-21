package term

import "testing"

func TestLogicalLineTextJoinsWrapsAndKeepsTheirSpaces(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cols, rows int
		text, want string
	}{
		{"space-at-wrap", 10, 4, "[sudo] password for dev: ", "[sudo] password for dev:"},
		{"wide-wrap-padding", 3, 4, "密码：", "密码："},
		{"combining-mark", 3, 4, "a界e\u0301X", "a界e\u0301X"},
		{"scrollback", 8, 2, "deploy@node.example's password: ", "deploy@node.example's password:"},
		{"hard-newline", 10, 4, "Password:\r\n$ ", "$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			screen := newTerm(tc.cols, tc.rows)
			feed(screen, tc.text)
			got, id, ok := screen.LogicalLineText(screen.Cursor().Y, 1024)
			if !ok || id == 0 || got != tc.want {
				t.Fatalf("logical line = %q, id %d, ok %v; want %q", got, id, ok, tc.want)
			}
		})
	}
}

func TestLogicalLineTextBoundsUntrustedOutput(t *testing.T) {
	screen := newTerm(3, 8)
	feed(screen, "密码：")
	for _, limit := range []int{0, 1, 8} {
		if _, _, ok := screen.LogicalLineText(screen.Cursor().Y, limit); ok {
			t.Fatalf("accepted %d UTF-8 bytes with limit %d", len("密码："), limit)
		}
	}
	if _, _, ok := screen.LogicalLineText(screen.Cursor().Y, len("密码：")); !ok {
		t.Fatal("refused a line exactly at the byte limit")
	}
	for _, row := range []int{-1, 8} {
		if _, _, ok := screen.LogicalLineText(row, 1024); ok {
			t.Fatalf("accepted invalid row %d", row)
		}
	}
}

func TestLogicalLineTextDistinguishesIdenticalQuestions(t *testing.T) {
	screen := newTerm(20, 4)
	feed(screen, "Password: ")
	_, first, _ := screen.LogicalLineText(screen.Cursor().Y, 1024)
	feed(screen, "\x1b[0m")
	_, same, _ := screen.LogicalLineText(screen.Cursor().Y, 1024)
	feed(screen, "\r\nPassword: ")
	_, next, _ := screen.LogicalLineText(screen.Cursor().Y, 1024)
	if first != same || next == first {
		t.Fatalf("prompt identities: first %d, restyled %d, retry %d", first, same, next)
	}
}
