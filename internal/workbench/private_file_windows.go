//go:build windows

package workbench

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// restrictToOwner replaces the access list of a file with a single entry for
// its owner, and stops it inheriting from the folder. It works by name: a handle
// opened for writing does not carry the right to change the access list. Windows has no
// permission bits, and a new file otherwise takes the access list of the folder
// it is created in: an export folder readable by every signed-in user would make
// a private key readable by them too, and ssh-agent refuses such a key.
func restrictToOwner(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read the owner of the file: %w", err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		return fmt.Errorf("build the access list of the file: %w", err)
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil)
}
