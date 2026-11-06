package completion

import "testing"

func TestParsePathQueryAcceptsOnlySimpleFileArguments(t *testing.T) {
	for _, test := range []struct {
		line, cwd, directory, prefix, word string
		foldersOnly                        bool
	}{
		{"tail -f /var/lo", "/home/user", "/var", "lo", "/var/lo", false},
		{"cd ./Do", "/home/user", "/home/user", "Do", "./Do", true},
		{"cat report", "/home/user", "/home/user", "report", "report", false},
		{"cd /var/", "/home/user", "/var", "", "/var/", true},
		{"tail ../er", "/home/user", "/home", "er", "../er", false},
	} {
		query, ok := ParsePathQuery(test.line, test.cwd)
		if !ok || query.Directory != test.directory || query.Prefix != test.prefix || query.Word != test.word || query.DirectoriesOnly != test.foldersOnly {
			t.Errorf("query %q cwd %q = %+v ok=%v", test.line, test.cwd, query, ok)
		}
	}
}

func TestParsePathQueryRejectsUnprovenOrComplexInput(t *testing.T) {
	for _, test := range []struct{ line, cwd string }{
		{"tail -f /var/lo", ""},
		{"cat report", "relative"},
		{"cat report", "/tmp/\nunsafe"},
		{"cat report", "/tmp/\tunsafe"},
		{"tail -f \"/var/log with space\"", "/tmp"},
		{"tail -f /var/lo;id", "/tmp"},
		{"tail -f /var/lo$(id)", "/tmp"},
		{"tail -f /var/lo\\g", "/tmp"},
		{"tail  -f /var/lo", "/tmp"},
		{"tail -n 10", "/tmp"},
		{"tail -f -", "/tmp"},
		{"sudo tail -f /var/lo", "/tmp"},
		{"go run main.go", "/tmp"},
		{"cat ", "/tmp"},
		{"tail -f /var/lo\n", "/tmp"},
	} {
		if query, ok := ParsePathQuery(test.line, test.cwd); ok {
			t.Errorf("unsafe query accepted: %+v", query)
		}
	}
}
