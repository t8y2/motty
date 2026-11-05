package completion

import "sync"

var (
	commonOnce    sync.Once
	commonCatalog *Catalog
	commonErr     error
)

func Common() (*Catalog, error) {
	commonOnce.Do(func() {
		commonCatalog, commonErr = NewCatalog("motty-common", "v1.0.0", "issue-510", "Apache-2.0", []*Node{
			{Token: "awk", Desc: "Process text using patterns and actions", Flags: []Flag{
				{Short: "F", Value: true, Desc: "Set the input field separator"},
				{Short: "f", Value: true, Desc: "Read an AWK program from a file"},
			}},
			{Token: "cat", Desc: "Display file contents", Flags: []Flag{
				{Short: "b", Desc: "Number nonempty output lines"},
				{Short: "e", Desc: "Display nonprinting characters and line ends"},
				{Short: "n", Desc: "Number all output lines"},
				{Short: "s", Desc: "Squeeze adjacent blank lines"},
				{Short: "t", Desc: "Display nonprinting characters and tabs"},
				{Short: "u", Desc: "Disable output buffering"},
				{Short: "v", Desc: "Display nonprinting characters"},
			}},
			{Token: "ls", Desc: "List directory contents", Flags: []Flag{
				{Short: "a", Desc: "Include hidden entries"},
				{Short: "h", Desc: "Show sizes in human-readable units"},
				{Short: "l", Desc: "Use the long listing format"},
			}},
			{Token: "kill", Desc: "Send a signal to a process", Flags: []Flag{
				{Short: "l", Desc: "List available signals"},
				{Short: "s", Value: true, Desc: "Choose a signal by name or number"},
			}},
			{Token: "ip", Desc: "Manage network interfaces and routes", Children: []*Node{
				{Token: "addr", Desc: "Show or change interface addresses"},
				{Token: "link", Desc: "Show or change network interfaces"},
				{Token: "route", Desc: "Show or change routing tables"},
			}},
			{Token: "ss", Desc: "Inspect network sockets", Flags: []Flag{
				{Long: "all", Short: "a", Desc: "Show listening and non-listening sockets"},
				{Long: "listening", Short: "l", Desc: "Show listening sockets"},
				{Long: "numeric", Short: "n", Desc: "Do not resolve service names"},
			}},
			{Token: "netstat", Desc: "Inspect network connections", Flags: []Flag{
				{Long: "listening", Short: "l", Desc: "Show listening sockets"},
				{Long: "tcp", Short: "t", Desc: "Show TCP connections"},
				{Long: "udp", Short: "u", Desc: "Show UDP connections"},
			}},
			{Token: "yum", Desc: "Manage packages on RPM-based systems", Children: []*Node{
				{Token: "install", Desc: "Install packages"},
				{Token: "list", Desc: "List packages"},
				{Token: "remove", Desc: "Remove packages"},
				{Token: "update", Desc: "Update installed packages"},
			}},
			{Token: "gcc", Desc: "Compile C programs", Flags: []Flag{
				{Short: "c", Desc: "Compile without linking"},
				{Short: "o", Value: true, Desc: "Name the output file"},
			}},
			{Token: "pwsh", Desc: "Start PowerShell", Flags: []Flag{
				{Short: "NoProfile", Desc: "Start without loading profile scripts"},
				{Short: "Command", Value: true, Desc: "Run a command then exit"},
			}},
			{Token: "powershell", Desc: "Start Windows PowerShell", Flags: []Flag{
				{Short: "NoProfile", Desc: "Start without loading profile scripts"},
				{Short: "Command", Value: true, Desc: "Run a command then exit"},
			}},
			{Token: "cmd", Desc: "Start the Windows command interpreter", Children: []*Node{
				{Token: "/C", Desc: "Run the command then exit"},
				{Token: "/K", Desc: "Run the command and remain open"},
			}},
			{Token: "dir", Desc: "List items in a CMD directory", Children: []*Node{
				{Token: "/A", Desc: "Filter entries by attributes"},
				{Token: "/B", Desc: "Use a bare listing format"},
			}},
			{Token: "Get-Command", Desc: "Find commands available in PowerShell", Flags: []Flag{
				{Short: "Name", Value: true, Desc: "Find a command by name"},
				{Short: "CommandType", Value: true, Desc: "Filter command types"},
			}},
			{Token: "Get-ChildItem", Desc: "List items in a PowerShell location", Flags: []Flag{
				{Short: "Path", Value: true, Desc: "List the named path"},
				{Short: "Recurse", Desc: "Include child directories"},
			}},
		})
	})
	return commonCatalog, commonErr
}
