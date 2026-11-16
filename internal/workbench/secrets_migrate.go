package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"motty/internal/sshconn"
	"motty/internal/vault"
)

// Earlier versions kept the password and the one-time code secret of a login
// under its endpoint ("password:user@address:port"), which hosts at one endpoint
// shared and which an edited address left behind. They are kept under the host
// now (hostSecretName). This moves what an earlier version saved.

const (
	legacyPasswordPrefix = "password:"
	legacyOTPPrefix      = "otp:"
	// secretsLayoutFile, beside the vault, says the move was made. It belongs to
	// this computer: the vault's records travel to others, the move does not.
	secretsLayoutFile  = "secrets-layout"
	secretsLayoutHosts = "hosts"
)

func legacyEndpointName(prefix string, endpoint sshconn.Endpoint) string {
	return prefix + endpoint.User + "@" + endpoint.Host + ":" + strconv.Itoa(int(endpoint.Port))
}

// legacySecretNames are the names an earlier version could have kept the
// password and the one-time code secret of a host under.
func (state *State) legacySecretNames(host Host) (password, otp string) {
	endpoint := sshconn.Endpoint{Host: host.Address, Port: host.Port}
	switch host.Protocol {
	case protocolSSH:
		endpoint.User = state.login(host)
		return legacyEndpointName(legacyPasswordPrefix, endpoint), legacyEndpointName(legacyOTPPrefix, endpoint)
	case protocolVNC:
		endpoint.User = vncAccount
	case protocolRDP:
		endpoint.User = rdpAccountPrefix + host.User
	default:
		return "", ""
	}
	return legacyEndpointName(legacyPasswordPrefix, endpoint), ""
}

// parseLegacyEndpoint reads "user@address:port" back from a legacy secret name.
func parseLegacyEndpoint(rest string) (user, address string, port uint16, ok bool) {
	colon := strings.LastIndex(rest, ":")
	at := strings.LastIndex(rest[:max(colon, 0)], "@")
	if colon < 0 || at < 0 {
		return "", "", 0, false
	}
	number, err := strconv.ParseUint(rest[colon+1:], 10, 16)
	if err != nil {
		return "", "", 0, false
	}
	return rest[:at], rest[at+1 : colon], uint16(number), true
}

// migrateSecretsToHosts moves the secrets an earlier version saved under an
// endpoint to the hosts that log in there. Hosts that shared an endpoint each get
// a copy. Passwords typed at a prompt for a connection that was never saved as a
// host go to the host such a connection has (see connectParsed). The old records
// are removed only once every new one is written; a failure at any point leaves
// the old data as it was and the move is tried again at the next start.
//
// A vault that still needs its master password waits: the move runs again when
// it has been converted. force skips the "already done" mark, for a restore of a
// backup made before the change.
func (state *State) migrateSecretsToHosts(force bool) {
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.NeedsConversion() || state.options.StatePath == "" {
		return
	}
	mark := filepath.Join(filepath.Dir(state.options.StatePath), secretsLayoutFile)
	if data, err := os.ReadFile(mark); err == nil && string(data) == secretsLayoutHosts && !force {
		return
	}
	done := func() { _ = os.WriteFile(mark, []byte(secretsLayoutHosts), 0o600) }
	// An empty vault has nothing to move, and asking it would open the key store.
	if vaultFile.Len() == 0 {
		done()
		return
	}
	names, err := vaultFile.Names()
	if err != nil {
		return
	}
	existing := make(map[string]bool, len(names))
	for _, name := range names {
		existing[name] = true
	}

	var changes []vault.Change
	var consumed []string
	copied := make(map[string]bool)
	copyTo := func(from, to string) {
		if from == "" || to == "" || !existing[from] {
			return
		}
		consumed = append(consumed, from)
		if existing[to] || copied[to] {
			return
		}
		value, found, err := vaultFile.Get(from)
		if err != nil || !found {
			return
		}
		copied[to] = true
		changes = append(changes, vault.Change{Name: to, Value: value})
	}
	for _, host := range state.hosts {
		password, otp := state.legacySecretNames(host)
		copyTo(password, hostSecretName(host.CatalogID, hostPasswordPart))
		copyTo(otp, hostSecretName(host.CatalogID, hostOTPPart))
	}
	// What no saved host claims was typed at a prompt for a connection that was
	// never saved; such a connection is a host of its own, named by where it goes.
	for _, name := range names {
		prefix, part := "", ""
		switch {
		case strings.HasPrefix(name, legacyPasswordPrefix):
			prefix, part = legacyPasswordPrefix, hostPasswordPart
		case strings.HasPrefix(name, legacyOTPPrefix):
			prefix, part = legacyOTPPrefix, hostOTPPart
		default:
			continue
		}
		user, address, port, ok := parseLegacyEndpoint(strings.TrimPrefix(name, prefix))
		if !ok || user == vncAccount || strings.HasPrefix(user, rdpAccountPrefix) || slices.Contains(consumed, name) {
			continue
		}
		adhoc := Host{Protocol: protocolSSH, Address: address, User: user, Port: port}
		copyTo(name, hostSecretName(catalogIDFor(adhoc), part))
	}

	if len(changes) > 0 {
		if err := vaultFile.Apply(changes); err != nil {
			return
		}
	}
	var removals []vault.Change
	seen := make(map[string]bool, len(consumed))
	for _, name := range consumed {
		if !seen[name] {
			seen[name] = true
			removals = append(removals, vault.Change{Name: name, Delete: true})
		}
	}
	if len(removals) > 0 {
		if err := vaultFile.Apply(removals); err != nil {
			return
		}
	}
	done()
}
