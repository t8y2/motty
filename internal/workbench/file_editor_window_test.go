package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
)

func TestASlowDocumentDoesNotTakeOverTheTabOpenedAfterIt(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() {
		host.Quit()
		app.UseApplicationRunner(nil)
	})
	if err := app.RunApplication(func(application *app.Application) error {
		state := newTestState(t, testOptions(t))
		state.options.OpenContentWindow = application.OpenContentWindow
		manager := &state.fileWindows
		slow := &fileDocument{owner: state, cacheKey: "1:false:/slow.txt", location: "/slow.txt", name: "slow.txt"}
		fast := &fileDocument{owner: state, cacheKey: "1:false:/fast.txt", location: "/fast.txt", name: "fast.txt"}
		manager.documents = map[string]*fileDocument{slow.cacheKey: slow, fast.cacheKey: fast}
		// The document opened last finishes loading first.
		manager.pending = fast.cacheKey
		if err := manager.present(fast); err != nil {
			return err
		}
		// The earlier, slower load finishing afterwards must not take the tab.
		if err := manager.present(slow); err != nil {
			return err
		}
		if manager.active != fast.cacheKey {
			t.Fatalf("active tab = %q, want the document opened last", manager.active)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
