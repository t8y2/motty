//go:build (!darwin || ios) && !windows && !linux

package rdp

func platformMicDevice(micFormat) (micDevice, error) { return nil, errMicUnsupported }
