package workbench

import (
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	sshknownhosts "golang.org/x/crypto/ssh/knownhosts"
)

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("g5-known-hosts", func(t *testing.T, s *State) {
			g5KnownHosts(t, s)
			s.Click(hitKnownHosts)
		}),
		plainScene("g5-known-hosts-search", func(t *testing.T, s *State) {
			g5KnownHosts(t, s)
			s.Click(hitKnownHosts)
			s.knownHosts.search.Insert("primary")
			s.knownHosts.synced = false
		}),
		g5SessionLogsScene("g5-session-logs", ""),
		g5SessionLogsScene("g5-session-logs-search", "web"),
	)
}

func g5SessionLogsScene(name, query string) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state := sessionLogsState(t, true)
		if query != "" {
			state.sessionLogs.view.search.Insert(query)
		}
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

// g5KnownHosts writes a known_hosts file without a hashed name: a hash is salted at random,
// which would make the snapshot differ from run to run.
func g5KnownHosts(t *testing.T, s *State) {
	key := workbenchKnownHostKey(t)
	data := strings.Join([]string{
		sshknownhosts.Line([]string{"plain.example.com"}, key) + " primary server",
		sshknownhosts.Line([]string{"other.example.com"}, key) + " second",
	}, "\n") + "\n"
	if err := os.WriteFile(s.options.KnownHostsPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
