package workbench

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
)

// Two small actions on a terminal's text: keeping what it shows, with its
// scrollback, as a text file, and looking the selection up on the web.

// saveTerminalOutput writes the terminal's retained text (scrollback and screen)
// to a file the user chooses.
func (state *State) saveTerminalOutput(session *Session) {
	m := state.messages.Tools
	data := session.term.screen.AppendRetainedText(nil)
	if len(data) == 0 {
		state.showActionInfo(m.OutputSaveTitle, m.OutputEmpty)
		return
	}
	data = append(data, '\n')
	path, err := state.options.SaveFile(app.SaveFileOptions{
		Title:            m.OutputSaveTitle,
		SuggestedName:    terminalOutputName(session.Title, time.Now()),
		DefaultExtension: "txt",
		Filters:          []app.FileFilter{{Name: "Text", Pattern: "*.txt"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err != nil {
		state.showActionFailure(m.OutputSaveTitle, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	if err = writePrivateAtomic(path, data); err != nil {
		state.showFileSaveFailure(m.OutputSaveTitle, err)
		return
	}
	state.status = fmt.Sprintf(m.OutputSavedFmt, path)
}

func terminalOutputName(title string, at time.Time) string {
	return exportFileName(title, "output", "txt", at)
}

// searchSelection opens a web search for the selected text, as one line.
func (state *State) searchSelection(session *Session) {
	selection := session.term.selection
	if !selection.selected {
		return
	}
	query := strings.Join(strings.Fields(selection.text(session.term.screen)), " ")
	if len(query) > maxSearchQuery {
		query = strings.ToValidUTF8(query[:maxSearchQuery], "")
	}
	if query == "" {
		return
	}
	state.openExternal("https://www.bing.com/search?q=" + url.QueryEscape(query))
}
