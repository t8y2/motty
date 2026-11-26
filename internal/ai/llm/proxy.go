package llm

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ParseProxyURL splits an outbound proxy address into the address a provider
// may keep and the password that belongs in the vault. The returned address
// never carries a password. Empty raw means "no proxy" and is valid.
func ParseProxyURL(raw string) (address, password string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", "", false
	}
	if parsed.User != nil {
		user := parsed.User.Username()
		if user == "" {
			return "", "", false
		}
		password, _ = parsed.User.Password()
		parsed.User = url.User(user)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Path = ""
	return parsed.String(), password, true
}

// proxyEndpoint is the URL the HTTP transport dials. A password kept outside
// the address is put back here, just before the request is made.
func proxyEndpoint(raw, password string) (*url.URL, error) {
	address, inURL, ok := ParseProxyURL(raw)
	if !ok {
		return nil, errors.New("invalid proxy address")
	}
	if password == "" {
		password = inURL
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if password != "" {
		if parsed.User == nil {
			return nil, errors.New("a proxy password needs a user name")
		}
		parsed.User = url.UserPassword(parsed.User.Username(), password)
	}
	return parsed, nil
}

// proxyFor returns the transport's proxy policy. No address keeps the
// environment/system behavior; an unusable address fails the request instead
// of silently connecting directly.
func proxyFor(raw, password string) func(*http.Request) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return http.ProxyFromEnvironment
	}
	parsed, err := proxyEndpoint(raw, password)
	if err != nil {
		return func(*http.Request) (*url.URL, error) {
			return nil, fmt.Errorf("llm: proxy: %w", err)
		}
	}
	return http.ProxyURL(parsed)
}

// NewHTTPClient returns the client requests to a provider use. cfg.Client wins;
// otherwise the client follows cfg.ProxyURL.
func NewHTTPClient(cfg Config) *http.Client {
	if cfg.Client != nil {
		return cfg.Client
	}
	return defaultClient(cfg)
}
