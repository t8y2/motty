package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

// A deletion in a detached files window must look like work in progress: the
// status line animates a spinner beside the entry being deleted.
func TestDetachedSFTPWindowAnimatesWhileDeleting(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	w := &sftpWindow{owner: state, session: *session, view: session.files}
	w.ui.split = ui.SplitterState{Ratio: .5}
	w.view.lfolder.path = filepath.Clean(os.TempDir())
	w.view.lfolder.rows = prepareSFTPRows(nil, []sftpclient.Entry{{Name: "local.txt"}})
	w.view.busy = true
	w.view.status = fmt.Sprintf(state.messages.SFTPDeletingFmt, "notes.txt")

	var canvas gfx.Canvas
	w.paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if _, animating := canvas.NextFrame(); !animating {
		t.Fatal("the deleting status line does not animate a spinner")
	}
	if !canvasHasText(&canvas, w.view.status) {
		t.Fatal("the deleting status line does not name the entry")
	}
}
