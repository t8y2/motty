package workbench

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"motty/internal/i18n"
	"motty/internal/sshconfig"
)

// The sync is made in two passes so that a failure leaves nothing half done. The
// first pass compares the two sides and makes every change to the config in
// memory, touching nothing of the hosts; the config is then saved. Only when it
// is saved does the second pass change the hosts, and then the links are kept.
// A config file that changed while the first pass ran is not written over: the
// sync starts again from the file as it now is.

type sshSyncOutcome struct {
	changedHere  int
	changedThere int
	addedHere    int
	addedThere   int
	unlinked     int
	warnings     []string
	// notes are what reading the file could not make of its lines, for a sync the
	// user asked for.
	notes []string
	err   error
	retry bool
}

type sshDirection uint8

const (
	sshSame     sshDirection = iota // both sides agree
	sshToHere                       // the file changed: take its value
	sshToThere                      // the host changed: write its value
	sshConflict                     // both changed: the file's value is kept
)

// decide says which way a field goes, from the value each side has now and the
// value both had at the last sync.
func decide(here, there, base string) sshDirection {
	switch {
	case here == there:
		return sshSame
	case here == base:
		return sshToHere
	case there == base:
		return sshToThere
	}
	return sshConflict
}

func port22(port uint16) uint16 {
	if port == 0 {
		return defaultPort
	}
	return port
}

func lower(text string) string { return strings.ToLower(text) }

// syncSSHConfig makes one sync. manual is the user asking for it, which shows
// its result even when nothing changed.
func (state *State) syncSSHConfig(manual bool) {
	view := &state.sshSync
	if view.running {
		return
	}
	view.running = true
	defer func() { view.running = false }()
	state.sshSyncLoad()
	var out sshSyncOutcome
	for attempt := 0; attempt < 2; attempt++ {
		out = state.reconcileSSHConfig()
		if !out.retry {
			break
		}
	}
	if out.retry {
		out.warnings = append(out.warnings, state.messages.SSHSync.WarnChangedMeanwhile)
	}
	// A sync that failed is tried again when the hosts or the file change, not at
	// every save in between.
	view.pending, view.print = false, state.sshSyncPrint()
	state.finishSSHSync(out, manual)
}

func (state *State) finishSSHSync(out sshSyncOutcome, manual bool) {
	m := state.messages.SSHSync
	view := &state.sshSync
	changes := out.changedHere + out.changedThere + out.addedHere + out.addedThere + out.unlinked
	var line string
	switch {
	case out.err != nil:
		line = fmt.Sprintf(m.FailedFmt, out.err)
	case changes > 0:
		line = fmt.Sprintf(m.SummaryFmt, out.changedHere, out.changedThere, out.addedHere, out.addedThere, out.unlinked)
	default:
		line = m.UpToDate
	}
	view.result = line
	if out.err != nil || changes > 0 || manual {
		state.status = line
	}
	if manual && out.err == nil && len(out.warnings)+len(out.notes) > 0 && (state.modal == modalNone || state.modal == modalHostImportResult) {
		state.hostImportResult.set(state.messages, line, append(out.warnings, out.notes...))
		state.hostImportResult.title = m.ResultTitle
		state.modal = modalHostImportResult
	}
}

type sshHostPull struct {
	id       string
	label    string
	address  *string
	user     *string
	port     *uint16
	conflict bool
}

type sshJumpPull struct {
	id   string
	hops []sshconfig.JumpHost
}

type sshReconciler struct {
	state *State
	m     i18n.SSHSyncMessages
	cfg   *sshconfig.Config
	out   sshSyncOutcome

	work    []*sshSyncLink
	byID    map[string]*sshSyncLink
	owners  map[string]*sshSyncLink // lower-cased alias of each live link
	ignored map[string]bool
	warned  map[string]bool
	noPush  bool

	// fileAliases are the names the file already holds, to find a free one.
	fileAliases map[string]bool

	pulls    []sshHostPull
	imports  []sshconfig.Host
	jumps    []sshJumpPull
	usedIDs  map[string]struct{}
	hostsNew bool
	keysNew  bool
	routeNew bool
}

