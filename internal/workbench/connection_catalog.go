package workbench

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"motty/internal/sshconn"
	"motty/internal/vault"
)

// hostCatalog is what it takes to turn a saved host into a connection: the
// hosts and the jump routes between them, the private keys the user imported,
// the outbound proxy, and the timeouts. It draws nothing and owns no window, so
// the workbench and the headless MCP server resolve a host the same way. State
// embeds one; its methods are the State's own.
type hostCatalog struct {
	hosts      []Host
	proxies    []proxyRecord
	identities []identityRecord
	// proxy is the outbound proxy every host inherits unless it names its own.
	proxy             outboundProxy
	connectionTimeout time.Duration
	// sshAgent is the ssh-agent connections offer their keys to.
	sshAgent            agentChoice
	connectionKeepAlive time.Duration
	// systemUser is the login used for a host that names none.
	systemUser string
	// agentHosts holds the catalog ids of the hosts an agent may use without a
	// window (see savedState.AgentHosts).
	agentHosts map[string]bool
}

// connectEnv is what resolving a connection needs beyond the catalog.
type connectEnv struct {
	// KnownHostsPath is the file of trusted host keys.
	KnownHostsPath string
	// Notify is called after every change of the connection's status.
	Notify func()
	// Passwords answers the saved password of an outbound proxy; nil means none
	// is saved.
	Passwords passwordReader
	// ProxyLocked is what a proxy reports when its password is in a locked vault.
	ProxyLocked error
}

// passwordReader looks up a saved secret by name. *vault.Vault is one.
type passwordReader interface {
	Get(name string) (value string, found bool, err error)
}

// connectionRoute is what resolving a host also learns about how it is reached.
type connectionRoute struct {
	// proxyKey identifies the non-secret outbound route, so a live connection
	// through another route is not shared.
	proxyKey string
	// jumpPath is the chain of jump hosts, "a → b", or empty for a direct host.
	jumpPath string
}

// login is the account a connection to host signs in as.
func (catalog *hostCatalog) login(host Host) string {
	if host.User != "" {
		return host.User
	}
	return catalog.systemUser
}

func (catalog *hostCatalog) hostByID(id uint32) (Host, bool) {
	for _, host := range catalog.hosts {
		if host.ID == id {
			return host, true
		}
	}
	return Host{}, false
}

func (catalog *hostCatalog) hostByConnection(address, user string, port uint16) (Host, bool) {
	if port == 0 {
		port = defaultPort
	}
	for _, host := range catalog.hosts {
		if host.isSSH() && host.Address == address && host.User == user && host.Port == port {
			return host, true
		}
	}
	return Host{}, false
}

// secretOwner is the catalog ID of the host that the secrets of a connection
// prompt belong to: the host that was opened, or, for a prompt of a jump host
// along the way, the saved host that is that jump host. A jump host nobody saved
// gets the ID a host with its address, login and port would have been given.
func (catalog *hostCatalog) secretOwner(target Host, endpoint sshconn.Endpoint, role sshconn.EndpointRole) string {
	if role == sshconn.EndpointTarget {
		if target.CatalogID == "" {
			return catalogIDFor(target)
		}
		return target.CatalogID
	}
	if jump, ok := catalog.hostByConnection(endpoint.Host, endpoint.User, endpoint.Port); ok {
		return jump.CatalogID
	}
	return catalogIDFor(Host{Protocol: protocolSSH, Address: endpoint.Host, User: endpoint.User, Port: endpoint.Port})
}

func (catalog *hostCatalog) identityPaths() []string {
	paths := make([]string, 0, len(catalog.identities))
	for _, identity := range catalog.identities {
		if identity.Error == "" {
			paths = append(paths, identity.Path)
		}
	}
	return paths
}

func (catalog *hostCatalog) identityForFingerprint(fingerprint string) (identityRecord, bool) {
	for _, identity := range catalog.identities {
		if identity.Error == "" && identity.Fingerprint == fingerprint {
			return identity, true
		}
	}
	return identityRecord{}, false
}

