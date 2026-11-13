package workbench

import (
	"errors"
	"fmt"
	"github.com/vivaui/viva/gfx"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"motty/internal/i18n"
	"motty/internal/keyidentity"
	"motty/internal/sshconfig"
	"motty/internal/sshconn"
)

const maxVisibleHostImportWarnings = 7

type hostImportResult struct {
	// title replaces the dialog's own when set: the sync with the SSH config
	// reports in the same dialog.
	title         string
	summary       string
	warningsTitle string
	warnings      [maxVisibleHostImportWarnings]string
	visible       int
	more          string
}

type hostImportPlan struct {
	hosts          []Host
	identities     []identityRecord
	proxies        []proxyRecord
	warnings       []string
	duplicateHosts int
	nextHostID     uint32
	nextIdentityID uint32
	nextProxyID    uint32
}

type hostConnection struct {
	address string
	user    string
	port    uint16
}

type hostImportBuilder struct {
	state       *State
	plan        hostImportPlan
	sources     map[string]sshconfig.Host
	aliasIDs    map[string]uint32
	connections map[hostConnection]Host
	usedIDs     map[string]struct{}
	identitySet map[string]struct{}
	routes      map[uint32]uint32
}

func (state *State) importSSHConfig() {
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: state.messages.SSHConfigPickerTitle})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		state.status = state.messages.SSHConfigImportCanceled
		return
	}
	if err != nil {
		state.showActionFailure(state.messages.SSHConfigPickerTitle, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	parsed, err := sshconfig.ParseFile(paths[0])
	if err != nil {
		state.showFileReadFailure(state.messages.SSHConfigPickerTitle, err, state.messages.ActionFeedback.ImportSSHConfigFailed)
		return
	}
	plan := newHostImportBuilder(state, parsed).build(parsed.Hosts)
	state.applyHostImport(plan)
}

func newHostImportBuilder(state *State, parsed sshconfig.Result) *hostImportBuilder {
	builder := &hostImportBuilder{
		state:       state,
		sources:     make(map[string]sshconfig.Host, len(parsed.Hosts)),
		aliasIDs:    make(map[string]uint32, len(parsed.Hosts)),
		connections: make(map[hostConnection]Host, len(state.hosts)+len(parsed.Hosts)),
		usedIDs:     make(map[string]struct{}, len(state.hosts)+len(parsed.Hosts)),
		identitySet: make(map[string]struct{}, len(state.identities)+len(parsed.Hosts)),
		routes:      make(map[uint32]uint32, len(state.proxies)+len(parsed.Hosts)),
	}
	builder.plan.nextHostID = state.nextHostID
	builder.plan.nextIdentityID = state.nextIdentityID
	builder.plan.nextProxyID = state.nextProxyID
	for _, host := range state.hosts {
		builder.usedIDs[host.CatalogID] = struct{}{}
		if host.isSSH() {
			builder.connections[connectionKey(host.Address, host.User, host.Port)] = host
		}
	}
	for _, identity := range state.identities {
		builder.identitySet[identity.Path] = struct{}{}
	}
	for _, route := range state.proxies {
		builder.routes[route.TargetID] = route.JumpID
	}
	for _, warning := range parsed.Warnings {
		detail := warning.Detail
		if warning.Directive != "" {
			detail = warning.Directive + ": " + detail
		}
		builder.warn(fmt.Sprintf(state.messages.SSHConfigWarningSourceFmt, filepath.Base(warning.Path), warning.Line, detail))
	}
	return builder
}

func (builder *hostImportBuilder) build(sources []sshconfig.Host) hostImportPlan {
	for _, source := range sources {
		builder.sources[strings.ToLower(source.Alias)] = source
	}
	for _, source := range sources {
		builder.addSourceHost(source)
	}
	for _, source := range sources {
		if source.Options != nil {
			for _, path := range source.Options.IdentityFiles {
				if path != "none" {
					builder.addIdentity(path)
				}
			}
		} else {
			builder.addIdentity(source.IdentityFile)
		}
	}
	for _, source := range sources {
		if len(source.ProxyJump) != 0 {
			builder.addProxyChain(source)
		}
	}
	return builder.plan
}

func (builder *hostImportBuilder) addSourceHost(source sshconfig.Host) {
	if !validImportedAddress(source.HostName) {
		builder.warn(fmt.Sprintf(builder.state.messages.SSHConfigWarningHostFmt, source.Alias))
		return
	}
	port := source.Port
	if port == 0 {
		port = defaultPort
	}
	host, added := builder.ensureHost(source.Alias, source.HostName, source.User, port)
	builder.aliasIDs[strings.ToLower(source.Alias)] = host.ID
	if !added {
		builder.plan.duplicateHosts++
		return
	}
	builder.plan.hosts[len(builder.plan.hosts)-1].SSHOptions = importedSSHOptions(source.Options)
	if source.ProxyCommand != "" {
		builder.addProxyCommand(source)
	}
}

func importedSSHOptions(source *sshconfig.ConnectionOptions) *sshconfig.ConnectionOptions {
	options := source.Clone()
	if options != nil {
		for index, path := range options.IdentityFiles {
			if path != "none" {
				if absolute, err := filepath.Abs(path); err == nil {
					options.IdentityFiles[index] = absolute
				}
			}
		}
	}
	return options
}

// addProxyCommand gives the host just added the source's ProxyCommand as its
// own proxy, and says so.
func (builder *hostImportBuilder) addProxyCommand(source sshconfig.Host) {
	proxy, note := importedProxyCommand(&builder.state.messages, source)
	builder.plan.hosts[len(builder.plan.hosts)-1].Proxy = proxy
	builder.warn(note)
}

// importedProxyCommand is the proxy a host read from an SSH config gets for its
// ProxyCommand, and the line that tells the user. The command would run here,
// so it stays unconfirmed until the user saves it in the host's proxy dialog. A
// command Motty cannot run is left out (nil) and the line says so.
func importedProxyCommand(m *i18n.Messages, source sshconfig.Host) (*outboundProxy, string) {
	if sshconn.CheckProxyCommand(source.ProxyCommand) != nil {
		return nil, fmt.Sprintf(m.SSHConfigWarningProxyCommandFmt, source.Alias)
	}
	proxy := &outboundProxy{kind: sshconn.ProxyCommand, address: source.ProxyCommand, unconfirmed: true}
	return proxy, fmt.Sprintf(m.SSHConfigProxyCommandFmt, source.Alias)
}

func (builder *hostImportBuilder) addIdentity(path string) {
	if path == "" {
		return
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		absolute = filepath.Clean(path)
	}
	if _, exists := builder.identitySet[absolute]; exists {
		return
	}
	record := inspectIdentity(builder.plan.nextIdentityID, absolute, builder.state.keyMessages())
	if record.Error != "" {
		if _, err := keyidentity.PublicKey(absolute); err == nil {
			return
		}
		builder.warn(fmt.Sprintf(builder.state.messages.SSHConfigWarningIdentityFmt, filepath.Base(path), record.Error))
		return
	}
	builder.plan.nextIdentityID++
	builder.plan.identities = append(builder.plan.identities, record)
	builder.identitySet[record.Path] = struct{}{}
}

func (builder *hostImportBuilder) addProxyChain(source sshconfig.Host) {
	targetID, ok := builder.aliasIDs[strings.ToLower(source.Alias)]
	if !ok {
		builder.warn(fmt.Sprintf(builder.state.messages.SSHConfigWarningProxyFmt, source.Alias))
		return
	}
	jumps := make([]uint32, len(source.ProxyJump))
	for index, jump := range source.ProxyJump {
		jumpID, ok := builder.resolveJump(jump)
		if !ok {
			builder.warn(fmt.Sprintf(builder.state.messages.SSHConfigWarningProxyFmt, source.Alias))
			return
		}
		jumps[index] = jumpID
	}
	candidate := make(map[uint32]uint32, len(builder.routes)+len(jumps))
	for target, jump := range builder.routes {
		candidate[target] = jump
	}
	type edge struct{ target, jump uint32 }
	edges := make([]edge, 0, len(jumps))
	current := targetID
	for index := len(jumps) - 1; index >= 0; index-- {
		jump := jumps[index]
		if current == jump {
			builder.warn(fmt.Sprintf(builder.state.messages.SSHConfigWarningProxyFmt, source.Alias))
			return
		}
		if configured, exists := candidate[current]; exists {
			if configured != jump {
				builder.warn(fmt.Sprintf(builder.state.messages.SSHConfigWarningProxyFmt, source.Alias))
				return
			}
			current = jump
			continue
		}
		if proxyRouteWouldCycle(candidate, current, jump) {
			builder.warn(fmt.Sprintf(builder.state.messages.SSHConfigWarningProxyFmt, source.Alias))
			return
		}
		candidate[current] = jump
		edges = append(edges, edge{target: current, jump: jump})
		current = jump
	}
	for _, edge := range edges {
		route := proxyRecord{ID: builder.plan.nextProxyID, TargetID: edge.target, JumpID: edge.jump}
		builder.plan.nextProxyID++
		builder.plan.proxies = append(builder.plan.proxies, route)
	}
	builder.routes = candidate
}

func (builder *hostImportBuilder) resolveJump(jump sshconfig.JumpHost) (uint32, bool) {
	address, user, port := jump.Host, jump.User, jump.Port
	if source, exists := builder.sources[strings.ToLower(jump.Host)]; exists {
		if !validImportedAddress(source.HostName) {
			return 0, false
		}
		address = source.HostName
		if user == "" {
			user = source.User
		}
		if port == 0 {
			port = source.Port
		}
	}
	if !validImportedAddress(address) {
		return 0, false
	}
	if port == 0 {
		port = defaultPort
	}
	host, _ := builder.ensureHost(jump.Host, address, user, port)
	return host.ID, true
}

func (builder *hostImportBuilder) ensureHost(label, address, user string, port uint16) (Host, bool) {
	key := connectionKey(address, user, port)
	if host, exists := builder.connections[key]; exists {
		return host, false
	}
	host := Host{ID: builder.plan.nextHostID, Label: label, Address: address, User: user, Port: port, CatalogID: freshCatalogID(builder.usedIDs)}
	host.finish()
	builder.plan.nextHostID++
	builder.plan.hosts = append(builder.plan.hosts, host)
	builder.connections[key] = host
	return host, true
}

func (builder *hostImportBuilder) warn(message string) {
	for _, existing := range builder.plan.warnings {
		if existing == message {
			return
		}
	}
	builder.plan.warnings = append(builder.plan.warnings, message)
}

func (state *State) applyHostImport(plan hostImportPlan) {
	summary := fmt.Sprintf(state.messages.SSHConfigSummaryFmt, len(plan.hosts), len(plan.identities), len(plan.proxies), plan.duplicateHosts)
	state.applyHostImportSummary(plan, summary)
}

// applyHostImportSummary adds what a plan found and tells the user in summary.
func (state *State) applyHostImportSummary(plan hostImportPlan, summary string) error {
	state.hosts = append(state.hosts, plan.hosts...)
	state.identities = append(state.identities, plan.identities...)
	state.proxies = append(state.proxies, plan.proxies...)
	state.nextHostID = plan.nextHostID
	state.nextIdentityID = plan.nextIdentityID
	state.nextProxyID = plan.nextProxyID
	if len(plan.hosts) > 0 {
		state.selectedHostID = plan.hosts[0].ID
	}
	if len(plan.identities) > 0 {
		state.selectedIdentityID = plan.identities[len(plan.identities)-1].ID
	}
	if len(plan.proxies) > 0 {
		state.selectedProxyID = plan.proxies[len(plan.proxies)-1].ID
	}
	state.hostDataChanged()
	state.refreshIdentityCount()
	state.refreshProxies()

	state.hostImportResult.set(state.messages, summary, plan.warnings)
	state.status = summary
	state.modal = modalHostImportResult
	if len(plan.hosts)+len(plan.identities)+len(plan.proxies) > 0 {
		return state.saveWithResult()
	}
	return nil
}

func (result *hostImportResult) set(messages i18n.Messages, summary string, warnings []string) {
	result.title = ""
	result.summary = summary
	result.warningsTitle = ""
	result.more = ""
	result.visible = min(len(warnings), len(result.warnings))
	for index := range result.warnings {
		result.warnings[index] = ""
	}
	copy(result.warnings[:], warnings[:result.visible])
	if len(warnings) > 0 {
		result.warningsTitle = fmt.Sprintf(messages.SSHConfigWarningsFmt, len(warnings))
	}
	if len(warnings) > result.visible {
		result.more = fmt.Sprintf(messages.SSHConfigWarningMoreFmt, len(warnings)-result.visible)
	}
}

func (state *State) clickHostImportResult(id gfx.ID) {
	switch id {
	case hitHostImportDismiss, hitHostImportClose, hitHostImportDone:
		state.modal = modalNone
		state.showNextPrompt()
	}
}

func connectionKey(address, user string, port uint16) hostConnection {
	if port == 0 {
		port = defaultPort
	}
	return hostConnection{address: address, user: user, port: port}
}

func validImportedAddress(address string) bool {
	return address != "" && !strings.ContainsAny(address, " \t\r\n\x00")
}

// gitHostingHosts are the public forges a Git remote reaches over SSH. They
// offer no shell, so a block that points at one is a Git alias even when the
// remote URL, not the block, names the "git" user.
var gitHostingHosts = map[string]bool{
	"github.com":              true,
	"ssh.github.com":          true,
	"gitlab.com":              true,
	"altssh.gitlab.com":       true,
	"gitee.com":               true,
	"bitbucket.org":           true,
	"codeberg.org":            true,
	"git.sr.ht":               true,
	"ssh.dev.azure.com":       true,
	"vs-ssh.visualstudio.com": true,
}

// gitRemoteAlias reports whether a config block is an alias for a Git remote
// rather than a host to open: a Git server is asked to run Git as the user
// "git", and the public forges have no shell to offer either way.
func gitRemoteAlias(host sshconfig.Host) bool {
	if strings.EqualFold(strings.TrimSpace(host.User), "git") {
		return true
	}
	return gitHostingHosts[strings.ToLower(strings.TrimSpace(host.HostName))]
}

func proxyRouteWouldCycle(routes map[uint32]uint32, target, jump uint32) bool {
	current := jump
	for range len(routes) + 1 {
		if current == target {
			return true
		}
		next, exists := routes[current]
		if !exists {
			return false
		}
		current = next
	}
	return true
}
