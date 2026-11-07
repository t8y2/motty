// Package gist keeps the encrypted backup in one file of a secret GitHub Gist
// or Gitee snippet, through their REST APIs: test the token, upload, and
// download. A snippet is text, so the sealed bytes are stored base64-encoded.
// It never lists, deletes or touches any other file of the snippet.
package gist

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"motty/internal/cloudsync"
)

// Provider is the service that holds the snippet.
type Provider uint8

const (
	// GitHub is api.github.com and GitHub Enterprise.
	GitHub Provider = iota
	// Gitee is gitee.com.
	Gitee
)

// String names the provider in the settings state and messages.
func (provider Provider) String() string {
	if provider == Gitee {
		return "Gitee"
	}
	return "GitHub"
}

// Config says where the backup is and who may touch it.
type Config struct {
	// Provider is the service; the zero value is GitHub.
	Provider Provider
	// Token is a personal access token that may read and write snippets (a
	// fine-grained token with the Gists permission, or a classic one with the
	// gist scope), and nothing else.
	Token string
	// ID is the snippet that holds the backup; empty means none exists yet and
	// the first Put creates a secret one (see Client.ID).
	ID string
	// File is the file in the snippet.
	File string
	// APIBase overrides the provider's default, for GitHub Enterprise and tests.
	// https:// only, except for a loopback address.
	APIBase string
}

// DefaultFile is the file name used when the user gives none.
const DefaultFile = "motty-backup.mottybak.b64"

// DefaultAPIBase is where GitHub's gists live.
const DefaultAPIBase = "https://api.github.com"

// GiteeAPIBase is where Gitee's snippets live.
const GiteeAPIBase = "https://gitee.com/api/v5"

// DefaultAPIBaseFor is the provider's public API address.
func DefaultAPIBaseFor(provider Provider) string {
	if provider == Gitee {
		return GiteeAPIBase
	}
	return DefaultAPIBase
}

// MaxFileBytes is what the API takes per file in one request (1 MB of text); a
// bigger file is truncated when read back, so it is refused before it is sent.
const MaxFileBytes = 1_000_000

// Client talks to one snippet.
type Client struct {
	config Config
	base   *url.URL
	http   *http.Client
}

var _ cloudsync.Store = (*Client)(nil)

// New checks the configuration. A nil http client does not follow redirects to
// another host. The caller controls the request deadline through its context.
func New(config Config, client *http.Client) (*Client, error) {
	if config.Provider != GitHub && config.Provider != Gitee {
		return nil, errors.New("gist: unknown provider")
	}
	if strings.TrimSpace(config.APIBase) == "" {
		config.APIBase = DefaultAPIBaseFor(config.Provider)
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.APIBase), "/"))
	if err != nil || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("gist: %q is not a server address", config.APIBase)
	}
	if err := cloudsync.CheckAddress(base, false); err != nil {
		if errors.Is(err, cloudsync.ErrInsecure) {
			return nil, err
		}
		return nil, fmt.Errorf("gist: %q is not an http or https address", config.APIBase)
	}
	config.Token = strings.TrimSpace(config.Token)
	if config.Token == "" || strings.ContainsAny(config.Token, " \r\n\x00") {
		return nil, errors.New("gist: an access token is required")
	}
	config.ID = strings.TrimSpace(config.ID)
	if config.ID != "" && !validID(config.Provider, config.ID) {
		return nil, errors.New("gist: that is not a snippet id")
	}
	config.File = strings.TrimSpace(config.File)
	if config.File == "" {
		config.File = DefaultFile
	}
	if strings.ContainsAny(config.File, "/\\\x00\r\n") {
		return nil, errors.New("gist: a snippet file name cannot contain a slash")
	}
	return &Client{config: config, base: base, http: cloudsync.HTTPClient(client, false)}, nil
}

// ID is the snippet the backup is in. It is empty until the first Put created one.
func (client *Client) ID() string { return client.config.ID }