func (state *State) reconcileSSHConfig() (out sshSyncOutcome) {
	view := &state.sshSync
	cfg, err := sshconfig.Load(state.sshConfigPath())
	if err != nil {
		view.stamps = statStamps([]sshFileStamp{{path: state.sshConfigPath()}})
		out.err = err
		return out
	}
	if len(view.file.Links) > 0 && !cfg.Exists() {
		// A file that is gone for a moment, as an editor saves it, must not be taken
		// for the user taking every host out of it.
		view.stamps = stampsOf(cfg)
		out.warnings = append(out.warnings, state.messages.SSHSync.WarnFileMissing)
		return out
	}
	r := &sshReconciler{
		state: state, m: state.messages.SSHSync, cfg: cfg,
		byID: map[string]*sshSyncLink{}, owners: map[string]*sshSyncLink{}, ignored: map[string]bool{}, warned: map[string]bool{},
		fileAliases: map[string]bool{},
	}
	r.plan()
	r.notes()
	if cfg.Changed() {
		if err := cfg.Save(); err != nil {
			view.stamps = stampsOf(cfg)
			r.out.err, r.out.retry = err, errors.Is(err, sshconfig.ErrChanged)
			if r.out.retry {
				r.out.err = nil
			}
			return r.out
		}
	}
	r.apply()
	if err := r.keep(); err != nil && r.out.err == nil {
		r.out.err = err
	}
	view.stamps = stampsOf(cfg)
	return r.out
}

func (r *sshReconciler) warn(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if !r.warned[message] {
		r.warned[message] = true
		r.out.warnings = append(r.out.warnings, message)
	}
}

func (r *sshReconciler) plan() {
	r.loadLinks()
	r.removeOrphans()
	r.linkHosts()
	r.mergeHosts()
	r.pickImports()
	r.mergeJumps()
	r.pruneIgnored()
}

func (r *sshReconciler) loadLinks() {
	file := &r.state.sshSync.file
	for _, link := range file.Links {
		link := link
		r.work = append(r.work, &link)
		r.byID[link.ID] = &link
		if link.live() {
			r.owners[lower(link.Alias)] = &link
		}
	}
	for _, alias := range file.Ignored {
		r.ignored[lower(alias)] = true
	}
}

func (r *sshReconciler) link(id string) *sshSyncLink {
	if link := r.byID[id]; link != nil {
		return link
	}
	link := &sshSyncLink{ID: id}
	r.work = append(r.work, link)
	r.byID[id] = link
	return link
}

func (r *sshReconciler) live(link *sshSyncLink) bool {
	return link != nil && link.live()
}

func fieldsOf(host Host) sshSyncFields {
	return sshSyncFields{Label: host.Label, Address: host.Address, User: host.User, Port: port22(host.Port)}
}

// removeOrphans takes out of the file the hosts that are gone from here: their
// block if Motty wrote it, and otherwise a note not to bring them back.
func (r *sshReconciler) removeOrphans() {
	present := map[string]bool{}
	for _, host := range r.state.hosts {
		if host.isSSH() && host.CatalogID != "" {
			present[host.CatalogID] = true
		}
	}
	var orphans []*sshSyncLink
	alive := 0
	for _, link := range r.work {
		if present[link.ID] {
			alive++
		} else {
			orphans = append(orphans, link)
		}
	}
	if len(orphans) == 0 || r.state.saveBlocked {
		return
	}
	if alive == 0 && len(orphans) >= 2 {
		// Everything went at once: more likely a catalog that failed to load than a
		// decision, and the file must not pay for it.
		r.warn(r.m.WarnMissingFmt, len(orphans))
		return
	}
	for _, link := range orphans {
		if r.live(link) {
			delete(r.owners, lower(link.Alias))
			switch r.cfg.Ownership(link.Alias) {
			case sshconfig.Managed:
				if removed, err := r.cfg.Remove(link.Alias); err != nil {
					r.warn(r.m.WarnWriteFmt, link.Alias, err)
				} else if removed {
					r.out.changedThere++
				}
			case sshconfig.Own, sshconfig.Shared:
				r.ignored[lower(link.Alias)] = true
			}
		}
		r.drop(link)
	}
}

