package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sessionlog"
)

// The session journal's menus and detail panel are page controls of their own:
// the scenes here compare how they paint open, both beside the records and in
// their place on a narrow card.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		sessionLogsPaintScene("g5-session-logs-more", func(t *testing.T, s *State) {
			s.Click(hitSessionLogsMore)
		}),
		sessionLogsPaintScene("g5-session-logs-filter", func(t *testing.T, s *State) {
			s.Click(hitSessionLogsStatus)
		}),
		sessionLogsPaintScene("g5-session-logs-detail", func(t *testing.T, s *State) {
			s.selectSessionLogRecord("log-00")
			sessionLogsWait(t, s)
		}),
		sessionLogsPaintScene("g5-session-logs-duration", func(t *testing.T, s *State) {
			s.sessionLogs.view.query.Sort = sessionlog.SortDurationDesc
			s.applySessionLogFilters()
			sessionLogsWait(t, s)
		}),
	)
}

func sessionLogsPaintScene(name string, setup func(*testing.T, *State)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state := sessionLogsState(t, true)
		setup(t, state)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}
