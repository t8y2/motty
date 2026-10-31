package systemproxy

import "strings"

type Settings struct {
	HTTP   string
	HTTPS  string
	All    string
	Bypass string
}

func Environment(settings Settings) []string {
	values := []struct {
		name  string
		value string
	}{
		{"HTTP_PROXY", settings.HTTP},
		{"HTTPS_PROXY", settings.HTTPS},
		{"ALL_PROXY", settings.All},
		{"NO_PROXY", settings.Bypass},
	}
	environment := make([]string, 0, 2*len(values))
	for _, value := range values {
		environment = append(environment, value.name+"="+value.value, strings.ToLower(value.name)+"="+value.value)
	}
	return environment
}

func proxyURL(address, scheme string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.Contains(address, "://") {
		return address
	}
	return scheme + "://" + address
}

func bypassList(entries []string) string {
	seen := map[string]bool{}
	var list []string
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, "<local>") {
			for _, local := range []string{"localhost", "127.0.0.1", "::1"} {
				if !seen[local] {
					seen[local] = true
					list = append(list, local)
				}
			}
			continue
		}
		if strings.HasPrefix(entry, "*.") {
			entry = entry[1:]
		}
		if !seen[entry] {
			seen[entry] = true
			list = append(list, entry)
		}
	}
	return strings.Join(list, ",")
}
