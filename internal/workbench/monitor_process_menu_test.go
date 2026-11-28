//go:build darwin || windows || linux

package workbench

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/remotetools"
	"motty/internal/sshconn"
)

// monitorProcessMenuFixture opens the panel on its process page with two known
// rows, scrolled so that the rows are painted and hit.
func monitorProcessMenuFixture(t *testing.T) (*State, *Session, *headless.Session) {
	t.Helper()
	state, session := monitorFixture(t)
	p := &session.stats.panel
	p.page = 1
	p.procsOpen, p.memoryOpen = true, false
	p.procs = append(p.procs[:0],
		monitorProcessText{name: "nginx", pid: 812, cpu: "0.7%", memory: "1.2%", rss: "128.4 MB", cpuValue: .7, memoryValue: 1.2},
		monitorProcessText{name: "postgres", pid: 1100, cpu: "4.9%", memory: "8.3%", rss: "640.2 MB", cpuValue: 4.9, memoryValue: 8.3},
	)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Monitor", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	// The process card is the last card of a tall page: bring it into the
	// viewport so its rows register their hits.
	p.scroll = state.monitorScrollArea(p).Geometry().MaxOffsetY
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	return state, session, view
}

// guardMarker is the acknowledgement token a process-guard command carries.
var guardMarker = regexp.MustCompile(`motty-guard-[0-9a-f]{32}:`)

func TestMonitorProcessRightClickCopiesThePID(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	var copied []string
	state.options.SetClipboardText = func(value string) error {
		copied = append(copied, value)
		return nil
	}
	if err := view.RightClickLabel("button:nginx"); err != nil {
		t.Fatal(err)
	}
	if !session.stats.panel.procMenu || session.stats.panel.procMenuPID != 812 {
		t.Fatalf("right click did not open the row's menu: %+v", session.stats.panel.procMenuPID)
	}
	dualClickMenuLabel(t, view, state.messages.Tools.RemoteProcessCopyPID)
	if len(copied) != 1 || copied[0] != "812" {
		t.Fatalf("copy did not write the pid: %q", copied)
	}
	if !view.HasText(state.messages.StatusClipboardCopied) {
		t.Fatal("copy said nothing in the footer")
	}
}

func TestMonitorProcessRightClickConfirmsATerminate(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	if err := view.RightClickLabel("button:postgres"); err != nil {
		t.Fatal(err)
	}
	dualClickMenuLabel(t, view, state.messages.Tools.RemoteTerminate)
	if state.modal != modalForm || state.form.title != state.messages.Tools.RemoteConfirmTerminateTitle {
		t.Fatalf("terminate did not ask first: modal=%v title=%q", state.modal, state.form.title)
	}
	if !strings.Contains(state.form.description, "postgres") || !strings.Contains(state.form.description, "1100") {
		t.Fatalf("the confirmation does not name the process: %q", state.form.description)
	}
	state.click(hitFormCancel, true)
	if state.modal != modalNone {
		t.Fatalf("cancel left modal %v", state.modal)
	}
	if session.stats.panel.procMenu {
		t.Fatal("the row menu stayed open behind the cancelled confirmation")
	}
}

func TestMonitorProcessRightClickIgnoresNonRows(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	rect := state.monitorRect
	if err := view.RightClick(rect.X+4, rect.Y+4); err != nil {
		t.Fatal(err)
	}
	if session.stats.panel.procMenu {
		t.Fatal("a press outside the list opened a process menu")
	}
}

func TestMonitorProcessTerminateSignalsTheInstanceBehindThePID(t *testing.T) {
	state, session, _ := monitorProcessMenuFixture(t)
	fake, ok := session.conn.(*fakeConn)
	if !ok {
		t.Fatal("this fixture no longer runs on a scripted connection")
	}
	fake.mu.Lock()
	fake.runCommand = func(_ context.Context, command string, _ int) ([]byte, error) {
		if strings.Contains(command, "motty-process-identities") {
			return []byte("boot 12345678-1234-1234-1234-123456789abc\n812 4242\n"), nil
		}
		// The guard acknowledges with the random marker it generated, so the
		// answer has to echo the token of the command it was asked.
		if marker := guardMarker.FindString(command); marker != "" {
			return []byte(marker + "ok\n"), nil
		}
		return nil, nil
	}
	fake.mu.Unlock()

	state.startMonitorProcessSignal(session, 812, remotetools.Terminate)
	p := &session.stats.panel
	deadline := time.Now().Add(2 * time.Second)
	for p.actionStatus == state.messages.Tools.RemoteLoading && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if p.actionFailed {
		t.Fatalf("terminate failed: %s", p.actionStatus)
	}
	if want := fmt.Sprintf(state.messages.Tools.RemoteSignalSentFmt, "TERM", 812); p.actionStatus != want {
		t.Fatalf("footer says %q, want %q", p.actionStatus, want)
	}
	fake.mu.Lock()
	calls := append([]string(nil), fake.commandCalls...)
	fake.mu.Unlock()
	signalled := false
	for _, call := range calls {
		if strings.Contains(call, "kill -TERM 812 2>&1") {
			signalled = true
		}
	}
	if !signalled {
		t.Fatalf("no signal reached the host: %q", calls)
	}
}