func (r *sshReconciler) drop(link *sshSyncLink) {
	delete(r.byID, link.ID)
	r.work = slices.DeleteFunc(r.work, func(other *sshSyncLink) bool { return other == link })
}

// linkHosts gives every host that has no alias one: the alias of the file's
// that points to the same place, or a new one written to the file.
func (r *sshReconciler) linkHosts() {
	byEndpoint := map[hostConnection]string{}
	for _, fileHost := range r.cfg.Result().Hosts {
		r.fileAliases[lower(fileHost.Alias)] = true
		if !validImportedAddress(fileHost.HostName) {
			continue
		}
		key := connectionKey(fileHost.HostName, fileHost.User, fileHost.Port)
		if _, taken := byEndpoint[key]; !taken {
			byEndpoint[key] = fileHost.Alias
		}
	}
	for _, host := range r.state.hosts {
		if !host.isSSH() || host.CatalogID == "" {
			continue
		}
		link := r.byID[host.CatalogID]
		if r.live(link) {
			continue
		}
		if alias, ok := byEndpoint[connectionKey(host.Address, host.User, host.Port)]; ok && r.owners[lower(alias)] == nil {
			link = r.link(host.CatalogID)
			link.Alias, link.Detached = alias, false
			link.Base = fieldsOf(host)
			link.Base.Jump = r.mottyJumpKey(host)
			r.owners[lower(alias)] = link
			delete(r.ignored, lower(alias))
			continue
		}
		if link == nil {
			r.pushNew(host)
		}
	}
}

func (r *sshReconciler) freshAlias(host Host) string {
	base := sshconfig.AliasFor(host.Label)
	if base == "" {
		base = sshconfig.AliasFor(host.Address)
	}
	if base == "" {
		base = "host"
	}
	alias := base
	for number := 2; r.fileAliases[lower(alias)] || r.owners[lower(alias)] != nil || r.ignored[lower(alias)]; number++ {
		alias = base + "-" + strconv.Itoa(number)
	}
	r.fileAliases[lower(alias)] = true
	return alias
}

func (r *sshReconciler) pushNew(host Host) {
	if r.noPush {
		return
	}
	alias := r.freshAlias(host)
	address, user, port := host.Address, host.User, uint16(0)
	if value := port22(host.Port); value != defaultPort {
		port = value
	}
	err := r.cfg.Apply(sshconfig.Edit{Alias: alias, HostName: &address, User: &user, Port: &port})
	switch {
	case errors.Is(err, sshconfig.ErrMarkers):
		r.noPush = true
		r.warn("%s", r.m.WarnMarkers)
	case err != nil:
		r.warn(r.m.WarnWriteFmt, host.Label, err)
	default:
		link := r.link(host.CatalogID)
		link.Alias, link.Detached = alias, false
		link.Base = fieldsOf(host)
		r.owners[lower(alias)] = link
		r.out.addedThere++
	}
}

// mergeHosts compares each linked host with its alias, field by field.
func (r *sshReconciler) mergeHosts() {
	if r.noneLeft() {
		return
	}
	for _, host := range r.state.hosts {
		if !host.isSSH() || host.CatalogID == "" {
			continue
		}
		link := r.byID[host.CatalogID]
		if !r.live(link) {
			continue
		}
		fileHost, ok := r.cfg.Host(link.Alias)
		if !ok {
			link.Detached = true
			delete(r.owners, lower(link.Alias))
			r.out.unlinked++
			r.warn(r.m.WarnDetachedFmt, host.Label)
			continue
		}
		r.mergeHost(host, link, fileHost)
	}
}

