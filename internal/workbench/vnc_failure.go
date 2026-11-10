package workbench

import (
	"errors"
	"fmt"
	"strings"

	"motty/internal/rfb"
)

func (state *State) vncFailureDescription(err error) string {
	var unsupported *rfb.UnsupportedSecurityError
	if !errors.As(err, &unsupported) || len(unsupported.Types) == 0 {
		return ""
	}
	names := make([]string, len(unsupported.Types))
	for index, value := range unsupported.Types {
		if unsupported.VeNCrypt {
			names[index] = rfb.VeNCryptSubtypeName(value)
		} else {
			names[index] = rfb.SecurityTypeName(value)
		}
	}
	message := state.messages.Desktop.VNCUnsupportedSecurityFmt
	if unsupported.VeNCrypt {
		message = state.messages.Desktop.VNCUnsupportedVeNCryptFmt
	} else if unsupported.AuthenticationOnly {
		message = state.messages.Desktop.VNCAuthOnlyPermissionFmt
	}
	return fmt.Sprintf(message, strings.Join(names, ", "))
}
