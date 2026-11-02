package sessionimport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"
	"gopkg.in/yaml.v3"
)

// Tabby keeps its sessions in a YAML file (config.yaml) and its secrets in a
// "vault": one AES-256-CBC blob whose key is derived from a passphrase the user
// types, PBKDF2-SHA512 over a salt the file carries. The whole configuration can
// be moved inside that blob as well, leaving the file with nothing but the vault
// in it. A saved password is a vault secret of type "ssh:password" named by its
// user, host and port; a key passphrase is "ssh:key-passphrase" named by the
// SHA-512 of the key file; a private key the user chose to keep in the vault is
// a "file" secret holding its base64 bytes.
//
// Nothing here is guessed: a session is read only when the file says what it is,
// and a secret is handed back only when the user typed the passphrase that
// opens the vault.

// Errors a caller acts on; test with errors.Is.
var (
	// ErrTabbyVault means the passphrase did not open the vault.
	ErrTabbyVault = errors.New("tabby: the vault passphrase is wrong")
	// ErrTabbyNeedsPassphrase means the file keeps its sessions or its secrets in
	// a vault that has not been opened yet.
	ErrTabbyNeedsPassphrase = errors.New("tabby: the vault has not been opened")
)

// TabbyFile is one Tabby config.yaml: the sessions it holds, and the vault that
// holds what was saved beside them.
type TabbyFile struct {
	path string
	// config is the file as written, or, for a file whose configuration is
	// encrypted, what the vault held once it was opened.
	config tabbyConfig
	// encrypted is set when the sessions live inside the vault, so nothing can be
	// read before it is opened.
	encrypted bool
	unlocked  bool
	result    Result
	// index is where each session's own id landed in result.Entries, for the
	// routes that name one by id.
	index map[string]int
}

// NeedsPassphrase reports whether the file keeps its sessions or its secrets in
// a vault that has not been opened yet.
func (tabby *TabbyFile) NeedsPassphrase() bool {
	return !tabby.unlocked && (tabby.encrypted || tabby.config.Vault != nil)
}

// Encrypted reports whether the sessions themselves are inside the vault, so
// that nothing at all can be read without the passphrase.
func (tabby *TabbyFile) Encrypted() bool {
	return tabby.encrypted
}

// Path is the file this configuration was read from.
func (tabby *TabbyFile) Path() string {
	return tabby.path
}

// Result is what the file held: the sessions, and the secrets of a vault that
// has been opened.
func (tabby *TabbyFile) Result() Result {
	return tabby.result
}

// ParseTabby reads a Tabby config.yaml. A file whose sessions are not encrypted
// gives its sessions at once and its vault, if any, stays closed; Unlock opens
// it and adds what it holds.
func ParseTabby(path string) (*TabbyFile, error) {
	text, err := readFile(path)
	if err != nil {
		return nil, err
	}
	var config tabbyConfig
	if err := yaml.Unmarshal([]byte(text), &config); err != nil {
		return nil, fmt.Errorf("not a Tabby config: %v", err)
	}
	if len(config.Profiles) == 0 && config.Vault == nil && !config.Encrypted {
		return nil, errors.New("not a Tabby config: it has no sessions and no vault")
	}
	tabby := &TabbyFile{path: path, config: config, encrypted: config.Encrypted}
	if tabby.encrypted {
		// Nothing but the vault is in the file; the sessions come out of it.
		return tabby, nil
	}
	tabby.build()
	return tabby, nil
}

// Unlock opens the vault with the passphrase the user typed, and reads what it
// holds: the sessions themselves for an encrypted configuration, then the saved
// passwords, key passphrases and keys. A wrong passphrase leaves the result as
// it was.
func (tabby *TabbyFile) Unlock(passphrase string) error {
	if tabby.unlocked {
		return nil
	}
	if tabby.config.Vault == nil {
		tabby.unlocked = true
		return nil
	}
	plain, err := decryptTabbyVault(tabby.config.Vault, passphrase)
	if err != nil {
		return err
	}
	var contents tabbyVaultContents
	if err := json.Unmarshal(plain, &contents); err != nil {
		return fmt.Errorf("%w: what it opened is not a configuration", ErrTabbyVault)
	}
	if tabby.encrypted {
		if contents.Config == nil {
			return fmt.Errorf("%w: it does not hold a configuration", ErrTabbyVault)
		}
		tabby.config = *contents.Config
		tabby.build()
	}
	tabby.unlocked = true
	tabby.applySecrets(contents.Secrets)
	return nil
}

// ---- the file ----