// noneLeft is true when the file has no hosts at all while two or more are
// linked to it. That is a file that was replaced or emptied, more likely than a
// decision, and the hosts are left as they are.
func (r *sshReconciler) noneLeft() bool {
	if len(r.cfg.Result().Hosts) > 0 {
		return false
	}
	live, missing := 0, 0
	for _, host := range r.state.hosts {
		link := r.byID[host.CatalogID]
		if !host.isSSH() || !r.live(link) {
			continue
		}
		live++
		if _, ok := r.cfg.Host(link.Alias); !ok {
			missing++
		}
	}
	if live >= 2 && missing == live {
		r.warn(r.m.WarnNoneLeftFmt, live)
		return true
	}
	return false
}

func (r *sshReconciler) mergeHost(host Host, link *sshSyncLink, fileHost sshconfig.Host) {
	if !validImportedAddress(fileHost.HostName) {
		return
	}
	base := &link.Base
	pull := sshHostPull{id: host.CatalogID, label: host.Label}
	edit := sshconfig.Edit{Alias: link.Alias}
	hereAddress, hereUser, herePort := host.Address, host.User, port22(host.Port)
	thereAddress, thereUser, therePort := fileHost.HostName, fileHost.User, port22(fileHost.Port)

	switch decide(lower(hereAddress), lower(thereAddress), lower(base.Address)) {
	case sshSame:
		base.Address = hereAddress
	case sshToThere:
		edit.HostName = &hereAddress
	case sshConflict:
		pull.conflict = true
		fallthrough
	case sshToHere:
		pull.address = &thereAddress
	}
	switch decide(hereUser, thereUser, base.User) {
	case sshSame:
		base.User = hereUser
	case sshToThere:
		edit.User = &hereUser
	case sshConflict:
		pull.conflict = true
		fallthrough
	case sshToHere:
		pull.user = &thereUser
	}
	portText := func(port uint16) string { return strconv.Itoa(int(port)) }
	switch decide(portText(herePort), portText(therePort), portText(base.Port)) {
	case sshSame:
		base.Port = herePort
	case sshToThere:
		written := uint16(0)
		if herePort != defaultPort {
			written = herePort
		}
		edit.Port = &written
	case sshConflict:
		pull.conflict = true
		fallthrough
	case sshToHere:
		pull.port = &therePort
	}
	if pull.address != nil || pull.user != nil || pull.port != nil {
		r.pulls = append(r.pulls, pull)
	}
	if edit.HostName != nil || edit.User != nil || edit.Port != nil {
		r.push(host, link, edit, hereAddress, hereUser, herePort)
	}
	r.rename(host, link)
}

// push writes the host's changed fields into the alias's block and checks that
// the file then says what was written: an earlier general block can override it.
func (r *sshReconciler) push(host Host, link *sshSyncLink, edit sshconfig.Edit, address, user string, port uint16) {
	err := r.cfg.Apply(edit)
	if errors.Is(err, sshconfig.ErrShared) {
		r.warn(r.m.WarnSharedFmt, link.Alias)
		return
	}
	if err != nil {
		r.warn(r.m.WarnWriteFmt, link.Alias, err)
		return
	}
	r.out.changedThere++
	effective, _ := r.cfg.Host(link.Alias)
	if edit.Port != nil && port22(effective.Port) != port && port == defaultPort {
		written := uint16(defaultPort)
		if r.cfg.Apply(sshconfig.Edit{Alias: link.Alias, Port: &written}) == nil {
			effective, _ = r.cfg.Host(link.Alias)
		}
	}
	base := &link.Base
	if edit.HostName != nil {
		if strings.EqualFold(effective.HostName, address) {
			base.Address = address
		} else {
			base.Address = effective.HostName
			r.warn(r.m.WarnOverriddenFmt, link.Alias, "HostName")
		}
	}
	if edit.User != nil {
		if effective.User == user {
			base.User = user
		} else {
			base.User = effective.User
			r.warn(r.m.WarnOverriddenFmt, link.Alias, "User")
		}
	}
	if edit.Port != nil {
		if port22(effective.Port) == port {
			base.Port = port
		} else {
			base.Port = port22(effective.Port)
			r.warn(r.m.WarnOverriddenFmt, link.Alias, "Port")
		}
	}
}

