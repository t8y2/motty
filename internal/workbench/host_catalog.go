package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"motty/internal/sshconn"
)

const (
	hostCatalogSchemaVersion = 4
	maxHostTags              = 12
	maxHostTagRunes          = 32
)

const hostCatalogFormat = "motty-host-catalog"

type hostIcon uint8

const (
	hostIconAuto hostIcon = iota
	hostIconServer
	hostIconTerminal
	hostIconGlobe
	hostIconKey
	// hostIconDesktop is the generic remote-desktop glyph, for VNC/RDP hosts
	// whose system is not known.
	hostIconDesktop
	// hostIconSystem and the values above it are a system's badge: the value is
	// hostIconSystem plus the sshconn.RemoteSystem.
	hostIconSystem
)

// hostIconOf is the icon that shows a system's badge.
func hostIconOf(system sshconn.RemoteSystem) hostIcon { return hostIconSystem + hostIcon(system) }

// system is the system whose badge the icon shows, if it shows one.
func (icon hostIcon) system() (sshconn.RemoteSystem, bool) {
	if icon < hostIconSystem {
		return sshconn.SystemUnknown, false
	}
	system := sshconn.RemoteSystem(icon - hostIconSystem)
	return system, system != sshconn.SystemUnknown && system.String() != ""
}

// valid says the icon is one the dialog offers.
func (icon hostIcon) valid() bool {
	if icon < hostIconSystem {
		return true
	}
	_, ok := icon.system()
	return ok
}

func (icon hostIcon) String() string {
	if system, ok := icon.system(); ok {
		return system.String()
	}
	switch icon {
	case hostIconServer:
		return "server"
	case hostIconTerminal:
		return "terminal"
	case hostIconGlobe:
		return "globe"
	case hostIconKey:
		return "key"
	case hostIconDesktop:
		return "desktop"
	default:
		return "auto"
	}
}

func parseHostIcon(value string) (hostIcon, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return hostIconAuto, true
	case "server":
		return hostIconServer, true
	case "terminal":
		return hostIconTerminal, true
	case "globe":
		return hostIconGlobe, true
	case "key":
		return hostIconKey, true
	case "desktop":
		return hostIconDesktop, true
	default:
		if system := sshconn.ParseRemoteSystem(value); system != sshconn.SystemUnknown {
			return hostIconOf(system), true
		}
		return hostIconAuto, false
	}
}

func parseHostTags(value string) ([]string, bool) {
	if strings.TrimSpace(value) == "" {
		return nil, true
	}
	parts := strings.Split(value, ",")
	tags := make([]string, 0, min(len(parts), maxHostTags))
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > maxHostTagRunes {
			return nil, false
		}
		duplicate := false
		for _, existing := range tags {
			if strings.EqualFold(existing, tag) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			tags = append(tags, tag)
			if len(tags) > maxHostTags {
				return nil, false
			}
		}
	}
	return tags, true
}

func normalizeHostTags(tags []string) ([]string, bool) {
	return parseHostTags(strings.Join(tags, ","))
}

func setHostTags(host *Host, tags []string) {
	host.Tags = strings.Join(tags, ", ")
}

func splitHostTags(host Host) []string {
	if host.Tags == "" {
		return nil
	}
	return strings.Split(host.Tags, ", ")
}

func formatHostTags(host Host) string {
	return host.Tags
}

func catalogIDFor(host Host) string {
	source := strings.ToLower(strings.TrimSpace(host.Address)) + "\x00" + strings.TrimSpace(host.User) + "\x00" + fmt.Sprint(host.Port)
	if !host.isSSH() {
		// SSH hosts keep the ids they always had; another protocol to the same
		// address is another host.
		source += "\x00" + host.transport().Protocol
	}
	digest := sha256.Sum256([]byte(source))
	return "host_" + hex.EncodeToString(digest[:12])
}