type tabbyConfig struct {
	Version   int            `yaml:"version"`
	Profiles  []tabbyProfile `yaml:"profiles"`
	Groups    []tabbyGroup   `yaml:"groups"`
	Vault     *tabbyVault    `yaml:"vault"`
	Encrypted bool           `yaml:"encrypted"`
}

type tabbyProfile struct {
	ID      string       `yaml:"id"`
	Type    string       `yaml:"type"`
	Name    string       `yaml:"name"`
	Group   string       `yaml:"group"`
	Color   string       `yaml:"color"`
	Options tabbyOptions `yaml:"options"`
}

// tabbyOptions is one session's settings. The same YAML key means different
// things by protocol: "port" is a number for SSH and Telnet and a device name
// for serial, which is why it is read as both (see tabbyPort).
type tabbyOptions struct {
	Host           string        `yaml:"host"`
	Port           tabbyPort     `yaml:"port"`
	User           string        `yaml:"user"`
	Auth           string        `yaml:"auth"`
	Password       string        `yaml:"password"`
	PrivateKeys    []string      `yaml:"privateKeys"`
	JumpHost       string        `yaml:"jumpHost"`
	ProxyCommand   string        `yaml:"proxyCommand"`
	SocksProxyHost string        `yaml:"socksProxyHost"`
	SocksProxyPort int           `yaml:"socksProxyPort"`
	HTTPProxyHost  string        `yaml:"httpProxyHost"`
	HTTPProxyPort  int           `yaml:"httpProxyPort"`
	X11            bool          `yaml:"x11"`
	AgentForward   bool          `yaml:"agentForward"`
	ForwardedPorts []tabbyTunnel `yaml:"forwardedPorts"`
	// Serial.
	BaudRate int    `yaml:"baudrate"`
	DataBits int    `yaml:"databits"`
	StopBits int    `yaml:"stopbits"`
	Parity   string `yaml:"parity"`
	RTSCTS   bool   `yaml:"rtscts"`
	XON      bool   `yaml:"xon"`
	XOFF     bool   `yaml:"xoff"`
}

// tabbyPort is Tabby's "port" setting: a number for a network session and a
// device path for a serial one.
type tabbyPort struct {
	Number int
	Device string
}

func (port *tabbyPort) UnmarshalYAML(node *yaml.Node) error {
	var number int
	if err := node.Decode(&number); err == nil {
		port.Number = number
		return nil
	}
	var device string
	if err := node.Decode(&device); err == nil {
		port.Device = device
		return nil
	}
	return nil // neither a port nor a device: the session is read without one
}

// UnmarshalJSON is the same reading for the configuration a vault holds, which
// is JSON rather than YAML.
func (port *tabbyPort) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		port.Number = number
		return nil
	}
	var device string
	if err := json.Unmarshal(data, &device); err == nil {
		port.Device = device
	}
	return nil
}

type tabbyGroup struct {
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	ParentGroupID string `yaml:"parentGroupId"`
}

type tabbyTunnel struct {
	Type          string `yaml:"type"` // Local, Remote, Dynamic
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	TargetAddress string `yaml:"targetAddress"`
	TargetPort    int    `yaml:"targetPort"`
}

// build turns the profiles of the file into sessions, in file order.
func (tabby *TabbyFile) build() {
	source := filepath.Base(tabby.path)
	tabby.result = Result{}
	tabby.index = make(map[string]int, len(tabby.config.Profiles))
	tree := newTabbyTree(tabby.config.Groups)
	shortened := false
	var plainPasswords int
	for _, profile := range tabby.config.Profiles {
		kind := SSH
		switch strings.ToLower(strings.TrimSpace(profile.Type)) {
		case "", "ssh":
		case "telnet":
			kind = Telnet
		case "serial":
			kind = Serial
		default:
			// A local shell is not a host, and a plugin's own profile type cannot
			// be opened by Motty.
			tabby.result.Skipped++
			continue
		}
		if len(tabby.result.Entries) >= MaxEntries {
			tabby.result.warn("%s: more than %d sessions; the rest were not read", source, MaxEntries)
			break
		}
		group, cut := tree.path(profile.Group)
		shortened = shortened || cut
		entry := tabby.entry(profile, kind, group, source)
		before := len(tabby.result.Entries)
		tabby.result.add(entry)
		if len(tabby.result.Entries) == before {
			continue
		}
		if profile.ID != "" {
			tabby.index[profile.ID] = len(tabby.result.Entries) - 1
		}
		if entry.Password != "" {
			plainPasswords++
		}
		if entry.KeyPath != "" || entry.KeyID != "" {
			tabby.result.Keys++
		}
	}
	tabby.buildRoutes(source)
	tabby.buildForwards(source)
	if shortened {
		tabby.result.warn("%s: folders nested more than %d levels deep were shortened", source, maxGroupDepth)
	}
	if plainPasswords > 0 {
		tabby.result.warn("%s: %d sessions kept a password in clear text; they were read into Motty's vault", source, plainPasswords)
	}
	if tabby.config.Vault == nil && !tabby.encrypted {
		// Tabby before its vault kept passwords in the system's secret store, which
		// is not this program's to read.
		tabby.result.warn("%s: no vault in the file; passwords Tabby kept in the system keychain were not read", source)
	}
}

