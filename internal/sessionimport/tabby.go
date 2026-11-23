package sessionimport

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type tabbyOptions struct {
	Host           string   `yaml:"host"`
	Port           *int     `yaml:"port"`
	User           *string  `yaml:"user"`
	Auth           string   `yaml:"auth"`
	Password       string   `yaml:"password"`
	PrivateKeys    []string `yaml:"privateKeys"`
	JumpHost       string   `yaml:"jumpHost"`
	ProxyCommand   string   `yaml:"proxyCommand"`
	SOCKSProxyHost string   `yaml:"socksProxyHost"`
	HTTPProxyHost  string   `yaml:"httpProxyHost"`
}

type tabbyDefaults struct {
	SSH struct {
		Options map[string]yaml.Node
	}
}

type tabbyGroup struct {
	ID       string
	Name     string
	ParentID string `yaml:"parentGroupId"`
	Defaults tabbyDefaults
}

type tabbyConfig struct {
	Encrypted       bool
	Profiles        []yaml.Node
	Groups          []tabbyGroup
	ProfileDefaults tabbyDefaults `yaml:"profileDefaults"`
}

// ParseTabby reads saved SSH profiles from Tabby's config.yaml. Profile
// defaults follow Tabby's precedence: global, immediate group, then profile.
// The import retains local key references and never reads the named files.
func ParseTabby(path string) (Result, error) {
	source, err := OpenTabbyConfig(path)
	if err != nil {
		return Result{}, err
	}
	result, err := source.ReadProfiles()
	if err == nil && source.HasVault() {
		result.warn("%s: select this file on its own to unlock its Tabby Vault", source.name)
	}
	return result, err
}

// TabbyConfigImport holds the profile settings and any encrypted Vault from
// one selected file. The same source is used whether the Vault is unlocked.
type TabbyConfigImport struct {
	name   string
	config tabbyConfig
	vault  yaml.Node
}

// OpenTabbyConfig reads a Tabby configuration without decrypting its Vault.
func OpenTabbyConfig(path string) (*TabbyConfigImport, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		tabbyConfig `yaml:",inline"`
		Vault       yaml.Node
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		// YAML errors can quote scalar values, including credentials.
		return nil, errors.New("invalid Tabby YAML configuration")
	}
	return &TabbyConfigImport{name: filepath.Base(path), config: file.tabbyConfig, vault: file.Vault}, nil
}

func (source *TabbyConfigImport) HasVault() bool {
	return source.vault.Kind != 0 && source.vault.Tag != "!!null"
}

func (source *TabbyConfigImport) RequiresUnlock() bool {
	return source.config.Encrypted
}

// ReadProfiles imports the settings that are accessible without the Vault.
func (source *TabbyConfigImport) ReadProfiles() (Result, error) {
	if source.RequiresUnlock() {
		return Result{}, errors.New("the Tabby configuration is encrypted; select this file on its own to unlock it")
	}
	return parseTabbyConfig(source.name, source.config, nil)
}

func parseTabbyConfig(source string, config tabbyConfig, passwords tabbyVaultPasswords) (Result, error) {
	if config.Profiles == nil {
		return Result{}, errors.New("not a Tabby configuration: no profiles list")
	}
	groups := make(map[string]tabbyGroup, len(config.Groups))
	for _, group := range config.Groups {
		groups[group.ID] = group
	}
	var result Result
	for index, node := range config.Profiles {
		var profile struct {
			Type    string
			Name    string
			Group   string
			Options map[string]yaml.Node
		}
		if err := node.Decode(&profile); err != nil {
			result.Skipped++
			result.warn("%s: invalid Tabby profile at position %d", source, index+1)
			continue
		}
		if profile.Type != "ssh" {
			result.Skipped++
			continue
		}
		group := groups[profile.Group]
		options, err := decodeTabbyOptions(config.ProfileDefaults.SSH.Options, group.Defaults.SSH.Options, profile.Options)
		if err != nil {
			result.Skipped++
			result.warn("%s: invalid SSH settings at position %d", source, index+1)
			continue
		}
		entry, ok := tabbySSHEntry(&result, source, index+1, profile.Name, tabbyGroupPath(groups, profile.Group), options)
		if ok {
			switch options.Auth {
			case "", "password", "keyboardInteractive":
				if entry.Password == "" {
					entry.Password = passwords.lookup(entry.Host, entry.User, options.Port)
				}
			}
			result.add(entry)
		}
	}
	return result, nil
}

