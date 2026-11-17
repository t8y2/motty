package gist

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"motty/internal/cloudsync"
)

// A separate disposable snippet avoids modifying files or revision history in
// the real backup. The returned id stays in the temporary client only.
func (client *Client) TestReadWrite(ctx context.Context) error {
	name, err := cloudsync.ProbeName()
	if err != nil {
		return err
	}
	probe := *client
	probe.probe = true
	probe.config.ID, probe.config.File = "", name
	return cloudsync.Probe(ctx, &probe, func(cleanup context.Context) error {
		id := probe.config.ID
		if id == "" || id == client.config.ID || !validID(client.config.Provider, id) {
			return errors.New("gist: the service did not return a distinct test snippet id")
		}
		response, err := probe.do(cleanup, http.MethodDelete, "/gists/"+id, nil)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusNotFound || response.StatusCode >= 200 && response.StatusCode < 300 {
			return nil
		}
		return fmt.Errorf("gist: remove temporary snippet %s: %w", id, failure(client.config.Provider, response))
	})
}
