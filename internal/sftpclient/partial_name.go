package sftpclient

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"unicode/utf8"
)

// Retain ordinary partial names for resumability. Near a filesystem's component
// limit, prefixing the original name would make even a valid destination fail.
// Remote names use the common POSIX byte limit; local NTFS names use UTF-16 units.
func transferPartialName(base string, local bool) string {
	length := len(base)
	if local && runtime.GOOS == "windows" {
		length = utf8.RuneCountInString(base)
		for _, r := range base {
			if r > 0xffff {
				length++
			}
		}
	}
	if length+len(".motty..part") <= 255 {
		return ".motty." + base + ".part"
	}
	digest := sha256.Sum256([]byte(base))
	return fmt.Sprintf(".motty.%x.part", digest[:16])
}
