package admin

import (
	"bytes"
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
		// obiectl makes one request per run; keep no idle connections.
		DisableKeepAlives: true,
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

// Identity fetches the node identity.
func (c *Client) Identity(ctx context.Context) (*IdentityResponse, error) {
	var resp IdentityResponse
	if err := c.get(ctx, IdentityPath, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Peers fetches the connected mesh peers.
func (c *Client) Peers(ctx context.Context) (*PeersResponse, error) {
	var resp PeersResponse
	if err := c.get(ctx, PeersPath, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// StatusError is an admin API answer other than 200 OK.
type StatusError struct {
	Method, Path string
	// Code is the HTTP status code, Status its text.
	Code   int
	Status string
	// Body is the start of the response body.
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: %s: %s", e.Method, e.Path, e.Status, e.Body)
}

// do sends a request with body (as JSON, if not nil) and decodes the
// response into out.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(data)
	}
	// The host is ignored: the transport always dials the socket.
	req, err := http.NewRequestWithContext(ctx, method, "http://obied"+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.dialError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &StatusError{Method: method, Path: path, Code: resp.StatusCode, Status: resp.Status, Body: string(data)}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
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
