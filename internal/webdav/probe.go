package webdav

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"motty/internal/cloudsync"
)

func (client *Client) TestReadWrite(ctx context.Context) error {
	name, err := cloudsync.ProbeName()
	if err != nil {
		return err
	}
	probe := *client
	probe.config.Path = path.Join(path.Dir(client.config.Path), name)
	return cloudsync.Probe(ctx, &probe, func(cleanup context.Context) error {
		response, err := probe.do(cleanup, http.MethodDelete, probe.address(strings.Split(probe.config.Path, "/")...), nil, nil)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusNotFound || response.StatusCode >= 200 && response.StatusCode < 300 {
			return nil
		}
		return fmt.Errorf("webdav: delete %s: %s", probe.config.Path, response.Status)
	})
}
