//go:build !windows && !darwin && !linux

package systemproxy

func Lookup() Settings {
	return Settings{}
}