// entry is one profile as a session.
func (tabby *TabbyFile) entry(profile tabbyProfile, kind Protocol, group, source string) Entry {
	entry := Entry{
		Protocol: kind, Name: profile.Name, Group: group, User: profile.Options.User,
		Source: source, Color: profile.Color,
	}
	switch kind {
	case Serial:
		entry.Host = profile.Options.Port.Device
		entry.Line, entry.Flow = tabbySerial(profile.Options)
		return entry
	case Telnet:
		entry.Host, entry.Port = profile.Options.Host, profile.Options.Port.Number
		return entry
	}
	entry.Host, entry.Port = profile.Options.Host, profile.Options.Port.Number
	entry.X11, entry.AgentForward = profile.Options.X11, profile.Options.AgentForward
	entry.Auth = tabbyAuth(profile.Options.Auth)
	entry.Password = profile.Options.Password
	for _, key := range profile.Options.PrivateKeys {
		if id, ok := strings.CutPrefix(key, "vault://"); ok {
			// The key itself is a vault secret, read when the vault is opened.
			if entry.KeyID == "" {
				entry.KeyID = id
			}
			continue
		}
		if entry.KeyPath == "" && key != "" {
			entry.KeyPath = key
		}
	}
	return entry
}

// tabbyAuth reads the authentication a session asked for. An empty value is
// Tabby's "ask the server", which is what Motty's automatic mode does.
func tabbyAuth(auth string) Auth {
	switch strings.ToLower(strings.TrimSpace(auth)) {
	case "password", "keyboardinteractive":
		return AuthPassword
	case "publickey":
		return AuthPrivateKey
	case "agent":
		return AuthAgent
	}
	return AuthUnset
}

// tabbySerial writes a serial session's line as "9600 8N1" and its flow control,
// with Tabby's own defaults where the file leaves a setting out.
func tabbySerial(options tabbyOptions) (line, flow string) {
	baud := options.BaudRate
	if baud == 0 {
		baud = 9600
	}
	bits := options.DataBits
	if bits == 0 {
		bits = 8
	}
	stop := options.StopBits
	if stop == 0 {
		stop = 1
	}
	parity := 'N'
	switch strings.ToLower(strings.TrimSpace(options.Parity)) {
	case "odd":
		parity = 'O'
	case "even":
		parity = 'E'
	case "mark":
		parity = 'M'
	case "space":
		parity = 'S'
	}
	switch {
	case options.RTSCTS:
		flow = "rtscts"
	case options.XON || options.XOFF:
		flow = "xonxoff"
	}
	return fmt.Sprintf("%d %d%c%d", baud, bits, parity, stop), flow
}

// buildRoutes turns the jump host each session names into a route. Tabby names
// it by the profile's id, so a session whose jump host was not imported (a
// local one, or one that could not be read) gets none, and is told so.
func (tabby *TabbyFile) buildRoutes(source string) {
	for _, profile := range tabby.config.Profiles {
		jump := profile.Options.JumpHost
		if jump == "" {
			continue
		}
		target, ok := tabby.index[profile.ID]
		if !ok {
			continue
		}
		via, ok := tabby.index[jump]
		if !ok {
			tabby.result.warn("%s: %s names a jump host that was not imported", source, clean(profile.Name, maxNameLength))
			continue
		}
		if via == target {
			continue
		}
		tabby.result.Jumps = append(tabby.result.Jumps, Jump{Target: target, Via: via})
	}
}

