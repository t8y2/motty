package term

import "testing"

func TestWordRangeSeparatedBy(t *testing.T) {
	for _, test := range []struct {
		name, text, separators string
		column                 int
		want                   string
	}{
		{"built-in rule stops at punctuation", "/usr/local/bin x", "", 6, "local"},
		{"a path is one word", "/usr/local/bin x", ":", 6, "/usr/local/bin"},
		{"an address is one word", "at 192.168.1.1, ok", ",", 5, "192.168.1.1"},
		{"a listed character ends the word", "host:8080", ":", 1, "host"},
		{"the other side of it", "host:8080", ":", 6, "8080"},
		{"the separator itself", "a::b", ":", 1, "::"},
		{"letters can be separators", "fooXbar", "X", 1, "foo"},
		{"a blank always ends a word", "ab cd", ":", 0, "ab"},
		{"a wide character is a word character", "世界/路径 x", ":", 0, "世界/路径"},
		{"a wide separator", "世界，路径", "，", 0, "世界"},
	} {
		screen := New(40, 2, 0)
		screen.Write([]byte(test.text))
		point := screen.PointAt(Position{Row: 0, Column: test.column})
		start, end := screen.WordRangeSeparatedBy(point, test.separators)
		if got := screen.SelectedRange(start, end); got != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
}

// An empty value is exactly the built-in rule, so nobody's double click changes
// until they ask for it.
func TestWordRangeIsTheBuiltInRule(t *testing.T) {
	screen := New(40, 2, 0)
	screen.Write([]byte("go_name 世界 !! a/b"))
	for column := range 20 {
		point := screen.PointAt(Position{Row: 0, Column: column})
		start, end := screen.WordRange(point)
		otherStart, otherEnd := screen.WordRangeSeparatedBy(point, "")
		if start != otherStart || end != otherEnd {
			t.Errorf("column %d differs", column)
		}
	}
}
