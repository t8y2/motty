//go:build windows

package workbench

import (
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A private file must not inherit the access list of the folder it lands in:
// a folder a user picks for an export can be readable by every signed-in user,
// and ssh-agent refuses a private key that anyone else can read.
func TestPrivateFileDoesNotInheritTheFolderACL(t *testing.T) {
	dir := t.TempDir()
	owner := currentUserSID(t)
	grantFolderReadToAuthenticatedUsers(t, dir, owner)

	path := filepath.Join(dir, "id_test")
	if err := writePrivateAtomic(path, []byte("key material")); err != nil {
		t.Fatal(err)
	}

	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("the file's access list still inherits from its folder")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if acl == nil {
		t.Fatal("the file has no access list, so every user can read it")
	}
	for i := range int(acl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, uint32(i), &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("ACE %d is not an allow entry", i)
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			t.Fatalf("ACE %d is inherited from the folder", i)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(owner) {
			t.Fatalf("ACE %d grants access to %s, not only to the owner", i, sid.String())
		}
	}
}

func currentUserSID(t *testing.T) *windows.SID {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := user.User.Sid.Copy()
	if err != nil {
		t.Fatal(err)
	}
	return sid
}

// grantFolderReadToAuthenticatedUsers gives the folder the access list of a
// user's own folder under a shared drive: its owner, and every signed-in user
// with read access, both inherited by everything created below it.
func grantFolderReadToAuthenticatedUsers(t *testing.T, dir string, owner *windows.SID) {
	t.Helper()
	authenticated, err := windows.StringToSid("S-1-5-11")
	if err != nil {
		t.Fatal(err)
	}
	entries := []windows.EXPLICIT_ACCESS{
		testTrusteeAccess(owner, windows.GENERIC_ALL),
		testTrusteeAccess(authenticated, windows.GENERIC_READ),
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func testTrusteeAccess(sid *windows.SID, permissions windows.ACCESS_MASK) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: permissions,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}
