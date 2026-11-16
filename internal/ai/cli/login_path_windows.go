//go:build windows

package cli

// Windows processes inherit the user's PATH, so no login shell is consulted.
func loginPath() string { return "" }
