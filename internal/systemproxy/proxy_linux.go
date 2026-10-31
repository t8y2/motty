//go:build linux

package systemproxy

import (
	"os/exec"
	"strings"
)

func Lookup() Settings {
	if gnomeValue("org.gnome.system.proxy", "mode") != "manual" {
		return Settings{}
	}
	return Settings{
		HTTP:   gnomeProxy("http", "http"),
		HTTPS:  gnomeProxy("https", "http"),
		All:    gnomeProxy("socks", "socks5"),
		Bypass: bypassList(gnomeList(gnomeValue("org.gnome.system.proxy", "ignore-hosts"))),
	}
}

func gnomeProxy(protocol, scheme string) string {
	schema := "org.gnome.system.proxy." + protocol
	return hostPortURL(gnomeValue(schema, "host"), gnomeValue(schema, "port"), scheme)
}

func gnomeValue(schema, key string) string {
	output, err := exec.Command("gsettings", "get", schema, key).Output()
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(string(output)), "'")
}
