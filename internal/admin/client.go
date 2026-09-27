package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
)

// ErrDaemonNotRunning is returned by the client when nothing listens on the
// admin socket.
var ErrDaemonNotRunning = errors.New("obied is not running")

// maxErrorBody bounds how much of an error response is quoted.
const maxErrorBody = 512

// Client talks to the admin API of a local obied.
type Client struct {
	socket string
	http   *http.Client
}

// NewClient returns a client for the admin socket at socket.
func NewClient(socket string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{socket: socket, http: &http.Client{Transport: transport}}
}

// Status fetches the node status.
func (c *Client) Status(ctx context.Context) (*StatusResponse, error) {
	var resp StatusResponse
	if err := c.get(ctx, StatusPath, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	// The host is ignored: the transport always dials the socket.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://obied"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.dialError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: decode response: %w", path, err)
	}
	return nil
}

func (c *Client) dialError(err error) error {
	switch {
	case errors.Is(err, syscall.ENOENT), errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("%w: nothing is listening on admin socket %s", ErrDaemonNotRunning, c.socket)
	case errors.Is(err, syscall.EACCES):
		return fmt.Errorf("permission denied on admin socket %s: run as root or as a member of the socket's group", c.socket)
	default:
		return fmt.Errorf("connect to admin socket %s: %w", c.socket, err)
	}
}