// rename follows a host's new label with its alias, when the alias was made
// from the label, and with every ProxyJump in the file that named the alias.
func (r *sshReconciler) rename(host Host, link *sshSyncLink) {
	base := &link.Base
	if host.Label == base.Label {
		return
	}
	derived := base.Label == link.Alias || sshconfig.AliasFor(base.Label) == link.Alias
	wanted := sshconfig.AliasFor(host.Label)
	base.Label = host.Label
	if !derived || wanted == "" || strings.EqualFold(wanted, link.Alias) {
		return
	}
	old := link.Alias
	var referrers []sshconfig.Host
	for _, other := range r.cfg.Result().Hosts {
		for _, hop := range other.ProxyJump {
			if strings.EqualFold(hop.Host, old) {
				referrers = append(referrers, other)
				break
			}
		}
	}
	if err := r.cfg.Rename(old, wanted); err != nil {
		r.warn(r.m.WarnRenameFmt, old, err)
		return
	}
	delete(r.owners, lower(old))
	link.Alias = wanted
	r.owners[lower(wanted)] = link
	r.out.changedThere++
	for _, other := range referrers {
		hops := slices.Clone(other.ProxyJump)
		for index := range hops {
			if strings.EqualFold(hops[index].Host, old) {
				hops[index].Host = wanted
			}
		}
		text := sshconfig.FormatJumps(hops)
		if err := r.cfg.Apply(sshconfig.Edit{Alias: other.Alias, ProxyJump: &text}); err != nil {
			r.warn(r.m.WarnWriteFmt, other.Alias, err)
		}
	}
}

// pickImports lists the aliases of the file that no host stands for.
func (r *sshReconciler) pickImports() {
	for _, fileHost := range r.cfg.Result().Hosts {
		key := lower(fileHost.Alias)
		if r.owners[key] != nil || r.ignored[key] {
			continue
		}
		if !validImportedAddress(fileHost.HostName) {
			r.warn(r.state.messages.SSHConfigWarningHostFmt, fileHost.Alias)
			continue
		}
		candidate := Host{Label: fileHost.Alias, Address: fileHost.HostName, User: fileHost.User, Port: port22(fileHost.Port)}
		if r.state.hostIndexByEndpoint(candidate) >= 0 {
			r.warn(r.m.WarnEndpointFmt, fileHost.Alias)
			continue
		}
		r.imports = append(r.imports, fileHost)
	}
}

// notes turns what reading the file could not understand into lines for the
// result dialog, the way importing a config does.
func (r *sshReconciler) notes() {
	for _, warning := range r.cfg.Result().Warnings {
		if warning.Code == sshconfig.WarningUnsupportedDirective {
			continue // directives Motty has no use for stay in the file untouched
		}
		detail := warning.Detail
		if warning.Directive != "" {
			detail = warning.Directive + ": " + detail
		}
		r.out.notes = append(r.out.notes, fmt.Sprintf(r.state.messages.SSHConfigWarningSourceFmt, filepath.Base(warning.Path), warning.Line, detail))
	}
}

func (r *sshReconciler) pruneIgnored() {
	for alias := range r.ignored {
		if r.cfg.Ownership(alias) == sshconfig.Absent {
			delete(r.ignored, alias)
		}
	}
}

// mottyChain is the hosts a connection to host goes through, first hop first.
func (r *sshReconciler) mottyChain(host Host) []Host {
	var chain []Host
	current := host.ID
	for range 32 {
		jump, ok := r.state.routeJump(current)
		if !ok {
			break
		}
		next, ok := r.state.hostByID(jump)
		if !ok {
			break
		}
		chain = append(chain, next)
		current = jump
	}
	slices.Reverse(chain)
	return chain
}

