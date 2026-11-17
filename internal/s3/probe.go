package s3

import (
	"context"
	"net/http"
	"path"

	"motty/internal/cloudsync"
)

func (client *Client) TestReadWrite(ctx context.Context) error {
	name, err := cloudsync.ProbeName()
	if err != nil {
		return err
	}
	probe := *client
	probe.config.Key = path.Join(path.Dir(client.config.Key), name)
	return cloudsync.Probe(ctx, &probe, func(cleanup context.Context) error {
		response, err := probe.do(cleanup, http.MethodDelete, nil, nil)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			return nil
		}
		_, err = check(response)
		return err
	})
}
