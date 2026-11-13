package pathcomp

import (
	"strconv"
	"testing"
)

// The workbench asks this package about the line after every prompt-sized
// output burst, so parsing must stay cheap compared with the screen work
// around it.
func BenchmarkParsePathLine(b *testing.B) {
	line := "cat /var/log/nginx/access"
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, ok := Parse(line); !ok {
			b.Fatal("line refused")
		}
	}
}

// Candidates walks the whole listing and escapes the matching names; this is
// the steady-state cost of one filtered popup.
func BenchmarkCandidates(b *testing.B) {
	ctx, ok := Parse("cd /home/me/pro")
	if !ok || !ctx.Path {
		b.Fatal("line is not a path position")
	}
	entries := make([]Entry, 0, 2048)
	for index := 0; index < 2048; index++ {
		entries = append(entries, Entry{Name: "project-" + strconv.Itoa(index), Dir: index%4 == 0})
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		Candidates(ctx, entries)
	}
}