func (catalog *hostCatalog) authenticationFor(host Host) sshconn.Authentication {
	switch host.Auth {
	case authPassword:
		return sshconn.Authentication{
			Methods:       sshconn.AuthKeyboardInteractive | sshconn.AuthPassword,
			IdentityFiles: []string{},
		}
	case authPrivateKey:
		if fingerprint := strings.TrimSpace(host.Identity); fingerprint != "" {
			if identity, ok := catalog.identityForFingerprint(fingerprint); ok {
				return sshconn.Authentication{Methods: sshconn.AuthPublicKey, IdentityFiles: []string{identity.Path}}
			}
			return sshconn.Authentication{Methods: sshconn.AuthPublicKey, IdentityFiles: []string{}}
		}
		return sshconn.Authentication{
			Methods:                 sshconn.AuthPublicKey,
			AdditionalIdentityFiles: catalog.identityPaths(),
		}
	case authAgent:
		return sshconn.Authentication{
			Methods:       sshconn.AuthPublicKey,
			IdentityFiles: []string{},
			Agent:         true,
		}
	default:
		return sshconn.Authentication{
			Methods:                 sshconn.AuthAll,
			AdditionalIdentityFiles: catalog.identityPaths(),
			Agent:                   true,
		}
	}
}

// algorithmsFor maps each endpoint a connection touches to its host's algorithms.
func (catalog *hostCatalog) algorithmsFor(target Host, jumps []Host) map[string]sshconn.Algorithms {
	var result map[string]sshconn.Algorithms
	for _, host := range append([]Host{target}, jumps...) {
		if algorithms := host.algorithms(); !algorithms.IsZero() && host.isSSH() {
			if result == nil {
				result = make(map[string]sshconn.Algorithms)
			}
			result[sshconn.EndpointKey(host.Address, host.Port)] = algorithms
		}
	}
	return result
}

func (catalog *hostCatalog) proxyTargetConfigured(hostID uint32) bool {
	for _, route := range catalog.proxies {
		if route.TargetID == hostID {
			return true
		}
	}
	return false
}

// proxyChainFor follows target's jump routes from the immediate jump outward.
// The boolean is false when the in-memory graph is damaged or cyclic.
func (catalog *hostCatalog) proxyChainFor(target Host) ([]Host, bool) {
	var chain []Host
	current := target
	for range len(catalog.proxies) + 1 {
		jump, routed, valid := catalog.proxyJump(current.ID)
		if !routed {
			return chain, true
		}
		if !valid {
			return nil, false
		}
		for _, seen := range chain {
			if seen.ID == jump.ID {
				return nil, false
			}
		}
		if jump.ID == target.ID {
			return nil, false
		}
		if chain == nil {
			chain = make([]Host, 0, min(len(catalog.proxies), 4))
		}
		chain = append(chain, jump)
		current = jump
	}
	return nil, false
}

func (catalog *hostCatalog) proxyJump(targetID uint32) (Host, bool, bool) {
	for _, route := range catalog.proxies {
		if route.TargetID != targetID {
			continue
		}
		jump, ok := catalog.hostByID(route.JumpID)
		return jump, true, ok
	}
	return Host{}, false, true
}

func (catalog *hostCatalog) proxyWouldCycle(targetID, jumpID uint32) bool {
	current := jumpID
	for range len(catalog.proxies) + 1 {
		if current == targetID {
			return true
		}
		jump, routed, valid := catalog.proxyJump(current)
		if !routed {
			return false
		}
		if !valid {
			return true
		}
		current = jump.ID
	}
	return true
}

// proxyWithPassword is the saved proxy with its password, as a connection is
// given it, or nil for none.
func (catalog *hostCatalog) proxyWithPassword(saved outboundProxy, passwordSecret string, env connectEnv) *sshconn.Proxy {
	if saved.kind == sshconn.ProxyNone || saved.address == "" {
		return nil
	}
	proxy := &sshconn.Proxy{Kind: saved.kind, Address: saved.address, Username: saved.user, Bypass: splitBypass(saved.bypass)}
	if saved.user != "" && env.Passwords != nil {
		password, found, err := env.Passwords.Get(passwordSecret)
		switch {
		case errors.Is(err, vault.ErrNeedsConversion):
			proxy.Err = env.ProxyLocked
		case err == nil && found:
			proxy.Password = password
		}
	}
	return proxy
}

// proxyForHost resolves a host's policy at connection time so inherited hosts
// observe later changes to the global proxy.
func (catalog *hostCatalog) proxyForHost(host Host, env connectEnv) *sshconn.Proxy {
	if host.Protocol == protocolSerial {
		return nil
	}
	if host.Proxy == nil {
		return catalog.proxyWithPassword(catalog.proxy, proxyPasswordSecret, env)
	}
	return catalog.proxyWithPassword(*host.Proxy, hostProxyPasswordSecret(host.CatalogID, host.Proxy), env)
}

