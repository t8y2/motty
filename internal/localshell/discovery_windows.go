//go:build windows

package localshell

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// InstalledWindowsShells looks in PATH and standard machine/per-user install
// locations. It checks executable files without starting the user's programs.
func InstalledWindowsShells() []Choice {
	pwsh := []string{"pwsh.exe"}
	bash := []string{}
	nu := []string{"nu.exe"}
	for _, root := range []string{os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")} {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		versions, _ := filepath.Glob(filepath.Join(root, "PowerShell", "*", "pwsh.exe"))
		slices.Sort(versions)
		slices.Reverse(versions)
		pwsh = append(pwsh, versions...)
		bash = append(bash, filepath.Join(root, "Git", "bin", "bash.exe"))
		nu = append(nu, filepath.Join(root, "nu", "bin", "nu.exe"), filepath.Join(root, "Nushell", "nu.exe"))
	}
	if root := os.Getenv("LOCALAPPDATA"); root != "" {
		versions, _ := filepath.Glob(filepath.Join(root, "Microsoft", "PowerShell", "*", "pwsh.exe"))
		pwsh = append(pwsh, versions...)
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		pwsh = append(pwsh, filepath.Join(home, "scoop", "apps", "pwsh", "current", "pwsh.exe"))
		bash = append(bash, filepath.Join(home, "scoop", "apps", "git", "current", "bin", "bash.exe"))
		nu = append(nu, filepath.Join(home, "scoop", "apps", "nu", "current", "nu.exe"))
	}
	if git, err := exec.LookPath("git.exe"); err == nil {
		bash = append(bash, filepath.Join(filepath.Dir(git), "..", "bin", "bash.exe"))
	}
	// Cygwin installs to a root of the user's choosing; the setup program's
	// defaults are the two roots beside the system drive. A bare bash.exe on
	// the PATH is deliberately not probed: it could be this Cygwin, Git Bash,
	// or the WSL launcher, and only the path tells them apart (issue #435).
	cygwin := []string{}
	if drive := os.Getenv("SystemDrive"); drive != "" {
		cygwin = append(cygwin,
			filepath.Join(drive+string(os.PathSeparator), "cygwin64", "bin", "bash.exe"),
			filepath.Join(drive+string(os.PathSeparator), "cygwin", "bin", "bash.exe"))
	}
	system := filepath.Join(os.Getenv("SystemRoot"), "System32")
	return discoverShells([]shellCandidate{
		{"PowerShell 7", pwsh},
		{"Windows PowerShell", []string{"powershell.exe", filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")}},
		{"CMD", []string{os.Getenv("COMSPEC"), "cmd.exe", filepath.Join(system, "cmd.exe")}},
		{"Git Bash", bash},
		{"Cygwin", cygwin},
		{"Nushell", nu},
	}, defaultShell(), exec.LookPath)
}