func (state *State) routeJump(target uint32) (uint32, bool) {
	for _, route := range state.proxies {
		if route.TargetID == target {
			return route.JumpID, true
		}
	}
	return 0, false
}

func (r *sshReconciler) mottyJumpKey(host Host) string {
	chain := r.mottyChain(host)
	keys := make([]string, len(chain))
	for index, hop := range chain {
		keys[index] = hop.CatalogID
	}
	return strings.Join(keys, ",")
}

// hopEndpoint is where a ProxyJump hop of the file goes: the hop's alias is
// looked up in the file the way ssh does it, and what the hop names itself wins.
func (r *sshReconciler) hopEndpoint(hop sshconfig.JumpHost) (address, user string, port uint16) {
	address, user, port = hop.Host, hop.User, hop.Port
	if named, ok := r.cfg.Host(hop.Host); ok && validImportedAddress(named.HostName) {
		address = named.HostName
		if user == "" {
			user = named.User
		}
		if port == 0 {
			port = named.Port
		}
	}
	return address, user, port22(port)
}

func (r *sshReconciler) hostAt(address, user string, port uint16) (Host, bool) {
	return r.state.hostByConnection(address, user, port)
}

func (r *sshReconciler) fileJumpKey(fileHost sshconfig.Host) string {
	keys := make([]string, len(fileHost.ProxyJump))
	for index, hop := range fileHost.ProxyJump {
		address, user, port := r.hopEndpoint(hop)
		if known, ok := r.hostAt(address, user, port); ok && known.CatalogID != "" {
			keys[index] = known.CatalogID
		} else {
			keys[index] = "?" + sshconfig.FormatJump(sshconfig.JumpHost{Host: address, User: user, Port: port})
		}
	}
	return strings.Join(keys, ",")
}

// mottyJumpText writes the host's chain as a ProxyJump value: a hop with an
// alias in the file by that alias, any other as the place it is.
func (r *sshReconciler) mottyJumpText(host Host) string {
	chain := r.mottyChain(host)
	hops := make([]sshconfig.JumpHost, len(chain))
	for index, hop := range chain {
		if link := r.byID[hop.CatalogID]; r.live(link) {
			hops[index] = sshconfig.JumpHost{Host: link.Alias}
			continue
		}
		hops[index] = sshconfig.JumpHost{Host: hop.Address, User: hop.User}
		if hop.Port != defaultPort {
			hops[index].Port = hop.Port
		}
	}
	return sshconfig.FormatJumps(hops)
}

func (r *sshReconciler) mergeJumps() {
	for _, host := range r.state.hosts {
		link := r.byID[host.CatalogID]
		if !host.isSSH() || !r.live(link) {
			continue
		}
		fileHost, ok := r.cfg.Host(link.Alias)
		if !ok {
			continue
		}
		here, there := r.mottyJumpKey(host), r.fileJumpKey(fileHost)
		switch decide(here, there, link.Base.Jump) {
		case sshSame:
			link.Base.Jump = here
		case sshToThere:
			text := r.mottyJumpText(host)
			if err := r.cfg.Apply(sshconfig.Edit{Alias: link.Alias, ProxyJump: &text}); err != nil {
				if errors.Is(err, sshconfig.ErrShared) {
					r.warn(r.m.WarnSharedFmt, link.Alias)
				} else {
					r.warn(r.m.WarnWriteFmt, link.Alias, err)
				}
				continue
			}
			r.out.changedThere++
			written, _ := r.cfg.Host(link.Alias)
			if now := r.fileJumpKey(written); now == here {
				link.Base.Jump = here
			} else {
				link.Base.Jump = now
				r.warn(r.m.WarnOverriddenFmt, link.Alias, "ProxyJump")
			}
		case sshConflict, sshToHere:
			r.jumps = append(r.jumps, sshJumpPull{id: host.CatalogID, hops: fileHost.ProxyJump})
		}
	}
}

