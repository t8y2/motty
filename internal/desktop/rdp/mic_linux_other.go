//go:build linux && !amd64 && !arm64 && !android

package rdp

func platformMicDevice(micFormat) (micDevice, error) { return nil, errMicUnsupported }
