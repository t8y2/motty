package workbench

import (
	"reflect"
	"testing"
)

func TestSystemFolderCommandOpensDirectoryAsSingleArgument(t *testing.T) {
	for _, tc := range []struct {
		goos, directory, executable string
	}{
		{"linux", "/data/workspace/a folder; $(echo test)", "xdg-open"},
		{"darwin", "/Users/test/a folder", "open"},
		{"windows", `C:\Users\test\a folder`, "explorer.exe"},
		{"windows", `\\wsl.localhost\Ubuntu\home\test`, "explorer.exe"},
	} {
		t.Run(tc.goos+tc.directory, func(t *testing.T) {
			command := systemFolderCommand(tc.goos, tc.directory)
			if command == nil || !reflect.DeepEqual(command.Args, []string{tc.executable, tc.directory}) {
				t.Fatalf("unexpected file manager command: %+v", command)
			}
		})
	}
}
