package sshconn

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Algorithms narrows or widens what one endpoint negotiates. An empty list keeps
// the library's own choice for that kind, which excludes the algorithms with
// known weaknesses; those have to be asked for by name.
type Algorithms struct {
	KeyExchanges []string
	Ciphers      []string
	MACs         []string
	HostKeys     []string
}

// IsZero reports whether the library's defaults apply to everything.
func (algorithms Algorithms) IsZero() bool {
	return len(algorithms.KeyExchanges)+len(algorithms.Ciphers)+len(algorithms.MACs)+len(algorithms.HostKeys) == 0
}

// CompatibleAlgorithms offers everything the library implements: the defaults
// first, in their order, then the weak ones, so a modern server still gets a
// modern choice and an old device can be reached at all.
func CompatibleAlgorithms() Algorithms {
	supported, weak := ssh.SupportedAlgorithms(), ssh.InsecureAlgorithms()
	return Algorithms{
		KeyExchanges: slices.Concat(supported.KeyExchanges, weak.KeyExchanges),
		Ciphers:      slices.Concat(supported.Ciphers, weak.Ciphers),
		MACs:         slices.Concat(supported.MACs, weak.MACs),
		HostKeys:     slices.Concat(supported.HostKeys, weak.HostKeys),
	}
}

// UnknownAlgorithmError names a requested algorithm this build does not implement.
type UnknownAlgorithmError struct{ Name string }

func (err *UnknownAlgorithmError) Error() string {
	return fmt.Sprintf("sshconn: %q is not an algorithm this program implements", err.Name)
}

// ParseAlgorithms reads names separated by commas or blanks and sorts each into
// the kind it belongs to, keeping the order given: the first is preferred. A
// kind the list does not mention keeps the defaults. A name this build does not
// implement is an error rather than silently ignored, so a typo cannot quietly
// leave a connection on the defaults.
func ParseAlgorithms(list string) (Algorithms, error) {
	supported, weak := ssh.SupportedAlgorithms(), ssh.InsecureAlgorithms()
	var result Algorithms
	type kind struct {
		known  []string
		target *[]string
	}
	kinds := []kind{
		{slices.Concat(supported.KeyExchanges, weak.KeyExchanges), &result.KeyExchanges},
		{slices.Concat(supported.Ciphers, weak.Ciphers), &result.Ciphers},
		{slices.Concat(supported.MACs, weak.MACs), &result.MACs},
		{slices.Concat(supported.HostKeys, weak.HostKeys), &result.HostKeys},
	}
	for _, name := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		placed := false
		for _, kind := range kinds {
			if slices.Contains(kind.known, name) {
				if !slices.Contains(*kind.target, name) {
					*kind.target = append(*kind.target, name)
				}
				placed = true
				break
			}
		}
		if !placed {
			return Algorithms{}, &UnknownAlgorithmError{Name: name}
		}
	}
	return result, nil
}

// weakNames is every algorithm the library marks as having known weaknesses.
func weakNames() []string {
	weak := ssh.InsecureAlgorithms()
	return slices.Concat(weak.KeyExchanges, weak.Ciphers, weak.MACs, weak.HostKeys)
}

// Weak lists the algorithms among the offered ones that have known weaknesses.
func (algorithms Algorithms) Weak() []string {
	var found []string
	known := weakNames()
	for _, list := range [][]string{algorithms.KeyExchanges, algorithms.Ciphers, algorithms.MACs, algorithms.HostKeys} {
		for _, name := range list {
			if slices.Contains(known, name) && !slices.Contains(found, name) {
				found = append(found, name)
			}
		}
	}
	return found
}

// Negotiated is what the two ends agreed on for one connection.
type Negotiated struct {
	KeyExchange string
	HostKey     string
	Cipher      string
	MAC         string
}

func (negotiated Negotiated) String() string {
	parts := []string{negotiated.KeyExchange, negotiated.HostKey, negotiated.Cipher}
	if negotiated.MAC != "" {
		parts = append(parts, negotiated.MAC)
	}
	return strings.Join(parts, " · ")
}

// Weak lists the agreed algorithms that have known weaknesses.
func (negotiated Negotiated) Weak() []string {
	known := weakNames()
	var found []string
	for _, name := range []string{negotiated.KeyExchange, negotiated.HostKey, negotiated.Cipher, negotiated.MAC} {
		if name != "" && slices.Contains(known, name) && !slices.Contains(found, name) {
			found = append(found, name)
		}
	}
	return found
}

func negotiatedFrom(metadata ssh.ConnMetadata) (Negotiated, bool) {
	reporter, ok := metadata.(ssh.AlgorithmsConnMetadata)
	if !ok {
		return Negotiated{}, false
	}
	agreed := reporter.Algorithms()
	return Negotiated{
		KeyExchange: agreed.KeyExchange, HostKey: agreed.HostKey,
		Cipher: agreed.Read.Cipher, MAC: agreed.Read.MAC,
	}, true
}

// hostKeyAlgorithms picks what to offer for the host key. known are the
// signature algorithms of keys already on file for the host (nil for a new one),
// wanted the caller's list (nil for the defaults). A known key narrows the offer
// to it, so a server with several key types is checked against the one the user
// trusted, as before.
func hostKeyAlgorithms(known, wanted []string) []string {
	if len(wanted) == 0 {
		return known
	}
	if len(known) == 0 {
		return wanted
	}
	var offer []string
	for _, name := range wanted {
		if slices.Contains(known, name) {
			offer = append(offer, name)
		}
	}
	if len(offer) == 0 {
		return known
	}
	return offer
}