// sshConfigFor is how to connect to an SSH host: its endpoint, identities and
// keep-alive, and the jump chain its route names, which it also reports as the
// route. valid is false when the route is broken, and the config then has no
// host to dial. The outbound proxy is the policy of proxyHost, which is host
// itself except for the gateway of a tunneled desktop host: that connection
// leaves by the desktop host's proxy.
func (catalog *hostCatalog) sshConfigFor(host, proxyHost Host, env connectEnv) (config sshconn.Config, route connectionRoute, valid bool) {
	proxy := catalog.proxyForHost(proxyHost, env)
	route.proxyKey = proxyRouteKey(proxy)
	config = sshconn.Config{
		Host: host.Address, Port: host.Port, User: catalog.login(host),
		KnownHostsPath:          env.KnownHostsPath,
		Agent:                   true,
		AgentAddress:            catalog.sshAgent.resolved(),
		DialTimeout:             catalog.connectionTimeout,
		AdditionalIdentityFiles: catalog.identityPaths(),
		Authentication:          make(map[sshconn.Endpoint]sshconn.Authentication, 1),
		Notify:                  env.Notify,
		Proxy:                   proxy,
	}
	targetEndpoint := sshconn.Endpoint{Host: host.Address, Port: host.Port, User: catalog.login(host)}
	config.Authentication[targetEndpoint] = catalog.authenticationFor(host)
	if catalog.connectionKeepAlive > 0 {
		config.KeepAlive = sshconn.KeepAliveConfig{Interval: catalog.connectionKeepAlive, Timeout: 10 * time.Second}
	}
	chain, valid := catalog.proxyChainFor(host)
	config.Algorithms = catalog.algorithmsFor(host, chain)
	disableKeepAliveForTunnel(host, &config)
	if !valid {
		config.Host = ""
	} else if len(chain) > 0 {
		config.ProxyJumps = make([]sshconn.Endpoint, len(chain))
		config.Authentication = make(map[sshconn.Endpoint]sshconn.Authentication, len(chain)+1)
		config.Authentication[targetEndpoint] = catalog.authenticationFor(host)
		var path strings.Builder
		for source := len(chain) - 1; source >= 0; source-- {
			jump := chain[source]
			endpoint := sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: catalog.login(jump)}
			config.ProxyJumps[len(chain)-1-source] = endpoint
			config.Authentication[endpoint] = catalog.authenticationFor(jump)
			if path.Len() > 0 {
				path.WriteString(" → ")
			}
			path.WriteString(formatEndpoint(endpoint))
		}
		route.jumpPath = path.String()
	}
	return config, route, valid
}

// decodeHosts turns saved host records into hosts. A host whose catalog id is in
// known keeps that run-local number; the others are numbered from next on. It
// returns the hosts and the first number still unused. A record without a label
// or an address, or whose transport does not parse, is dropped.
func decodeHosts(saved []savedHost, known map[string]uint32, next uint32) (hosts []Host, unused uint32) {
	next = max(next, 1)
	hosts = make([]Host, 0, len(saved))
	usedIDs := make(map[string]struct{}, len(saved))
	for _, entry := range saved {
		if entry.Label == "" || entry.Address == "" {
			continue
		}
		id, kept := known[entry.ID]
		if !kept {
			id = next
			next++
		}
		host := Host{
			ID: id, CatalogID: entry.ID, Label: entry.Label, Group: strings.TrimSpace(entry.Group),
			Address: entry.Address, User: entry.User, Port: entry.Port, System: sshconn.ParseRemoteSystem(entry.System),
			InitScript: entry.InitScript,
		}
		if !host.applyTransport(entry.savedTransport) {
			if !kept {
				next--
			}
			continue
		}
		host.Icon, _ = parseHostIcon(entry.Icon)
		host.Color, _ = normalizeHostColor(entry.Color)
		if tags, ok := normalizeHostTags(entry.Tags); ok {
			setHostTags(&host, tags)
		}
		host.normalizeEndpoint()
		host.finish()
		host.CatalogID = uniqueCatalogID(host.CatalogID, host, usedIDs)
		hosts = append(hosts, host)
	}
	return hosts, next
}