// Merge before decoding so an explicit null clears an inherited option.
func decodeTabbyOptions(layers ...map[string]yaml.Node) (tabbyOptions, error) {
	fields := make(map[string]yaml.Node)
	for _, layer := range layers {
		for key, value := range layer {
			fields[key] = value
		}
	}
	merged := yaml.Node{Kind: yaml.MappingNode}
	for key, value := range fields {
		merged.Content = append(merged.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &value)
	}
	// An omitted username uses Tabby's built-in default. An explicit
	// null or empty username asks Tabby to prompt at connection time.
	defaultUser := "root"
	defaultPort := 22
	options := tabbyOptions{User: &defaultUser, Port: &defaultPort}
	err := merged.Decode(&options)
	if fields["password"].Tag != "!!str" {
		options.Password = ""
	}
	return options, err
}

func tabbyGroupPath(groups map[string]tabbyGroup, id string) string {
	var names []string
	visited := make(map[string]bool)
	for id != "" && len(visited) < maxGroupDepth && !visited[id] {
		visited[id] = true
		group, ok := groups[id]
		if !ok {
			names = append(names, id)
			break
		}
		if group.Name != "" {
			names = append(names, group.Name)
		}
		id = group.ParentID
	}
	slices.Reverse(names)
	return strings.Join(names, "/")
}

func tabbySSHEntry(result *Result, source string, index int, name, group string, options tabbyOptions) (Entry, bool) {
	port := 22
	if options.Port != nil {
		port = *options.Port
	}
	user := ""
	if options.User != nil {
		user = *options.User
	}
	if port < 1 || port > 65535 || options.Host == "" || options.Host != clean(options.Host, 255) ||
		strings.ContainsAny(options.Host, " \t/\\") || user == "" ||
		strings.HasPrefix(user, "$") || user != clean(user, 128) {
		result.Skipped++
		result.warn("%s: unusable SSH address, port or username at position %d", source, index)
		return Entry{}, false
	}
	for _, route := range []string{options.JumpHost, options.ProxyCommand, options.SOCKSProxyHost, options.HTTPProxyHost} {
		if route != "" {
			// Importing as a direct connection would change where it connects.
			result.Skipped++
			result.warn("%s: SSH profile at position %d uses a jump host or proxy", source, index)
			return Entry{}, false
		}
	}
	entry := Entry{Name: name, Group: group, Host: options.Host, Port: port, User: user, Source: source}
	entry.SSHSettings = &SSHSettings{Auth: options.Auth}
	if options.Password != "" {
		result.PlainSecrets++
		switch entry.SSHSettings.Auth {
		case "", "password", "keyboardInteractive":
			entry.Password = options.Password
		}
	}
	var skippedKey bool
	for _, key := range options.PrivateKeys {
		path, ok := tabbyLocalKeyPath(key)
		if !ok {
			skippedKey = true
			continue
		}
		if !slices.Contains(entry.SSHSettings.IdentityFiles, path) {
			entry.SSHSettings.IdentityFiles = append(entry.SSHSettings.IdentityFiles, path)
		}
	}
	if skippedKey {
		result.Keys++
	}
	return entry, true
}

func tabbyLocalKeyPath(path string) (string, bool) {
	// Tabby's file provider prefixes the literal native path, without URL
	// escaping it. Older entries may already contain a bare path.
	path = strings.TrimPrefix(path, "file://")
	if relative, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, relative)
	}
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") ||
		strings.HasPrefix(path, "//") || strings.HasPrefix(path, `\\`) {
		return "", false
	}
	return filepath.Clean(path), true
}
