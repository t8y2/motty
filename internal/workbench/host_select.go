package workbench

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
)

// hostSelectValue is the stable value a host picker keeps for a host. A picker
// with no choice stores nothing.
func hostSelectValue(id uint32) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(id), 10)
}

// hostSelectID reads back the host a picker chose.
func hostSelectID(value string) (uint32, bool) {
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint32(id), true
}

// hostSelectSearch is the text a host picker's query matches besides the
// host's label: what a user types to find a host among many, such as its
// address or account.
func hostSelectSearch(host Host) string {
	return strings.TrimSpace(strings.Join([]string{
		host.Label, host.target, host.Address, host.User, host.Group, host.Tags,
	}, " "))
}

// hostSelectOption is one host in a picker. Callers decide which hosts are
// disabled instead of dropping them, so a search can still explain why a host
// cannot be chosen.
func hostSelectOption(host Host, disabled bool) components.SingleSelectOption {
	return components.SingleSelectOption{
		Value:    hostSelectValue(host.ID),
		Label:    host.Label,
		Search:   hostSelectSearch(host),
		Disabled: disabled,
	}
}
