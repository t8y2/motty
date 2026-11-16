//go:build darwin || windows || linux

package main

import (
	"os"
	"path/filepath"

	"motty/internal/buildinfo"
	"motty/internal/workbench"
)

func developmentImportSource(explicitDir string) (*workbench.DevelopmentSource, error) {
	if !buildinfo.Development || explicitDir != "" {
		return nil, nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &workbench.DevelopmentSource{Directory: filepath.Join(config, "motty")}, nil
}
