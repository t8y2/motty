package workbench

import (
	"strings"

	"motty/internal/sshconn"
)

// algoMode says which SSH algorithms a host negotiates.
type algoMode uint8

const (
	// algoDefault is the library's own set, which leaves out algorithms with
	// known weaknesses.
	algoDefault algoMode = iota
	// algoCompatible adds the weak ones after the strong, to reach old devices.
	algoCompatible
	// algoCustom is the list the user wrote.
	algoCustom
)

// hostAlgo is a host's SSH algorithm choice other than the default.
type hostAlgo struct {
	mode algoMode
	list string // the user's list, for algoCustom
}

func (host Host) algoMode() algoMode {
	if host.Algo == nil {
		return algoDefault
	}
	return host.Algo.mode
}

// algorithms is what the connection layer is told for this host. A custom list
// that no longer parses (a state file edited by hand) falls back to the defaults
// rather than to something wider.
func (host Host) algorithms() sshconn.Algorithms {
	if host.Algo == nil {
		return sshconn.Algorithms{}
	}
	switch host.Algo.mode {
	case algoCompatible:
		return sshconn.CompatibleAlgorithms()
	case algoCustom:
		parsed, err := sshconn.ParseAlgorithms(host.Algo.list)
		if err != nil {
			return sshconn.Algorithms{}
		}
		return parsed
	}
	return sshconn.Algorithms{}
}

// savedAlgo is the state-file and catalog form.
func (host Host) savedAlgo() (mode, list string) {
	switch host.algoMode() {
	case algoCompatible:
		return "compatible", ""
	case algoCustom:
		return "custom", host.Algo.list
	}
	return "", ""
}

// applyAlgo reads the saved form. It reports false for a custom list that does
// not parse, so a damaged entry is dropped instead of connecting with a guess.
func (host *Host) applyAlgo(mode, list string) bool {
	switch mode {
	case "":
		host.Algo = nil
	case "compatible":
		host.Algo = &hostAlgo{mode: algoCompatible}
	case "custom":
		list = strings.TrimSpace(list)
		if _, err := sshconn.ParseAlgorithms(list); err != nil || list == "" {
			return false
		}
		host.Algo = &hostAlgo{mode: algoCustom, list: list}
	default:
		return false
	}
	return true
}
