package workbench

import (
	"fmt"
	"os"
	"strings"
	"testing"

	sshknownhosts "golang.org/x/crypto/ssh/knownhosts"
)

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("known-hosts-selected", func(t *testing.T, s *State) {
			g5KnownHosts(t, s)
			s.Click(hitKnownHosts)
			s.Click(hitKnownHostRowList.At(0))
			s.Click(hitKnownHostCheckList.At(1))
		}),
		plainScene("known-hosts-paginated", func(t *testing.T, s *State) {
			writeKnownHostsPaginationFixture(t, s)
			s.Click(hitKnownHosts)
			s.Click(hitKnownHostsNext)
		}),
	)
}

// writeKnownHostsPaginationFixture writes one more entry than a page holds, so
// the footer declares its page buttons and the second page holds the last entry.
func writeKnownHostsPaginationFixture(t *testing.T, s *State) {
	t.Helper()
	key := workbenchKnownHostKey(t)
	var data strings.Builder
	for index := range knownHostsPageSize + 1 {
		data.WriteString(sshknownhosts.Line([]string{fmt.Sprintf("host-%02d.example.com", index)}, key))
		data.WriteByte('\n')
	}
	if err := os.WriteFile(s.options.KnownHostsPath, []byte(data.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}