// buildForwards reads the port forwards each session saved. Motty listens on
// this computer's loopback only, so a tunnel Tabby opened on another address is
// brought in on loopback and the difference is told.
func (tabby *TabbyFile) buildForwards(source string) {
	loopbackWarned := false
	for _, profile := range tabby.config.Profiles {
		at, ok := tabby.index[profile.ID]
		if !ok {
			continue
		}
		for _, tunnel := range profile.Options.ForwardedPorts {
			var mode ForwardMode
			switch strings.ToLower(strings.TrimSpace(tunnel.Type)) {
			case "local":
				mode = ForwardLocal
			case "remote":
				mode = ForwardRemote
			case "dynamic":
				mode = ForwardSOCKS
			default:
				continue
			}
			if tunnel.Port <= 0 || tunnel.Port > 65535 {
				continue
			}
			if mode != ForwardRemote && tunnel.Host != "" && !IsLoopbackHost(tunnel.Host) {
				if !loopbackWarned {
					tabby.result.warn("%s: a tunnel listened on %s; Motty listens on loopback only", source, clean(tunnel.Host, 64))
					loopbackWarned = true
				}
			}
			forward := Forward{Entry: at, Mode: mode, LocalPort: tunnel.Port}
			if mode != ForwardSOCKS {
				if tunnel.TargetAddress == "" || tunnel.TargetPort <= 0 || tunnel.TargetPort > 65535 {
					continue
				}
				forward.DestinationHost = tunnel.TargetAddress
				forward.DestinationPort = tunnel.TargetPort
			}
			tabby.result.Forwards = append(tabby.result.Forwards, forward)
		}
	}
}

// tabbyTree answers where a group sits: Tabby keeps its groups in one list and
// each names the group above it.
type tabbyTree struct {
	groups map[string]tabbyGroup
}

func newTabbyTree(groups []tabbyGroup) tabbyTree {
	tree := tabbyTree{groups: make(map[string]tabbyGroup, len(groups))}
	for _, group := range groups {
		if _, seen := tree.groups[group.ID]; !seen && group.ID != "" {
			tree.groups[group.ID] = group
		}
	}
	return tree
}

// path is the folders a group sits in, joined with "/" the way the other
// imports join them, and whether it had to be cut at maxGroupDepth levels (the
// innermost ones are kept). A group that is its own ancestor stops the climb
// where it would come round to itself.
func (tree tabbyTree) path(id string) (string, bool) {
	var names []string
	seen := make(map[string]bool, maxGroupDepth)
	cut := false
	for current := id; current != ""; {
		group, known := tree.groups[current]
		if !known || seen[current] {
			break
		}
		if len(names) == maxGroupDepth {
			cut = true
			break
		}
		seen[current] = true
		if name := clean(group.Name, maxNameLength); name != "" {
			names = append(names, name)
		}
		current = group.ParentGroupID
	}
	// names holds the innermost folder first; the path reads from the outside in.
	for left, right := 0, len(names)-1; left < right; left, right = left+1, right-1 {
		names[left], names[right] = names[right], names[left]
	}
	return strings.Join(names, "/"), cut
}

// ---- the vault ----

// tabbyVault is the encrypted blob Tabby keeps in its config file.
type tabbyVault struct {
	Version  int    `yaml:"version"`
	Contents string `yaml:"contents"` // base64 of the AES-256-CBC ciphertext
	KeySalt  string `yaml:"keySalt"`  // hex
	IV       string `yaml:"iv"`       // hex
}

// The passphrase is stretched with PBKDF2-SHA512 exactly as Tabby does it, so a
// vault this program cannot open is one Tabby could not open either.
const (
	tabbyPBKDFIterations = 100000
	tabbyKeySize         = 32
	tabbySaltSize        = 8
	tabbyIVSize          = 16
)

// tabbyVaultContents is what the blob holds: the configuration itself when the
// user asked Tabby to encrypt it, and the secrets.
type tabbyVaultContents struct {
	Config  *tabbyConfig  `json:"config"`
	Secrets []tabbySecret `json:"secrets"`
}

type tabbySecret struct {
	Type  string         `json:"type"`
	Key   tabbySecretKey `json:"key"`
	Value string         `json:"value"`
}

// tabbySecretKey is what names a secret: a connection for a password, the hash
// of a key file for a passphrase, and an id for a file kept in the vault.
type tabbySecretKey struct {
	User string `json:"user"`
	Host string `json:"host"`
	Port int    `json:"port"`
	Hash string `json:"hash"`
	ID   string `json:"id"`
}

const (
	tabbySecretPassword   = "ssh:password"
	tabbySecretPassphrase = "ssh:key-passphrase"
	tabbySecretFile       = "file"
)

