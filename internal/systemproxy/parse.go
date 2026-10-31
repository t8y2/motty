package systemproxy

import (
	"net"
	"strconv"
	"strings"
)

func windowsSettings(server, override string) Settings {
	var settings Settings
	for _, entry := range strings.Split(server, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		protocol, address, separate := strings.Cut(entry, "=")
		if !separate {
			settings.HTTP = proxyURL(entry, "http")
			settings.HTTPS = settings.HTTP
			settings.All = settings.HTTP
			break
		}
		switch strings.ToLower(strings.TrimSpace(protocol)) {
		case "http":
			settings.HTTP = proxyURL(address, "http")
		case "https":
			settings.HTTPS = proxyURL(address, "http")
		case "socks":
			settings.All = proxyURL(address, "socks5")
		}
	}
	settings.Bypass = bypassList(strings.Split(override, ";"))
	return settings
}

func darwinSettings(output string) Settings {
	values := map[string]string{}
	var exceptions []string
	inExceptions := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if inExceptions {
			if strings.HasPrefix(line, "}") {
				inExceptions = false
				continue
			}
			if _, value, ok := strings.Cut(line, " : "); ok {
				exceptions = append(exceptions, value)
			}
			continue
		}
		key, value, ok := strings.Cut(line, " : ")
		if !ok {
			continue
		}
		if key == "ExceptionsList" {
			inExceptions = true
		}
		values[key] = value
	}
	settings := Settings{
		HTTP:   darwinProxy(values, "HTTP", "http"),
		HTTPS:  darwinProxy(values, "HTTPS", "http"),
		All:    darwinProxy(values, "SOCKS", "socks5"),
		Bypass: bypassList(exceptions),
	}
	return settings
}

func darwinProxy(values map[string]string, protocol, scheme string) string {
	if values[protocol+"Enable"] != "1" {
		return ""
	}
	return hostPortURL(values[protocol+"Proxy"], values[protocol+"Port"], scheme)
}

func hostPortURL(host, portText, scheme string) string {
	port, err := strconv.Atoi(portText)
	if host == "" || err != nil || port < 1 || port > 65535 {
		return ""
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func gnomeList(value string) []string {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil
	}
	value = strings.TrimSpace(value[1 : len(value)-1])
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	for index := range parts {
		parts[index] = strings.Trim(strings.TrimSpace(parts[index]), "'")
	}
	return parts
}
