//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestNetworkRuleListsSearchScrollAndRemoveMatchingSelection(t *testing.T) {
	const total = 40
	for _, section := range []string{"forwards", "jump routes"} {
		t.Run(section, func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			list, ids := &state.forwardList, forwardListIDs
			remove := hitForwardRemove
			ruleCount := func() int { return len(state.forwards) }
			label := func(i int) string {
				if i%2 == 0 {
					return fmt.Sprintf("Lab %02d", i)
				}
				return fmt.Sprintf("Production %02d", i)
			}
			if section == "forwards" {
				state.Click(hitPortForwarding)
				host := state.hosts[0]
				for i := range total {
					state.forwards = append(state.forwards, forwardRecord{
						ID: uint32(i + 1), Label: label(i), HostLabel: host.Label, HostAddress: host.Address,
						HostPort: host.Port, HostUser: host.User, LocalPort: uint16(8000 + i),
						DestinationHost: "localhost", DestinationPort: 80,
					})
				}
				state.refreshForwards()
			} else {
				state.Click(hitProxies)
				list, ids = &state.proxyList, proxyListIDs
				remove = hitProxyRemove
				ruleCount = func() int { return len(state.proxies) }
				jump := state.hosts[0].ID
				for i := range total {
					host := Host{ID: uint32(i + 100), Label: label(i), Address: fmt.Sprintf("host-%d.example", i), Port: 22}
					host.finish()
					state.hosts = append(state.hosts, host)
					state.proxies = append(state.proxies, proxyRecord{ID: uint32(i + 1), TargetID: host.ID, JumpID: jump})
				}
				state.refreshProxies()
			}

			var input app.TextInput
			view, err := headless.Open(state.WindowConfig(app.Config{
				Width: 1000, Height: 620, Tree: Tree(), Paint: state.Paint,
				OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize,
				TextInput: func(ctx app.TextInputContext) *app.TextInput {
					if editor := state.EditorFor(ctx.ID); editor != nil {
						input = app.TextInput{Editor: editor}
						return &input
					}
					return nil
				},
			}), nil, headless.Options{Scale: 1.25})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			hit := func(id gfx.ID) (gfx.Hit, bool) {
				for _, hit := range view.Snapshot().Hits {
					if hit.ID == id && !hit.IsLabel {
						return hit, true
					}
				}
				return gfx.Hit{}, false
			}
			click := func(id gfx.ID) {
				t.Helper()
				h, ok := hit(id)
				if !ok {
					t.Fatalf("missing hit %v", id)
				}
				must(view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
				must(view.Rest())
			}

			header, _ := hit(ids.all)
			if _, ok := hit(ids.rows.At(total)); ok {
				t.Fatal("last row has a hit outside the viewport")
			}
			body := list.geometry.Viewport
			must(view.Scroll(gfx.ScrollEvent{X: body.X + 10, Y: body.Y + 10, DeltaY: 10000}))
			if _, ok := hit(ids.rows.At(total)); !ok || list.scroll != list.geometry.MaxOffsetY || list.scroll == 0 {
				t.Fatal("wheel did not reach the last row")
			}
			if after, _ := hit(ids.all); after.Bounds != header.Bounds {
				t.Fatal("table header moved with the rows")
			}
			click(ids.checks.At(total))
			if !list.checked[total] {
				t.Fatal("last row could not be selected")
			}
			_, ok := hit(ids.thumb)
			if !ok {
				t.Fatal("missing scrollbar thumb")
			}
			thumb := list.geometry.VerticalThumb
			x, y := thumb.X+thumb.Width/2, thumb.Y+thumb.Height/2
			must(view.Press(x, y))
			must(view.Move(x, body.Y-30))
			must(view.Release(x, body.Y-30))
			if list.scroll != 0 || list.dragging {
				t.Fatal("dragging the scrollbar did not return to the top")
			}

			must(view.Keys("End"))
			if list.scroll != list.geometry.MaxOffsetY {
				t.Fatal("keyboard did not scroll to the bottom")
			}
			click(ids.search)
			must(view.Type(" LAB "))
			if len(list.rows) != total/2 || len(list.checked) != 0 || list.scroll != 0 {
				t.Fatalf("search left %d results, %d checked, offset %v", len(list.rows), len(list.checked), list.scroll)
			}
			click(ids.all)
			if len(list.checked) != total/2 {
				t.Fatalf("select all checked %d matching rows", len(list.checked))
			}
			click(ids.checks.At(1))
			if all, _ := hit(ids.all); !all.Mixed {
				t.Fatal("select all checkbox should indicate a partial selection")
			}
			click(ids.checks.At(1))
			click(remove)
			click(hitNetworkRemoveCancel)
			if ruleCount() != total || len(list.checked) != total/2 {
				t.Fatal("cancelling changed the records or selection")
			}
			click(remove)
			click(hitNetworkRemoveConfirm)
			if ruleCount() != total/2 || len(list.rows) != 0 || !view.HasText(state.messages.KnownHostsNoMatches) {
				t.Fatal("removal did not leave only the nonmatching records")
			}
			click(ids.search)
			must(view.Keys("Escape"))
			if len(list.rows) != total/2 || len(list.checked) != 0 {
				t.Fatal("clearing search did not show the remaining records")
			}
		})
	}
}
