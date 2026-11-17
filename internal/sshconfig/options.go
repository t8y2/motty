package sshconfig

import "slices"

type ConnectionOptions struct {
	ServerAliveInterval *int     `json:"server_alive_interval,omitempty"`
	ServerAliveCountMax *int     `json:"server_alive_count_max,omitempty"`
	IdentitiesOnly      *bool    `json:"identities_only,omitempty"`
	IdentityFiles       []string `json:"identity_files,omitempty"`
}

func (options *ConnectionOptions) Clone() *ConnectionOptions {
	if options == nil {
		return nil
	}
	copy := *options
	copy.ServerAliveInterval = cloneOption(options.ServerAliveInterval)
	copy.ServerAliveCountMax = cloneOption(options.ServerAliveCountMax)
	copy.IdentitiesOnly = cloneOption(options.IdentitiesOnly)
	copy.IdentityFiles = slices.Clone(options.IdentityFiles)
	return &copy
}

func cloneOption[value any](original *value) *value {
	if original == nil {
		return nil
	}
	copy := *original
	return &copy
}

func (options *ConnectionOptions) Equal(other *ConnectionOptions) bool {
	if options == nil || other == nil {
		return options == other
	}
	return equalOption(options.ServerAliveInterval, other.ServerAliveInterval) &&
		equalOption(options.ServerAliveCountMax, other.ServerAliveCountMax) &&
		equalOption(options.IdentitiesOnly, other.IdentitiesOnly) &&
		slices.Equal(options.IdentityFiles, other.IdentityFiles)
}

func equalOption[value comparable](left, right *value) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func (options *ConnectionOptions) Valid() bool {
	return options == nil ||
		((options.ServerAliveInterval == nil || *options.ServerAliveInterval >= 0 && *options.ServerAliveInterval <= 2147483647) &&
			(options.ServerAliveCountMax == nil || *options.ServerAliveCountMax >= 0 && *options.ServerAliveCountMax <= 2147483647))
}