func decryptTabbyVault(vault *tabbyVault, passphrase string) ([]byte, error) {
	if vault.Version != 1 {
		return nil, fmt.Errorf("tabby: vault version %d is not read", vault.Version)
	}
	salt, err := hex.DecodeString(strings.TrimSpace(vault.KeySalt))
	if err != nil || len(salt) != tabbySaltSize {
		return nil, errors.New("tabby: the vault's salt is not readable")
	}
	iv, err := hex.DecodeString(strings.TrimSpace(vault.IV))
	if err != nil || len(iv) != tabbyIVSize {
		return nil, errors.New("tabby: the vault's IV is not readable")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(vault.Contents))
	if err != nil || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("tabby: the vault's contents are not readable")
	}
	key := pbkdf2.Key([]byte(passphrase), salt, tabbyPBKDFIterations, tabbyKeySize, sha512.New)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	// A wrong passphrase leaves the padding almost certainly wrong, which is the
	// only check the format allows: there is no tag to verify.
	padding := int(plain[len(plain)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(plain) {
		return nil, ErrTabbyVault
	}
	for _, last := range plain[len(plain)-padding:] {
		if int(last) != padding {
			return nil, ErrTabbyVault
		}
	}
	return plain[:len(plain)-padding], nil
}

// applySecrets hands the secrets of an open vault to the sessions they belong
// to. Files come first: a passphrase is named by the hash of the key it opens,
// so the key has to be known before the passphrase can be matched to it.
func (tabby *TabbyFile) applySecrets(secrets []tabbySecret) {
	source := filepath.Base(tabby.path)
	byPassword := make(map[string]int, len(tabby.result.Entries))
	for index, entry := range tabby.result.Entries {
		if entry.Protocol != SSH || entry.Password != "" {
			continue
		}
		byPassword[tabbyPasswordKey(entry.User, entry.Host, entry.Port)] = index
	}
	files := make(map[string][]byte, len(secrets))
	for _, secret := range secrets {
		if secret.Type == tabbySecretFile && secret.Key.ID != "" {
			if data, err := base64.StdEncoding.DecodeString(secret.Value); err == nil {
				files[secret.Key.ID] = data
			}
		}
	}
	for index, entry := range tabby.result.Entries {
		if entry.KeyID == "" {
			continue
		}
		data, ok := files[entry.KeyID]
		if !ok {
			tabby.result.warn("%s: a key kept in Tabby's vault was not found in it", source)
			continue
		}
		tabby.result.Entries[index].KeyData = data
	}
	passphrases := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		if secret.Type == tabbySecretPassphrase && secret.Key.Hash != "" {
			passphrases[secret.Key.Hash] = secret.Value
		}
	}
	for _, secret := range secrets {
		if secret.Type != tabbySecretPassword || secret.Value == "" {
			continue
		}
		if index, ok := tabby.passwordIndex(byPassword, secret.Key); ok {
			tabby.result.Entries[index].Password = secret.Value
		}
	}
	for index := range tabby.result.Entries {
		entry := &tabby.result.Entries[index]
		if entry.KeyPath == "" && len(entry.KeyData) == 0 {
			continue
		}
		hash := tabbyKeyHash(entry)
		if hash == "" {
			continue
		}
		if passphrase, ok := passphrases[hash]; ok && passphrase != "" {
			entry.KeyPassphrase = passphrase
		}
	}
}

// passwordIndex finds the session a saved password belongs to: the same user,
// host and port, or, for a password Tabby saved with no host at all, a session
// on that host with the same user.
func (tabby *TabbyFile) passwordIndex(byPassword map[string]int, key tabbySecretKey) (int, bool) {
	port := key.Port
	if port == 0 {
		port = 22
	}
	if index, ok := byPassword[tabbyPasswordKey(key.User, key.Host, port)]; ok {
		return index, true
	}
	if key.Host != "" {
		return 0, false
	}
	for index, entry := range tabby.result.Entries {
		if entry.Protocol == SSH && entry.User == key.User && entry.Password == "" {
			return index, true
		}
	}
	return 0, false
}

// tabbyPasswordKey is how a password names the connection it belongs to.
func tabbyPasswordKey(user, host string, port int) string {
	return user + "\x00" + strings.ToLower(host) + "\x00" + strconv.Itoa(port)
}

// tabbyKeyHash is Tabby's name for the passphrase of a key: the SHA-512 of the
// key's own bytes, which is why the key has to be read to find it.
func tabbyKeyHash(entry *Entry) string {
	data := entry.KeyData
	if len(data) == 0 && entry.KeyPath != "" {
		read, err := os.ReadFile(entry.KeyPath)
		if err != nil {
			return ""
		}
		data = read
	}
	if len(data) == 0 {
		return ""
	}
	sum := sha512.Sum512(data)
	return hex.EncodeToString(sum[:])
}