func TestMonitorProcessSignalRefusesAnInstanceTheHostCannotProve(t *testing.T) {
	state, session, _ := monitorProcessMenuFixture(t)
	fake, ok := session.conn.(*fakeConn)
	if !ok {
		t.Fatal("this fixture no longer runs on a scripted connection")
	}
	fake.mu.Lock()
	fake.runCommand = func(context.Context, string, int) ([]byte, error) {
		return nil, errors.New("no procfs")
	}
	fake.mu.Unlock()

	state.startMonitorProcessSignal(session, 812, remotetools.Kill)
	p := &session.stats.panel
	deadline := time.Now().Add(2 * time.Second)
	for p.actionStatus == state.messages.Tools.RemoteLoading && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if !p.actionFailed || p.actionStatus != state.messages.Tools.RemoteProcessUnverifiable {
		t.Fatalf("an unprovable instance was signalled: %+v", p.actionStatus)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, call := range fake.commandCalls {
		if strings.HasPrefix(call, "kill ") {
			t.Fatalf("a signal was attempted anyway: %q", call)
		}
	}
}

func TestMonitorProcessMenuIsReadOnlyOnAHostThatCannotProveInstances(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	session.host.System = sshconn.SystemDarwin
	if err := view.RightClickLabel("button:nginx"); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	disabled := map[string]bool{}
	for _, hit := range view.Snapshot().Hits {
		if hit.Role == gfx.RoleMenuItem {
			disabled[hit.Label] = hit.Disabled
		}
	}
	if !disabled[state.messages.Tools.RemoteTerminate] || !disabled[state.messages.Tools.RemoteForceKill] {
		t.Fatalf("a macOS host offered a signal it would refuse: %v", disabled)
	}
	if disabled[state.messages.Tools.RemoteProcessCopyPID] {
		t.Fatal("copying the pid needs no instance identity")
	}
}

// monitorGuardBoot is the boot identity the scripted host reports.
const monitorGuardBoot = "12345678-1234-1234-1234-123456789abc"

// scriptMonitorProcessHost answers the identity probe, the snapshot and the
// complete command of the fixture's nginx row.
func scriptMonitorProcessHost(t *testing.T, fake *fakeConn) {
	t.Helper()
	fake.mu.Lock()
	fake.runCommand = func(_ context.Context, command string, _ int) ([]byte, error) {
		switch {
		case strings.Contains(command, "motty-process-identities"):
			return []byte("boot " + monitorGuardBoot + "\n812 4242\n"), nil
		case strings.Contains(command, "ps -e -o pid="):
			return []byte("812 1 www-data S 0.7 1.2 131481 900000 01:02:03 nginx: worker process\n"), nil
		case strings.Contains(command, "motty-process-command"):
			cmdline := base64.StdEncoding.EncodeToString([]byte("nginx\x00-g\x00daemon\x00"))
			return []byte("motty-command-before " + monitorGuardBoot + " 4242\n" + cmdline +
				"\nmotty-command-after " + monitorGuardBoot + " 4242\n"), nil
		}
		return nil, nil
	}
	fake.mu.Unlock()
}

func TestMonitorProcessDetailsOpensTheToolsDialog(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	scriptMonitorProcessHost(t, session.conn.(*fakeConn))
	if err := view.RightClickLabel("button:nginx"); err != nil {
		t.Fatal(err)
	}
	dualClickMenuLabel(t, view, state.messages.Tools.RemoteProcessDetails)
	for deadline := time.Now().Add(2 * time.Second); !state.remote.processDetails.open && time.Now().Before(deadline); {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	details := &state.remote.processDetails
	if !details.open || !details.fromMonitor {
		t.Fatalf("the monitor did not open the details: %+v", details.open)
	}
	if details.process.PID != 812 || details.process.User != "www-data" {
		t.Fatalf("details are for another process: %+v", details.process)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if !view.HasText(state.messages.Tools.RemoteProcessDetailsTitle) || !view.HasText("www-data") {
		t.Fatal("the identical details dialog was not painted")
	}
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if details.open {
		t.Fatal("escape did not close the monitor's details")
	}
}

func TestMonitorProcessDetailsFooterConfirmsTheSignal(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	scriptMonitorProcessHost(t, session.conn.(*fakeConn))
	if err := view.RightClickLabel("button:nginx"); err != nil {
		t.Fatal(err)
	}
	dualClickMenuLabel(t, view, state.messages.Tools.RemoteProcessDetails)
	for deadline := time.Now().Add(2 * time.Second); !state.remote.processDetails.open && time.Now().Before(deadline); {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel("button:" + state.messages.Tools.RemoteTerminate); err != nil {
		t.Fatal(err)
	}
	if state.remote.processDetails.open {
		t.Fatal("the details stayed open behind the confirmation")
	}
	if state.modal != modalForm || !strings.Contains(state.form.description, "812") {
		t.Fatalf("the footer did not confirm the signal: modal=%v description=%q", state.modal, state.form.description)
	}
}

func TestMonitorProcessSearchNarrowsTheList(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	p := &session.stats.panel
	p.procs = append(p.procs,
		monitorProcessText{name: "dockerd", pid: 733, cpu: "1.9%", memory: "2.1%", rss: "212.0 MB", cpuValue: 1.9, memoryValue: 2.1},
	)
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	// The rows are addressed by the tab's own list, so a search that keeps a
	// row is the row that can still be pressed.
	if err := view.ClickLabel("textfield:" + state.messages.Monitor.ProcessSearch); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("dock"); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if len(p.procShown) != 1 || p.procShown[0].name != "dockerd" {
		t.Fatalf("search kept %d rows: %+v", len(p.procShown), p.procShown)
	}
	if _, ok := view.Hit("button:nginx"); ok {
		t.Fatal("a filtered-out row is still a control")
	}
	// A pid is a row too: the box matches it as well.
	p.procSearch.SetText("1100")
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if len(p.procShown) != 1 || p.procShown[0].pid != 1100 {
		t.Fatalf("pid search kept %+v", p.procShown)
	}
	p.procSearch.SetText("")
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if len(p.procShown) != len(p.procs) {
		t.Fatalf("clearing the query kept %d of %d rows", len(p.procShown), len(p.procs))
	}
}

func TestMonitorProcessSearchSitsBetweenMemoryAndTheList(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	p := &session.stats.panel
	// A collapsed cores card and an open memory card keep the meters, the box
	// and the list in one viewport, which is what the order is checked on.
	p.memoryOpen, p.coresCollapsed = true, true
	p.scroll = 0
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	field, err := view.Locate("textfield:" + state.messages.Monitor.ProcessSearch)
	if err != nil {
		t.Fatal(err)
	}
	memory, err := view.Locate("button:" + state.messages.Monitor.MemoryInfoSection)
	if err != nil {
		t.Fatal(err)
	}
	list, err := view.Locate("button:" + state.messages.Monitor.ProcessSection)
	if err != nil {
		t.Fatal(err)
	}
	// The box lives inside the process card, under its header: below the memory
	// card's bottom edge and below the header, and above the rows it narrows.
	if !(field.Bounds.Y > memory.Bounds.Y+32 && field.Bounds.Y > list.Bounds.Y+32) {
		t.Fatalf("search box %+v is not inside the process card under its header %+v (memory %+v)", field.Bounds, list.Bounds, memory.Bounds)
	}
	row, err := view.Locate("button:nginx")
	if err != nil {
		t.Fatal(err)
	}
	// The declared card must be exactly as wide as the painted one above it:
	// same inset from the content edge on both sides.
	if d := list.Bounds.X - memory.Bounds.X; d < 0 || d > 1 {
		t.Fatalf("the process card starts %v from the memory card's edge", d)
	}
	if d := (memory.Bounds.X + memory.Bounds.Width) - (list.Bounds.X + list.Bounds.Width); d < 0 || d > 1 {
		t.Fatalf("the process card ends %v from the memory card's edge", d)
	}
	// The card sits one gap below the memory card, like every other pair of
	// cards on the page.
	// The card's own border takes the first of the gap's pixels, so the header
	// inside it starts one further down.
	if gap := list.Bounds.Y - (memory.Bounds.Y + monitorMemoryHeight(p)); gap < monitorGap || gap > monitorGap+1 {
		t.Fatalf("the process card is %v below the memory card, want %v", gap, monitorGap)
	}
	if row.Bounds.Y < field.Bounds.Y+field.Bounds.Height {
		t.Fatalf("a row %+v is not below the search box %+v", row.Bounds, field.Bounds)
	}
	if !view.HasText(state.messages.Monitor.ProcessFullListHint) {
		t.Fatal("the page does not point at the full process list")
	}
}

func TestMonitorProcessCardKeepsItsHeaderWhenScrolled(t *testing.T) {
	state, session, view := monitorProcessMenuFixture(t)
	p := &session.stats.panel
	p.memoryOpen, p.coresCollapsed, p.procsOpen = true, true, true
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	// A scroll used to shift each slot's culling window by the slot's own y, so
	// the card's header and surface fell outside it and vanished.
	p.scroll = 120
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Locate("button:" + state.messages.Monitor.ProcessSection); err != nil {
		t.Fatal("the card header vanished after a scroll")
	}
	if _, err := view.Locate("textfield:" + state.messages.Monitor.ProcessSearch); err != nil {
		t.Fatal("the search box vanished after a scroll")
	}
}