// apply is the second pass: the config is saved, so the hosts follow it.
func (r *sshReconciler) apply() {
	for _, pull := range r.pulls {
		r.applyPull(pull)
	}
	for _, fileHost := range r.imports {
		r.importHost(fileHost)
	}
	for index := 0; index < len(r.jumps); index++ {
		r.applyJump(r.jumps[index])
	}
	r.offerIdentities()
	if r.hostsNew || r.keysNew || r.routeNew {
		state := r.state
		state.hostDataChanged()
		state.refreshIdentityCount()
		state.refreshProxies()
		state.save()
	}
}

func (r *sshReconciler) applyPull(pull sshHostPull) {
	state := r.state
	index := slices.IndexFunc(state.hosts, func(host Host) bool { return host.CatalogID == pull.id })
	link := r.byID[pull.id]
	if index < 0 || link == nil {
		return
	}
	previous := state.hosts[index]
	updated := previous
	if pull.address != nil {
		updated.Address = *pull.address
	}
	if pull.user != nil {
		updated.User = *pull.user
	}
	if pull.port != nil {
		updated.Port = *pull.port
	}
	updated.normalizeEndpoint()
	updated.finish()
	if other := state.hostIndexByEndpoint(updated); other >= 0 && other != index {
		r.warn(r.m.WarnEndpointFmt, link.Alias)
		return
	}
	state.replaceHost(index, previous, updated)
	link.Base.Address, link.Base.User, link.Base.Port = updated.Address, updated.User, port22(updated.Port)
	r.out.changedHere++
	r.hostsNew = true
	if pull.conflict {
		r.warn(r.m.WarnConflictFmt, pull.label)
	}
}

func (r *sshReconciler) newCatalogID(host Host) string {
	if r.usedIDs == nil {
		r.usedIDs = make(map[string]struct{}, len(r.state.hosts))
		for _, existing := range r.state.hosts {
			if existing.CatalogID != "" {
				r.usedIDs[existing.CatalogID] = struct{}{}
			}
		}
	}
	return uniqueCatalogID("", host, r.usedIDs)
}

func (r *sshReconciler) addHost(host Host) Host {
	state := r.state
	host.ID = state.nextHostID
	state.nextHostID++
	host.CatalogID = r.newCatalogID(host)
	host.finish()
	state.hosts = append(state.hosts, host)
	r.hostsNew = true
	return host
}

func (r *sshReconciler) importHost(fileHost sshconfig.Host) {
	host := Host{Label: fileHost.Alias, Address: fileHost.HostName, User: fileHost.User, Port: port22(fileHost.Port)}
	if r.state.hostIndexByEndpoint(host) >= 0 {
		r.warn(r.m.WarnEndpointFmt, fileHost.Alias)
		return
	}
	host = r.addHost(host)
	link := r.link(host.CatalogID)
	link.Alias, link.Detached = fileHost.Alias, false
	link.Base = fieldsOf(host)
	r.owners[lower(fileHost.Alias)] = link
	r.out.addedHere++
	if len(fileHost.ProxyJump) > 0 {
		r.jumps = append(r.jumps, sshJumpPull{id: host.CatalogID, hops: fileHost.ProxyJump})
	}
}

// ensureHop is the host a hop of the file goes to, made when there is none: a
// host that exists only to be a jump is not written to the file.
func (r *sshReconciler) ensureHop(hop sshconfig.JumpHost) (Host, bool) {
	address, user, port := r.hopEndpoint(hop)
	if known, ok := r.hostAt(address, user, port); ok {
		return known, true
	}
	if !validImportedAddress(address) {
		return Host{}, false
	}
	host := r.addHost(Host{Label: hop.Host, Address: address, User: user, Port: port})
	link := r.link(host.CatalogID)
	link.Detached = true
	link.Base = fieldsOf(host)
	r.out.addedHere++
	return host, true
}

