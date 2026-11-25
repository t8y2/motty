package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

func TestSFTPActionIconsRegistered(t *testing.T) {
	cases := []struct {
		name string
		id   uint8
		icon gfx.Icon
	}{
		{"Up", iconSFTPUp, icons.SftpUp},
		{"Upload", iconSFTPUpload, icons.SftpUpload},
		{"UploadFolder", iconSFTPUploadDirectory, icons.SftpUploadFolder},
		{"NewFile", iconSFTPNewFile, icons.SftpNewFile},
		{"NewFolder", iconSFTPNewDirectory, icons.SftpNewFolder},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registered := lucideIcons[tc.id]
			if len(registered.Commands) == 0 {
				t.Fatalf("lucideIcons[%d] (%s) has no commands", tc.id, tc.name)
			}
			if len(registered.Commands) != len(tc.icon.Commands) {
				t.Errorf("lucideIcons[%d] (%s) commands len %d != expected %d", tc.id, tc.name, len(registered.Commands), len(tc.icon.Commands))
			}
			if registered.Stroke != 1.6 {
				t.Errorf("lucideIcons[%d] (%s) stroke %v != expected 1.6", tc.id, tc.name, registered.Stroke)
			}
		})
	}
}

func TestSFTPToolbarCatalogUsesDedicatedIcons(t *testing.T) {
	sftpCatalog := toolbarCatalog[toolbarSFTP]
	expected := map[string]uint8{
		"upload-file":   iconSFTPUpload,
		"upload-folder": iconSFTPUploadDirectory,
		"new-file":      iconSFTPNewFile,
		"new-folder":    iconSFTPNewDirectory,
	}

	for _, spec := range sftpCatalog {
		if wantIcon, ok := expected[spec.key]; ok {
			if spec.icon != wantIcon {
				t.Errorf("spec %s has icon %d, want %d", spec.key, spec.icon, wantIcon)
			}
		}
	}
}
