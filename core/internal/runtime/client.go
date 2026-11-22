package runtime

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
)

// Client is the CLI side of a session: it dials the session's abstract
// socket and exchanges protocol requests and responses.
type Client struct {
	conn net.Conn
	br   *protocol.Reader
	mu   sync.Mutex
	seq  int64
}

// DialSession connects to the abstract socket of a live session. A missing
// session produces an error that tells the operator what to do next.
func DialSession(session string, timeout time.Duration) (*Client, error) {
	addr := "@" + SocketPrefix + session
	conn, err := net.DialTimeout("unix", addr, timeout)
	if err != nil {
		if isConnRefused(err) {
			return nil, fmt.Errorf("no live session %q — run agentdroid launch first (%v)", session, err)
		}
		return nil, fmt.Errorf("connect to session %q: %w", session, err)
	}
	return &Client{conn: conn, br: protocol.NewReader(conn)}, nil
}

func isConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

// Call sends req, waits for the matching response, and enforces a deadline.
func (c *Client) Call(req protocol.Request, timeout time.Duration) (*protocol.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.seq++
	req.ID = c.seq
	if err := c.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	if err := protocol.WriteMessage(c.conn, &req); err != nil {
		return nil, fmt.Errorf("send %s: %w", req.Cmd, err)
	}
	var resp protocol.Response
	if err := c.br.Next(&resp); err != nil {
		return nil, fmt.Errorf("read %s response: %w", req.Cmd, err)
	}
	if resp.ID != req.ID {
		return nil, fmt.Errorf("response id mismatch: sent %d, got %d", req.ID, resp.ID)
	}
	return &resp, nil
}

// NewClient wraps an existing connection — a live socket, or a net.Pipe in
// tests and embedders — as a session client.
func NewClient(conn net.Conn) *Client {
	return &Client{conn: conn, br: protocol.NewReader(conn)}
}

// Close drops the connection without ending the session.
func (c *Client) Close() error { return c.conn.Close() }
