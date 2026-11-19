package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/text"
)

// copyHostsToClipboard remembers the hosts Paste duplicates into another group:
// saved hosts, not a text rendering of them, so a copy carries everything a
// duplicate needs. A host deleted before the paste is skipped then.
func (state *State) copyHostsToClipboard(ids []uint32) int {
	state.hostClipboard = state.hostClipboard[:0]
	for _, id := range ids {
		if _, ok := state.hostByID(id); ok {
			state.hostClipboard = append(state.hostClipboard, id)
		}
	}
	if len(state.hostClipboard) == 0 {
		return 0
	}
	label := ""
	if host, ok := state.hostByID(state.hostClipboard[0]); ok {
		label = host.Label
	}
	if len(state.hostClipboard) > 1 {
		label = fmt.Sprintf("%s +%d", label, len(state.hostClipboard)-1)
	}
	state.status = state.messages.HostCopied + label
	return len(state.hostClipboard)
}

// pasteHostsInto duplicates the clipboard into group, as the Paste row of a
// group asks. It reports whether anything was made.
func (state *State) pasteHostsInto(group string) bool {
	count := state.duplicateHosts(state.hostClipboard, group)
	if count == 0 {
		return false
	}
	state.status = fmt.Sprintf(state.messages.HostPastedFmt, count)
	return true
}

// copyHostsToGroup duplicates the named hosts into group, as Copy to… asks.
func (state *State) copyHostsToGroup(ids []uint32, group string) bool {
	count := state.duplicateHosts(ids, group)
	if count == 0 {
		return false
	}
	label := group
	if label == "" {
		label = state.messages.HostGroupDefault
	}
	state.status = fmt.Sprintf(state.messages.HostCopiedToFmt, count, label)
	return true
}

// duplicateHosts copies each host that still exists into group, stopping at the
// first one the host dialog refuses: it stays open with its reason, as a copy
// the user asked for by hand does.
func (state *State) duplicateHosts(ids []uint32, group string) int {
	count := 0
	for _, id := range ids {
		if _, ok := state.hostByID(id); !ok {
			continue
		}
		if !state.duplicateHost(id, group) {
			break
		}
		count++
	}
	return count
}

// duplicateHost makes a saved copy of host in group through the same form,
// validation and secret planning the Copy Host dialog uses, without showing the
// dialog: the copy carries the address, options and saved secrets, and is
// renamed so it cannot collide with the host it came from.
func (state *State) duplicateHost(id uint32, group string) bool {
	if _, ok := state.hostByID(id); !ok {
		return false
	}
	state.openCopyHostID(id)
	label := state.freshCopyLabel()
	state.editor.group = text.Editor{}
	state.editor.group.Insert(group)
	state.editor.label = text.Editor{}
	state.editor.label.Insert(label)
	state.submitHostDialog()
	return state.modal != modalNewHost
}

// freshCopyLabel makes the form's suggested "<name> Copy" unique: a second
// paste of one host must not fail the same-host check that a repeated name
// with one endpoint would.
func (state *State) freshCopyLabel() string {
	base := strings.TrimSpace(state.editor.label.String())
	label := base
	for number := 2; state.hostLabelExists(label); number++ {
		label = fmt.Sprintf("%s %d", base, number)
	}
	return label
}

func (state *State) hostLabelExists(label string) bool {
	for _, host := range state.hosts {
		if strings.EqualFold(strings.TrimSpace(host.Label), label) {
			return true
		}
	}
	return false
}

// moveHostToGroup puts one host in group, as a drop onto a group row asks.
func (state *State) moveHostToGroup(hostID uint32, group string) bool {
	index := state.hostIndex(hostID)
	if index < 0 || strings.TrimSpace(state.hosts[index].Group) == group {
		return false
	}
	state.hosts[index].Group = group
	state.hostDataChanged()
	state.hostRevealID = hostID
	label := group
	if label == "" {
		label = state.messages.HostGroupDefault
	}
	state.status = fmt.Sprintf(state.messages.HostMovedFmt, 1, label)
	state.save()
	return true
}

// reorderHostBeside puts one host before or after another, as a drop on a host
// row asks. A target in another group also takes the host into that group, so a
// dragged host can be dropped where it belongs in one gesture.
func (state *State) reorderHostBeside(sourceID, targetID uint32, after bool) bool {
	source, target := state.hostIndex(sourceID), state.hostIndex(targetID)
	if source < 0 || target < 0 || source == target {
		return false
	}
	host := state.hosts[source]
	if host.Group != state.hosts[target].Group {
		host.Group = state.hosts[target].Group
	}
	// A display order other than Manual gives way to the order the drop writes.
	if state.hostOrder != hostOrderManual {
		state.hostOrder = hostOrderManual
	}
	remaining := append(state.hosts[:source], state.hosts[source+1:]...)
	// Removing the source shifts every target below it one place up.
	targetIndex := target
	if source < target {
		targetIndex--
	}
	insert := targetIndex
	if after {
		insert++
	}
	remaining = append(remaining, Host{})
	copy(remaining[insert+1:], remaining[insert:])
	remaining[insert] = host
	state.hosts = remaining
	state.hostDataChanged()
	state.hostRevealID = host.ID
	state.save()
	return true
}