// validID keeps the id one path segment. GitHub ids are hex; Gitee's are
// alphanumeric, sometimes with a dash or underscore.
func validID(provider Provider, id string) bool {
	if provider != Gitee {
		return strings.Trim(id, "0123456789abcdefABCDEF") == ""
	}
	if id == "" || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func (client *Client) do(ctx context.Context, method, route string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	target := client.base.String() + route
	if client.config.Provider == Gitee {
		// Gitee API v5 takes the token as a query parameter rather than an
		// Authorization header.
		parsed, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		query := parsed.Query()
		query.Set("access_token", client.config.Token)
		parsed.RawQuery = query.Encode()
		target = parsed.String()
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	if client.config.Provider == GitHub {
		request.Header.Set("Authorization", "Bearer "+client.config.Token)
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	request.Header.Set("User-Agent", "motty")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return client.http.Do(request)
}

// gistFile is one file of a snippet.
type gistFile struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	RawURL    string `json:"raw_url"`
}

// gistFiles accepts both GitHub's object and Gitee's JSON-encoded string.
type gistFiles map[string]gistFile

func (files *gistFiles) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return err
		}
		data = bytes.TrimSpace([]byte(encoded))
		if len(data) == 0 || bytes.Equal(data, []byte("null")) {
			return nil
		}
	}
	var parsed map[string]gistFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*files = parsed
	return nil
}

// gistHistory accepts GitHub's array and Gitee's opaque string.
type gistHistory []struct {
	Version string `json:"version"`
}

func (history *gistHistory) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return err
		}
		data = bytes.TrimSpace([]byte(encoded))
		if len(data) == 0 || data[0] != '[' {
			// Gitee documents history as a string, and its commit history is a
			// different resource; the content hash identifies the revision.
			return nil
		}
	}
	if data[0] != '[' {
		return nil
	}
	var parsed []struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*history = parsed
	return nil
}

// apiGist is the part of a snippet the program reads.
type apiGist struct {
	ID        string      `json:"id"`
	UpdatedAt string      `json:"updated_at"`
	History   gistHistory `json:"history"`
	Files     gistFiles   `json:"files"`
	Truncated bool        `json:"truncated"`
}

// tag identifies a revision. GitHub's newest commit changes on every edit
// (updated_at has only second resolution). Gitee has no revision field, so the
// content itself names the revision.
func (client *Client) tag(g apiGist) string {
	if client.config.Provider == Gitee {
		if file, ok := g.Files[client.config.File]; ok && !file.Truncated {
			return contentTag(file.Content)
		}
		return ""
	}
	if len(g.History) > 0 && g.History[0].Version != "" {
		return g.History[0].Version
	}
	return g.UpdatedAt
}

func contentTag(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// failure turns an error response into an error, with the service's own message.
func failure(provider Provider, response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	var parsed struct {
		Message string `json:"message"`
	}
	message := response.Status
	if json.Unmarshal(data, &parsed) == nil && parsed.Message != "" {
		message = fmt.Sprintf("%d %s", response.StatusCode, strings.Join(strings.Fields(parsed.Message), " "))
	}
	switch {
	case response.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w (%s)", cloudsync.ErrUnauthorized, message)
	case provider == GitHub && response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0":
		return fmt.Errorf("gist: GitHub's rate limit is used up, try again later (%s)", message)
	case response.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w (%s)", cloudsync.ErrUnauthorized, message)
	}
	return fmt.Errorf("gist: %s answered %s", provider, message)
}

// decodeGist accepts both GitHub's object and Gitee's one-element array.
func decodeGist(reader io.Reader) (apiGist, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 8*MaxFileBytes))
	if err != nil {
		return apiGist{}, err
	}
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		var list []apiGist
		if err := json.Unmarshal(data, &list); err != nil {
			return apiGist{}, err
		}
		if len(list) == 0 {
			return apiGist{}, errors.New("the answer did not include the snippet")
		}
		return list[0], nil
	}
	var g apiGist
	if err := json.Unmarshal(data, &g); err != nil {
		return apiGist{}, err
	}
	return g, nil
}

