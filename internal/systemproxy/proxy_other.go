//go:build !windows && !darwin && !linux

package systemproxy

// Lookup knows no system proxy setting on this platform.
func Lookup() Settings {
	return Settings{}
}
