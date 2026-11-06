// Package gist keeps the encrypted backup in one file of a secret GitHub Gist,
// through the REST API: test the token, upload, and download. A gist is text, so
// the sealed bytes are stored base64-encoded. It never lists, deletes or touches
// any other file of the gist.
package gist

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"motty/internal/cloudsync"
)

// Config says where the backup is and who may touch it.
type Config struct {
	// Token is a personal access token that may read and write gists (a
	// fine-grained token with the Gists permission, or a classic one with the
	// gist scope), and nothing else.
	Token string
	// ID is the gist that holds the backup; empty means none exists yet and the
	// first Put creates a secret one (see Client.ID).
	ID string
	// File is the file in the gist.
	File string
	// APIBase overrides https://api.github.com, for GitHub Enterprise and tests.
	// https:// only, except for a loopback address.
	APIBase string
}

// DefaultFile is the file name used when the user gives none.
const DefaultFile = "motty-backup.mottybak.b64"

// DefaultAPIBase is where the gists live.
const DefaultAPIBase = "https://api.github.com"

// MaxFileBytes is what the API takes per file in one request (1 MB of text); a
// bigger file is truncated when read back, so it is refused before it is sent.
const MaxFileBytes = 1_000_000

// Client talks to one gist.
type Client struct {
	config Config
	base   *url.URL
	http   *http.Client
}

var _ cloudsync.Store = (*Client)(nil)

// New checks the configuration. A nil http client does not follow redirects to
// another host. The caller controls the request deadline through its context.
func New(config Config, client *http.Client) (*Client, error) {
	if strings.TrimSpace(config.APIBase) == "" {
		config.APIBase = DefaultAPIBase
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
	if config.ID != "" && strings.Trim(config.ID, "0123456789abcdefABCDEF") != "" {
		return nil, errors.New("gist: that is not a gist id")
	}
	config.File = strings.TrimSpace(config.File)
	if config.File == "" {
		config.File = DefaultFile
	}
	if strings.ContainsAny(config.File, "/\\\x00\r\n") {
		return nil, errors.New("gist: a gist file name cannot contain a slash")
	}
	return &Client{config: config, base: base, http: cloudsync.HTTPClient(client, false)}, nil
}

// ID is the gist the backup is in. It is empty until the first Put created one.
func (client *Client) ID() string { return client.config.ID }

func (client *Client) do(ctx context.Context, method, route string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.base.String()+route, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+client.config.Token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "motty")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return client.http.Do(request)
}

// apiGist is the part of a gist the program reads.
type apiGist struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updated_at"`
	History   []struct {
		Version string `json:"version"`
	} `json:"history"`
	Files map[string]struct {
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	} `json:"files"`
	Truncated bool `json:"truncated"`
}

// tag identifies a revision: the newest commit of the gist, which changes on
// every edit (updated_at has only second resolution).
func (g apiGist) tag() string {
	if len(g.History) > 0 && g.History[0].Version != "" {
		return g.History[0].Version
	}
	return g.UpdatedAt
}

// failure turns an error response into an error, with GitHub's own message.
func failure(response *http.Response) error {
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
	case response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0":
		return fmt.Errorf("gist: GitHub's rate limit is used up, try again later (%s)", message)
	case response.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w (%s)", cloudsync.ErrUnauthorized, message)
	}
	return fmt.Errorf("gist: GitHub answered %s", message)
}

// fetch reads the gist. found is false when it does not exist.
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
		return g, false, failure(response)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8*MaxFileBytes)).Decode(&g); err != nil {
		return g, false, fmt.Errorf("gist: GitHub's answer could not be read: %w", err)
	}
	return g, true, nil
}

// Test checks the token. With a gist id it also checks that the gist is there.
func (client *Client) Test(ctx context.Context) error {
	if client.config.ID != "" {
		_, found, err := client.fetch(ctx)
		if err == nil && !found {
			return errors.New("gist: that gist was not found with this token")
		}
		return err
	}
	response, err := client.do(ctx, http.MethodGet, "/gists?per_page=1", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return failure(response)
	}
	return nil
}

// Get downloads the backup. The tag identifies this revision of the gist.
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
	return data, g.tag(), nil
}

// Put uploads the backup, creating a secret gist when there is none. The gist is
// first read: it must still be at the revision previous names, or, when previous
// is empty, must not hold the file yet (cloudsync.ErrChanged otherwise). The API has no conditional update,
// so another computer that saves between that read and the PATCH is not
// noticed; the window is one round trip.
func (client *Client) Put(ctx context.Context, data []byte, previous string) (tag string, err error) {
	content := base64.StdEncoding.EncodeToString(data)
	if len(content) > MaxFileBytes {
		return "", fmt.Errorf("%w: a gist file holds at most 1 MB, and this backup is %d KB as text", cloudsync.ErrTooLarge, len(content)/1000+1)
	}
	files := map[string]map[string]string{client.config.File: {"content": content}}
	method, route := http.MethodPatch, "/gists/"+client.config.ID
	var body any = map[string]any{"files": files}
	if client.config.ID == "" {
		method, route = http.MethodPost, "/gists"
		body = map[string]any{"description": "Motty encrypted backup", "public": false, "files": files}
	} else {
		g, found, err := client.fetch(ctx)
		if err != nil {
			return "", err
		}
		_, has := g.Files[client.config.File]
		if previous != "" && (!found || g.tag() != previous) || previous == "" && found && has {
			return "", cloudsync.ErrChanged
		}
	}
	response, err := client.do(ctx, method, route, body)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return "", failure(response)
	}
	var g apiGist
	if err := json.NewDecoder(io.LimitReader(response.Body, 8*MaxFileBytes)).Decode(&g); err != nil {
		return "", fmt.Errorf("gist: GitHub's answer could not be read: %w", err)
	}
	if client.config.ID == "" {
		client.config.ID = g.ID
	}
	return g.tag(), nil
}
