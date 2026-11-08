package workbench

import (
	"crypto/rand"
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
	// hostIconSerial is the serial-port glyph, for a host opened over a serial
	// line rather than a network.
	hostIconSerial
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
	case hostIconSerial:
		return "serial"
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
	case "serial":
		return hostIconSerial, true
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
// there. It is for a host that has none yet, as when a file written before IDs
// existed is read: the ID is then made from where the host connects, so the same
// file gives the same ID every time. A host created now takes freshCatalogID.
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

// freshCatalogID is the ID of a host that is being created: random, and recorded
// in used. It says nothing about the host, so it cannot collide with the ID that
// another device gives a different host with the same address, and it stays the
// host's own however the host is edited. Two devices that each add the same host
// end up with two IDs; mergeSameHosts brings them back to one.
func freshCatalogID(used map[string]struct{}) string {
	for {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			panic("workbench: no randomness for a catalog id: " + err.Error())
		}
		id := "host_" + hex.EncodeToString(random[:])
		if _, taken := used[id]; !taken {
			used[id] = struct{}{}
			return id
		}
	}
}

// hostMerge says that the host dropped was the same host as kept.
type hostMerge struct {
	dropped, kept Host
}

// mergeKey is what makes two hosts one host for mergeSameHosts: the destination
// and the name, as in sameHost.
type mergeKey struct {
	protocol uint8
	address  string
	user     string
	port     uint16
	viaSSH   bool
	label    string
}

func mergeKeyOf(host Host) mergeKey {
	key := mergeKey{protocol: uint8(host.Protocol), address: host.Address, label: strings.ToLower(strings.TrimSpace(host.Label))}
	if host.Protocol != protocolSerial {
		key.user, key.port, key.viaSSH = host.User, host.Port, host.ViaSSH
	}
	return key
}

// mergeSameHosts folds hosts that are the same host (see sameHost) but have
// different IDs into one, and returns what it did. A host has no ID that two
// devices agree on before they have synced, so a host added on both appears
// twice when their records meet. The host with the smallest ID stays, which every
// device chooses alike, so each ends with the same one without talking to the
// other. The caller carries secrets and references to the host that stays.
func mergeSameHosts(hosts []Host) ([]Host, []hostMerge) {
	first := make(map[mergeKey]int, len(hosts))
	var merges []hostMerge
	drop := make([]bool, len(hosts))
	for index, host := range hosts {
		key := mergeKeyOf(host)
		other, seen := first[key]
		if !seen {
			first[key] = index
			continue
		}
		if hosts[other].CatalogID == host.CatalogID {
			continue // one record twice is not two hosts to fold
		}
		if host.CatalogID < hosts[other].CatalogID {
			merges = append(merges, hostMerge{dropped: hosts[other], kept: host})
			drop[other], first[key] = true, index
		} else {
			merges = append(merges, hostMerge{dropped: host, kept: hosts[other]})
			drop[index] = true
		}
	}
	if len(merges) == 0 {
		return hosts, nil
	}
	kept := hosts[:0:0]
	for index, host := range hosts {
		if !drop[index] {
			kept = append(kept, host)
		}
	}
	return kept, merges
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
		if state.hostOrder != hostOrderManual {
			return hostGroupLess(state.hostGroups[left].Name, state.hostGroups[right].Name)
		}
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
	state.refreshHostSelection()
}

func hostGroupLeaf(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

// normalizeHostGroupPath is the one form a group path is kept in: its folder
// names, in order, joined with "/". A path written with spaces around the
// separators names the same folders, so it reads as one group rather than as a
// second group beside it — which is what an import that spelled the separator
// " / " left behind.
func normalizeHostGroupPath(path string) string {
	if !strings.Contains(path, "/") {
		return strings.TrimSpace(path)
	}
	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(path, "/") {
		if segment = strings.TrimSpace(segment); segment != "" {
			segments = append(segments, segment)
		}
	}
	return strings.Join(segments, "/")
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

// hostsFlat is whether the list has no group headers: a selected leaf group's
// name is already on its chip, but its child groups still need their headers.
func (state *State) hostsFlat() bool {
	if !state.hostFilterOn {
		return state.namedHostGroups() == 0
	}
	for _, group := range state.hostGroups {
		if group.Name != state.hostFilter && hostGroupWithin(group.Name, state.hostFilter) {
			return false
		}
	}
	return true
}

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
	filterDepth := 0
	if state.hostFilterOn {
		if index := state.hostGroupIndex(state.hostFilter); index >= 0 {
			filterDepth = int(state.hostGroups[index].Depth) + 1
		}
	}
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
		selected := state.hostFilterOn && group.Name == state.hostFilter
		if !flat && !selected {
			state.hostTreeRows = append(state.hostTreeRows, hostTreeRow{Kind: hostTreeGroup, GroupIndex: groupIndex, HostIndex: -1, Depth: uint8(max(0, int(group.Depth)-filterDepth))})
		}
		if !flat && !selected && !group.Expanded && !filtering {
			hidden = group.Name
			continue
		}
		for _, hostIndex := range state.visible {
			if strings.TrimSpace(state.hosts[hostIndex].Group) != group.Name {
				continue
			}
			depth := uint8(max(0, int(group.Depth)+1-filterDepth))
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
