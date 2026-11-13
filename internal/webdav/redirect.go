package webdav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"motty/internal/cloudsync"
)

var errDownloadVersion = errors.New("webdav: the redirected download has no WebDAV file version; in AList or OpenList, select the native proxy WebDAV policy")

func (client *Client) download(ctx context.Context, metadataTag string) (*http.Response, string, error) {
	downloadHTTP := *client.http
	external, tag := false, metadataTag
	downloadHTTP.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return http.ErrUseLastResponse
		}
		if request.URL.User != nil {
			return errors.New("webdav: a download redirect cannot include credentials")
		}
		if via[len(via)-1].URL.Scheme == "https" && request.URL.Scheme == "http" {
			return ErrInsecure
		}
		if err := cloudsync.CheckAddress(request.URL, client.config.AllowInsecureHTTP); err != nil {
			return err
		}
		if !external {
			tag = request.Response.Header.Get("ETag")
			if tag == "" {
				tag = metadataTag
			}
		}
		if !sameOrigin(request.URL, via[0].URL) {
			if tag == "" {
				return errDownloadVersion
			}
			external = true
		}
		if external {
			request.Header.Del("Authorization")
			request.Header.Del("Proxy-Authorization")
			request.Header.Del("Cookie")
			request.Header.Del("Referer")
			downloadHTTP.Jar = nil
		}
		return nil
	}
	download := *client
	download.http = &downloadHTTP
	response, err := download.do(ctx, http.MethodGet, client.address(strings.Split(client.config.Path, "/")...), nil, nil)
	if err != nil {
		return nil, "", err
	}
	if !external {
		tag = response.Header.Get("ETag")
		if tag == "" {
			tag = metadataTag
		}
	}
	return response, tag, nil
}

func sameOrigin(left, right *url.URL) bool {
	return left.Scheme == right.Scheme && strings.EqualFold(left.Host, right.Host)
}

func (client *Client) fileETag(ctx context.Context) (string, error) {
	address := client.address(strings.Split(client.config.Path, "/")...)
	body := []byte(`<d:propfind xmlns:d="DAV:"><d:prop><d:getetag/></d:prop></d:propfind>`)
	response, err := client.do(ctx, "PROPFIND", address, body, map[string]string{"Depth": "0", "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusMultiStatus:
	case http.StatusNotFound:
		return "", ErrNotFound
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return "", fmt.Errorf("%w (%s)", errDownloadVersion, response.Status)
	default:
		return "", fmt.Errorf("webdav: the file properties request answered %s", response.Status)
	}
	const limit = 64 << 10
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", ErrTooLarge
	}
	var properties struct {
		XMLName   xml.Name `xml:"DAV: multistatus"`
		Responses []struct {
			Href       string `xml:"DAV: href"`
			Properties []struct {
				Prop struct {
					Tag string `xml:"DAV: getetag"`
				} `xml:"DAV: prop"`
				Status string `xml:"DAV: status"`
			} `xml:"DAV: propstat"`
		} `xml:"DAV: response"`
	}
	if err := xml.Unmarshal(data, &properties); err != nil {
		return "", fmt.Errorf("webdav: invalid file properties: %w", err)
	}
	file, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	if response.Request != nil {
		file = response.Request.URL
	}
	for _, resource := range properties.Responses {
		href, err := url.Parse(strings.TrimSpace(resource.Href))
		if err != nil || resource.Href == "" {
			continue
		}
		target := file.ResolveReference(href)
		if !sameOrigin(target, file) || target.Path != file.Path {
			continue
		}
		for _, property := range resource.Properties {
			status := strings.Fields(property.Status)
			tag := strings.TrimSpace(property.Prop.Tag)
			if len(status) >= 2 && status[1] == "200" && tag != "" && !strings.ContainsAny(tag, "\r\n") {
				return tag, nil
			}
		}
	}
	return "", errDownloadVersion
}