func (r *sshReconciler) applyJump(pull sshJumpPull) {
	state := r.state
	target, ok := state.hostByCatalogID(pull.id)
	link := r.byID[pull.id]
	if !ok || link == nil {
		return
	}
	chain := make([]uint32, len(pull.hops))
	for index, hop := range pull.hops {
		jump, ok := r.ensureHop(hop)
		if !ok {
			r.warn(r.m.WarnJumpFmt, link.Alias)
			return
		}
		chain[index] = jump.ID
	}
	if !state.setProxyChain(target.ID, chain) {
		r.warn(r.m.WarnJumpFmt, link.Alias)
		return
	}
	link.Base.Jump = r.mottyJumpKey(target)
	r.out.changedHere++
	r.routeNew = true
}

// offerIdentities adds to the Keys list the IdentityFile a block names, once.
func (r *sshReconciler) offerIdentities() {
	state := r.state
	for _, link := range r.work {
		if !r.live(link) {
			continue
		}
		fileHost, ok := r.cfg.Host(link.Alias)
		if !ok || fileHost.IdentityFile == "" || fileHost.IdentityFile == link.Base.Identity {
			continue
		}
		link.Base.Identity = fileHost.IdentityFile
		path, err := filepath.Abs(filepath.Clean(fileHost.IdentityFile))
		if err != nil {
			path = filepath.Clean(fileHost.IdentityFile)
		}
		if slices.ContainsFunc(state.identities, func(known identityRecord) bool { return known.Path == path }) {
			continue
		}
		record := inspectIdentity(state.nextIdentityID, path, state.keyMessages())
		if record.Error != "" {
			r.warn(state.messages.SSHConfigWarningIdentityFmt, filepath.Base(path), record.Error)
			continue
		}
		state.nextIdentityID++
		state.identities = append(state.identities, record)
		r.keysNew = true
	}
}

// keep stores the links, now that both sides agree on them.
func (r *sshReconciler) keep() error {
	view := &r.state.sshSync
	view.file.Links = view.file.Links[:0]
	for _, link := range r.work {
		view.file.Links = append(view.file.Links, *link)
	}
	view.file.Ignored = view.file.Ignored[:0]
	for alias := range r.ignored {
		view.file.Ignored = append(view.file.Ignored, alias)
	}
	slices.Sort(view.file.Ignored)
	return r.state.sshSyncSave()
}

// setProxyChain makes the connection to target go through chain, first hop
// first, and reports whether the routes could be made so. Each hop reaches the
// next by the route it already has, so a chain that disagrees with a route
// already there, or would run in a circle, is refused and nothing changes.
func (state *State) setProxyChain(target uint32, chain []uint32) bool {
	candidate := make(map[uint32]uint32, len(state.proxies)+len(chain))
	for _, route := range state.proxies {
		if route.TargetID != target {
			candidate[route.TargetID] = route.JumpID
		}
	}
	type edge struct{ target, jump uint32 }
	var edges []edge
	current := target
	for index := len(chain) - 1; index >= 0; index-- {
		jump := chain[index]
		if current == jump {
			return false
		}
		if configured, exists := candidate[current]; exists {
			if configured != jump {
				return false
			}
			current = jump
			continue
		}
		if proxyRouteWouldCycle(candidate, current, jump) {
			return false
		}
		candidate[current] = jump
		edges = append(edges, edge{target: current, jump: jump})
		current = jump
	}
	state.proxies = slices.DeleteFunc(state.proxies, func(route proxyRecord) bool { return route.TargetID == target })
	for _, added := range edges {
		state.proxies = append(state.proxies, proxyRecord{ID: state.nextProxyID, TargetID: added.target, JumpID: added.jump})
		state.nextProxyID++
	}
	if _, ok := state.proxyByID(state.selectedProxyID); !ok {
		state.selectedProxyID = 0
		if len(state.proxies) > 0 {
			state.selectedProxyID = state.proxies[0].ID
		}
	}
	state.refreshProxies()
	return true
}