// uniqueCatalogID chooses a catalog ID no host in used holds, and records it
// there. used is the set of IDs already taken; a caller adding a batch of hosts
// builds it once instead of scanning a growing slice for every host.
func uniqueCatalogID(preferred string, host Host, used map[string]struct{}) string {
	base := strings.TrimSpace(preferred)
	if base == "" {
		base = catalogIDFor(host)
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, taken := used[candidate]; !taken {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", base, suffix)
	}
}

type hostGroup struct {
	Name     string
	Label    string
	Depth    uint8
	Expanded bool
	Explicit bool
	Pinned   bool
	Order    int
	Count    string // "(3)": how many hosts, after the name
	Digits   string // how many hosts, alone, for the filter chip
}

type hostTreeRowKind uint8

const (
	hostTreeGroup hostTreeRowKind = iota
	hostTreeHost
)

type hostTreeRow struct {
	Kind       hostTreeRowKind
	GroupIndex int
	HostIndex  int
	Depth      uint8
}

func (state *State) refreshHostGroups() {
	previous := append([]hostGroup(nil), state.hostGroups...)
	clear(state.hostGroups)
	state.hostGroups = state.hostGroups[:0]
	add := func(name string) {
		if state.hostGroupIndex(name) >= 0 {
			return
		}
		group := hostGroup{Name: name, Label: hostGroupLeaf(name), Depth: uint8(min(strings.Count(name, "/"), 12)), Expanded: true}
		for _, group := range previous {
			if group.Name == name {
				state.hostGroups = append(state.hostGroups, group)
				return
			}
		}
		state.hostGroups = append(state.hostGroups, group)
	}
	for _, group := range previous {
		if group.Explicit {
			add(group.Name)
		}
	}
	for _, host := range state.hosts {
		add(strings.TrimSpace(host.Group))
	}
	for index := 0; index < len(state.hostGroups); index++ {
		path := state.hostGroups[index].Name
		for {
			at := strings.LastIndex(path, "/")
			if at < 0 {
				break
			}
			path = path[:at]
			add(path)
		}
	}
	byName := make(map[string]hostGroup, len(state.hostGroups))
	for _, group := range state.hostGroups {
		byName[group.Name] = group
	}
	sort.Slice(state.hostGroups, func(left, right int) bool {
		return orderedHostGroupLess(state.hostGroups[left].Name, state.hostGroups[right].Name, byName)
	})
	for index := range state.hostGroups {
		group := &state.hostGroups[index]
		group.Label = hostGroupLeaf(group.Name)
		group.Depth = uint8(min(strings.Count(group.Name, "/"), 12))
		count := 0
		for _, host := range state.hosts {
			if hostGroupWithin(strings.TrimSpace(host.Group), group.Name) {
				count++
			}
		}
		state.hostGroups[index].Digits = strconv.Itoa(count)
		state.hostGroups[index].Count = "(" + state.hostGroups[index].Digits + ")"
	}
	state.hostTotalDigits = strconv.Itoa(len(state.hosts))
	if state.hostFilterOn && state.hostGroupIndex(state.hostFilter) < 0 {
		state.hostFilterOn = false
	}
	state.rebuildHostTreeRows()
}

func hostGroupLeaf(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

func hostGroupWithin(path, parent string) bool {
	return path == parent || parent != "" && strings.HasPrefix(path, parent+"/")
}

// Compare one segment at a time so punctuation in a sibling cannot split a subtree.
func hostGroupLess(left, right string) bool {
	for {
		l, lrest, lmore := strings.Cut(left, "/")
		r, rrest, rmore := strings.Cut(right, "/")
		if order := strings.Compare(strings.ToLower(l), strings.ToLower(r)); order != 0 {
			return order < 0
		}
		if l != r {
			return l < r
		}
		if !lmore || !rmore {
			return !lmore && rmore
		}
		left, right = lrest, rrest
	}
}

func (state *State) hostGroupIndex(name string) int {
	for index, group := range state.hostGroups {
		if group.Name == name {
			return index
		}
	}
	return -1
}

// namedHostGroups is how many groups the user has made; the unnamed bucket is
// not one of them.
func (state *State) namedHostGroups() int {
	named := 0
	for _, group := range state.hostGroups {
		if group.Name != "" {
			named++
		}
	}
	return named
}

// hasHostFilter is whether the row of group chips is offered: only once there
// is a group to choose.
func (state *State) hasHostFilter() bool { return state.namedHostGroups() > 0 }

// hostsFlat is whether the list has no group headers: with no named group there
// is nothing to head, and with one group chosen its name is on the chip.
func (state *State) hostsFlat() bool { return state.hostFilterOn || state.namedHostGroups() == 0 }

// setHostFilter shows every group (on false) or only the named one. What was
// checked is let go, so no action reaches a host that is out of sight.
func (state *State) setHostFilter(on bool, group string) {
	state.hostFilterOn, state.hostFilter = on, group
	state.hostFilterMenu = false
	state.hostSelections = state.hostSelections[:0]
	state.refreshHostSelection()
	state.hostsScroll = 0
	state.rebuildHostTreeRows()
}

// inHostFilter is whether a host belongs to what the chips currently show.
func (state *State) inHostFilter(host Host) bool {
	return !state.hostFilterOn || hostGroupWithin(strings.TrimSpace(host.Group), state.hostFilter)
}

func (state *State) rebuildHostTreeRows() {
	state.hostTreeRows = state.hostTreeRows[:0]
	words := strings.Fields(strings.ToLower(state.visibleQuery))
	filtering := len(words) != 0
	flat := state.hostsFlat()
	hidden := ""
	for groupIndex, group := range state.hostGroups {
		if state.hostFilterOn && !hostGroupWithin(group.Name, state.hostFilter) {
			continue
		}
		if hidden != "" && hostGroupWithin(group.Name, hidden) {
			continue
		}
		hidden = ""
		matched := 0
		for _, hostIndex := range state.visible {
			if hostGroupWithin(strings.TrimSpace(state.hosts[hostIndex].Group), group.Name) {
				matched++
			}
		}
		explicit := group.Explicit && (!filtering || matchesSearchWords(strings.ToLower(group.Name), words))
		if !explicit {
			for _, child := range state.hostGroups {
				if child.Explicit && hostGroupWithin(child.Name, group.Name) && (!filtering || matchesSearchWords(strings.ToLower(child.Name), words)) {
					explicit = true
					break
				}
			}
		}
		if matched == 0 && !explicit {
			continue
		}
		if !flat {
			state.hostTreeRows = append(state.hostTreeRows, hostTreeRow{Kind: hostTreeGroup, GroupIndex: groupIndex, HostIndex: -1, Depth: group.Depth})
		}
		if !flat && !group.Expanded && !filtering {
			hidden = group.Name
			continue
		}
		for _, hostIndex := range state.visible {
			if strings.TrimSpace(state.hosts[hostIndex].Group) != group.Name {
				continue
			}
			depth := group.Depth + 1
			if flat {
				depth = 0
			}
			state.hostTreeRows = append(state.hostTreeRows, hostTreeRow{Kind: hostTreeHost, GroupIndex: groupIndex, HostIndex: hostIndex, Depth: depth})
		}
	}
}

func (state *State) toggleHostGroup(index int) {
	if index < 0 || index >= len(state.hostGroups) {
		return
	}
	state.hostGroups[index].Expanded = !state.hostGroups[index].Expanded
	state.rebuildHostTreeRows()
	state.save()
}

func (state *State) hostGroupLabel(group hostGroup) string {
	if group.Name == "" {
		return state.messages.HostGroupDefault
	}
	return group.Name
}
