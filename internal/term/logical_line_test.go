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

func TestLogicalLineRowsReturnsWrappedRows(t *testing.T) {
	screen := newTerm(10, 6)
	// 25 characters wrap onto 3 rows at 10 columns: 10 + 10 + 5
	feed(screen, "abcdefghijklmnopqrstuvwxy\r\nnext line\r\n")

	if screen.RowIsWrapped(-1) || screen.RowIsWrapped(5) {
		t.Fatal("out of bounds row reported as wrapped")
	}
	if !screen.RowIsWrapped(0) || !screen.RowIsWrapped(1) || !screen.RowIsWrapped(2) {
		t.Fatalf("rows 0, 1, 2 should be wrapped: %v, %v, %v (rows: %q, %q, %q, %q)",
			screen.RowIsWrapped(0), screen.RowIsWrapped(1), screen.RowIsWrapped(2),
			screen.RowText(0), screen.RowText(1), screen.RowText(2), screen.RowText(3))
	}
	if screen.RowIsWrapped(3) {
		t.Fatal("row 3 should not be wrapped")
	}

	for row := 0; row < 3; row++ {
		rows := screen.LogicalLineRows(row)
		if len(rows) != 3 {
			t.Fatalf("row %d: got %d logical rows, want 3", row, len(rows))
		}
		if rows[0].ID != screen.Row(0).ID || rows[1].ID != screen.Row(1).ID || rows[2].ID != screen.Row(2).ID {
			t.Fatalf("row %d: unexpected row IDs in logical line: %+v", row, rows)
		}
	}

	rows3 := screen.LogicalLineRows(3)
	if len(rows3) != 1 || rows3[0].ID != screen.Row(3).ID {
		t.Fatalf("row 3: got %d rows, want 1", len(rows3))
	}

	// Test scrollback: line starts in scrollback and continues onto view row 0.
	scroll := newTerm(10, 2)
	// 20 characters wrap onto 2 rows; then "line3\r\n" scrolls the first row into scrollback.
	feed(scroll, "0123456789abcdefghij\r\nline3\r\n")
	// Now view row 0 is "line3", but let's scroll up 1 so view row 0 is the second wrapped row:
	scroll.Scroll(1)
	if !scroll.RowIsWrapped(0) {
		t.Fatalf("view row 0 should be wrapped from scrollback")
	}
	scrollRows := scroll.LogicalLineRows(0)
	if len(scrollRows) != 2 {
		t.Fatalf("got %d rows from scrollback logical line, want 2", len(scrollRows))
	}
}
