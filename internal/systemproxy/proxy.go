// Package systemproxy reads the proxy this computer's network settings name and
// turns it into the environment variables command-line programs read.
package systemproxy

import (
	"strconv"
	"strings"
	"time"
)

// lookupTimeout bounds the settings tools Lookup runs, so a stuck desktop
// service never holds up a terminal.
const lookupTimeout = 2 * time.Second

// Settings is a system proxy as URLs, with the hosts that bypass it as a
// NO_PROXY list. An empty field is no proxy for that protocol.
type Settings struct {
	HTTP   string
	HTTPS  string
	All    string
	Bypass string
}

// Enabled reports whether settings names any proxy server.
func (settings Settings) Enabled() bool {
	return settings.HTTP != "" || settings.HTTPS != "" || settings.All != ""
}

// Environment is settings as proxy variables, each in both spellings and empty
// where settings has none, so that no inherited value mixes with them.
func Environment(settings Settings) []string {
	values := []struct {
		name  string
		value string
	}{
		{"HTTP_PROXY", settings.HTTP},
		{"HTTPS_PROXY", settings.HTTPS},
		{"ALL_PROXY", settings.All},
		{"NO_PROXY", settings.Bypass},
	}
	environment := make([]string, 0, 2*len(values))
	for _, value := range values {
		environment = append(environment, value.name+"="+value.value, strings.ToLower(value.name)+"="+value.value)
	}
	return environment
}

func proxyURL(address, scheme string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.Contains(address, "://") {
		return address
	}
	return scheme + "://" + address
}

func bypassList(entries []string) string {
	seen := map[string]bool{}
	var list []string
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, "<local>") {
			for _, local := range []string{"localhost", "127.0.0.1", "::1"} {
				if !seen[local] {
					seen[local] = true
					list = append(list, local)
				}
			}
			continue
		}
		if strings.HasPrefix(entry, "*.") {
			entry = entry[1:]
		}
		if network, ok := ipv4PrefixNetwork(entry); ok {
			entry = network
		}
		// NO_PROXY matches domain suffixes and networks, not other patterns.
		if entry != "*" && strings.Contains(entry, "*") {
			continue
		}
		if !seen[entry] {
			seen[entry] = true
			list = append(list, entry)
		}
	}
	return strings.Join(list, ",")
}

// ipv4PrefixNetwork is an address pattern such as 192.168.* as the network it
// covers, 192.168.0.0/16: the form NO_PROXY readers such as curl and Go take.
func ipv4PrefixNetwork(pattern string) (string, bool) {
	prefix, ok := strings.CutSuffix(pattern, ".*")
	if !ok {
		return "", false
	}
	octets := strings.Split(prefix, ".")
	if len(octets) > 3 {
		return "", false
	}
	for _, octet := range octets {
		value, err := strconv.Atoi(octet)
		if err != nil || value < 0 || value > 255 || strconv.Itoa(value) != octet {
			return "", false
		}
	}
	bits := 8 * len(octets)
	for len(octets) < 4 {
		octets = append(octets, "0")
	}
	return strings.Join(octets, ".") + "/" + strconv.Itoa(bits), true
}
