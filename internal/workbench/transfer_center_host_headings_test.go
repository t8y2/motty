package workbench

import "testing"

// Two servers' tasks used to read as one list: the reader could not tell whose
// task was whose without opening the details of each row. With every server in
// view each section now names the server, and a single chosen server keeps the
// plain list it had.
func TestTransferCenterNamesTheServerOfEachRunOfTasks(t *testing.T) {
	state := transferPreviewState(t)
	state.describeTransferScope()

	headers := map[string]int{}
	sections := 0
	for _, row := range state.transfers.rows {
		if row.entry < 0 && !row.hostHeader {
			sections++
			continue
		}
		if row.hostHeader {
			headers[row.host]++
		}
	}
	if sections == 0 {
		t.Fatal("no sections were built")
	}
	if len(headers) != 2 {
		t.Fatalf("server headings = %v, want the two servers of the preview", headers)
	}
	for _, label := range []string{"docker-ubuntu · motty@127.0.0.1:2222", "docker-uos · motty@127.0.0.1:2222"} {
		if headers[label] == 0 {
			t.Fatalf("missing a heading for %q: %v", label, headers)
		}
	}
	// Each entry row must follow the heading of its own server.
	seen := ""
	for _, row := range state.transfers.rows {
		if row.hostHeader {
			seen = row.host
			continue
		}
		if row.entry < 0 {
			seen = ""
			continue
		}
		entry := &state.transfers.entries[row.entry]
		if want := state.transfers.entryHostLabel(entry); seen != want {
			t.Fatalf("a task of %q sits under the heading %q", want, seen)
		}
	}

	// With every task on one server there is nothing to tell apart.
	for index := range state.transfers.entries {
		state.transfers.entries[index].server = "docker-ubuntu"
	}
	state.describeTransferScope()
	for _, row := range state.transfers.rows {
		if row.hostHeader {
			t.Fatalf("one server still got a heading: %q", row.host)
		}
	}
}