// fetch reads the snippet. found is false when it does not exist.
func (client *Client) fetch(ctx context.Context) (g apiGist, found bool, err error) {
	response, err := client.do(ctx, http.MethodGet, "/gists/"+client.config.ID, nil)
	if err != nil {
		return g, false, err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return g, false, nil
	case response.StatusCode != http.StatusOK:
		return g, false, failure(client.config.Provider, response)
	}
	g, err = decodeGist(response.Body)
	if err != nil {
		return g, false, fmt.Errorf("gist: the answer could not be read: %w", err)
	}
	return g, true, nil
}

// Test checks the token. With a snippet id it also checks that the snippet is there.
func (client *Client) Test(ctx context.Context) error {
	if client.config.ID != "" {
		_, found, err := client.fetch(ctx)
		if err == nil && !found {
			return errors.New("gist: that snippet was not found with this token")
		}
		return err
	}
	response, err := client.do(ctx, http.MethodGet, "/gists?per_page=1", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return failure(client.config.Provider, response)
	}
	return nil
}

// Get downloads the backup. The tag identifies this revision of the snippet.
func (client *Client) Get(ctx context.Context) (data []byte, tag string, err error) {
	if client.config.ID == "" {
		return nil, "", cloudsync.ErrNotFound
	}
	g, found, err := client.fetch(ctx)
	if err != nil {
		return nil, "", err
	}
	file, has := g.Files[client.config.File]
	if !found || !has {
		return nil, "", cloudsync.ErrNotFound
	}
	if file.Truncated || g.Truncated && file.Content == "" {
		return nil, "", cloudsync.ErrTooLarge
	}
	data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(file.Content))
	if err != nil {
		return nil, "", errors.New("gist: the file is not a Motty backup")
	}
	return data, client.tag(g), nil
}

// Put uploads the backup, creating a secret snippet when there is none. It is
// first read: it must still be at the revision previous names, or, when previous
// is empty, must not hold the file yet (cloudsync.ErrChanged otherwise). The API has no conditional update,
// so another computer that saves between that read and the PATCH is not
// noticed; the window is one round trip.
func (client *Client) Put(ctx context.Context, data []byte, previous string) (tag string, err error) {
	content := base64.StdEncoding.EncodeToString(data)
	if len(content) > MaxFileBytes {
		return "", fmt.Errorf("%w: a snippet file holds at most 1 MB, and this backup is %d KB as text", cloudsync.ErrTooLarge, len(content)/1000+1)
	}
	files := map[string]map[string]string{client.config.File: {"content": content}}
	method, route := http.MethodPatch, "/gists/"+client.config.ID
	var body any = map[string]any{"files": files}
	if client.config.ID == "" {
		method, route = http.MethodPost, "/gists"
		body = map[string]any{"description": "Motty encrypted backup", "public": false, "files": files}
	} else {
		if client.config.Provider == Gitee {
			body = map[string]any{"description": "Motty encrypted backup", "files": files}
		}
		g, found, err := client.fetch(ctx)
		if err != nil {
			return "", err
		}
		_, has := g.Files[client.config.File]
		if previous != "" && (!found || client.tag(g) != previous) || previous == "" && found && has {
			return "", cloudsync.ErrChanged
		}
	}
	response, err := client.do(ctx, method, route, body)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return "", failure(client.config.Provider, response)
	}
	g, err := decodeGist(response.Body)
	if err != nil {
		return "", fmt.Errorf("gist: the answer could not be read: %w", err)
	}
	if client.config.ID == "" {
		client.config.ID = g.ID
	}
	if tag := client.tag(g); tag != "" {
		return tag, nil
	}
	// A service may answer an update without the file body. The content just
	// sent is still the revision this client now knows.
	return contentTag(content), nil
}