// decodeProxies turns saved jump routes into routes between hosts, numbered
// from 1. A route whose ends are unknown, that jumps a host through itself, that
// gives a target a second route, or that would close a loop, is dropped.
func decodeProxies(saved []savedProxy, hosts []Host) []proxyRecord {
	catalog := hostCatalog{hosts: hosts}
	next := uint32(1)
	for _, entry := range saved {
		target, targetOK := catalog.hostByConnection(entry.TargetAddress, entry.TargetUser, entry.TargetPort)
		jump, jumpOK := catalog.hostByConnection(entry.JumpAddress, entry.JumpUser, entry.JumpPort)
		if !targetOK || !jumpOK || target.ID == jump.ID || catalog.proxyTargetConfigured(target.ID) || catalog.proxyWouldCycle(target.ID, jump.ID) {
			continue
		}
		catalog.proxies = append(catalog.proxies, proxyRecord{ID: next, TargetID: target.ID, JumpID: jump.ID})
		next++
	}
	return catalog.proxies
}

// decodeOutboundProxy reads the saved outbound proxy. A damaged address means
// none, since it would only fail every connection.
func decodeOutboundProxy(entry *savedOutboundProxy) outboundProxy {
	if entry == nil {
		return outboundProxy{}
	}
	kind := proxyKindFromString(entry.Kind)
	if kind != sshconn.ProxyNone && !validProxyAddress(entry.Address) {
		kind = sshconn.ProxyNone
	}
	proxy := outboundProxy{kind: kind, bypass: strings.TrimSpace(entry.Bypass)}
	if kind != sshconn.ProxyNone {
		proxy.address, proxy.user = entry.Address, entry.User
	}
	return proxy
}

// agentHostSet is the set of catalog ids a saved list names.
func agentHostSet(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// agentHostList is the hosts an agent may use, in catalog order: the SSH hosts
// whose catalog id is in agentHosts.
func (catalog *hostCatalog) agentHostList() []Host {
	var allowed []Host
	for _, host := range catalog.hosts {
		if host.isSSH() && catalog.agentHosts[host.CatalogID] {
			allowed = append(allowed, host)
		}
	}
	return allowed
}

// decodeIdentities inspects the private-key files a saved list names, numbering
// them from 1 and skipping blanks and repeats.
func decodeIdentities(paths []string, messages keyMessages) []identityRecord {
	var identities []identityRecord
	for _, path := range paths {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(filepath.Clean(path))
		if err != nil {
			absolute = filepath.Clean(path)
		}
		if slices.ContainsFunc(identities, func(identity identityRecord) bool { return identity.Path == absolute }) {
			continue
		}
		identities = append(identities, inspectIdentity(uint32(len(identities))+1, path, messages))
	}
	return identities
}

// connectionTimes are the dial timeout and keep-alive a saved state asks for.
func connectionTimes(saved savedState) (timeout, keepAlive time.Duration) {
	return time.Duration(normalizeConnectionTimeoutSeconds(saved.ConnectionTimeoutSeconds)) * time.Second,
		time.Duration(normalizeConnectionKeepAliveSeconds(saved.ConnectionKeepAliveSeconds)) * time.Second
}

// catalogFromSaved is the catalog a saved state describes, built the way the
// window builds it when it starts, but with nothing to draw and no sessions.
func catalogFromSaved(saved savedState, systemUser string, messages keyMessages) hostCatalog {
	hosts, _ := decodeHosts(saved.Hosts, nil, 1)
	catalog := hostCatalog{
		hosts:      hosts,
		proxies:    decodeProxies(saved.Proxies, hosts),
		identities: decodeIdentities(saved.IdentityFiles, messages),
		proxy:      decodeOutboundProxy(saved.OutboundProxy),
		systemUser: systemUser,
		agentHosts: agentHostSet(saved.AgentHosts),
	}
	catalog.connectionTimeout, catalog.connectionKeepAlive = connectionTimes(saved)
	catalog.sshAgent = agentChoiceFromSaved(saved.SSHAgent, saved.SSHAgentPath)
	return catalog
}

// setAgentAccess opens a host to agents or closes it. Only an SSH host can be
// opened.
func (catalog *hostCatalog) setAgentAccess(host Host, open bool) {
	if !host.isSSH() || !open {
		delete(catalog.agentHosts, host.CatalogID)
		return
	}
	if catalog.agentHosts == nil {
		catalog.agentHosts = make(map[string]bool)
	}
	catalog.agentHosts[host.CatalogID] = true
}
