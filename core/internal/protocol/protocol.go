// Package protocol defines the wire protocol between the agentdroid CLI and
// a runtime adapter that serves an Android app session.
//
// Messages are newline-delimited JSON (NDJSON): exactly one JSON object per
// line, UTF-8, terminated by '\n'. Every request carries an ID that the
// response echoes back, so a session can be multiplexed or retried safely.
package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

// Command names accepted in Request.Cmd.
const (
	CmdLaunch     = "launch"
	CmdSnapshot   = "snapshot"
	CmdTap        = "tap"
	CmdType       = "type"
	CmdScreenshot = "screenshot"
	CmdClose      = "close"
)

// MaxMessageSize bounds one NDJSON line. It is generous enough for a large
// view tree or a full-screen PNG in base64; anything bigger is treated as a
// protocol violation.
const MaxMessageSize = 16 << 20 // 16 MiB

// Request is one command sent by the CLI to a runtime adapter. X and Y are
// pixel coordinates inside the rendered frame.
type Request struct {
	ID   int64  `json:"id"`
	Cmd  string `json:"cmd"`
	APK  string `json:"apk,omitempty"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	Text string `json:"text,omitempty"`
}

// Response is the adapter's reply to a request. OK=false carries a
// human-readable Error. Tree carries the view hierarchy for snapshot
// requests, and PNG carries raw PNG bytes (base64 in JSON) for screenshots.
type Response struct {
	ID      int64        `json:"id"`
	OK      bool         `json:"ok"`
	Error   string       `json:"error,omitempty"`
	Session string       `json:"session,omitempty"`
	PID     int          `json:"pid,omitempty"`
	Tree    *uitree.Node `json:"tree,omitempty"`
	PNG     []byte       `json:"png,omitempty"`
}

// WriteMessage serializes v as a single line of JSON on w.
func WriteMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	b = append(b, '\n')
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

// Reader reads NDJSON messages with a size bound, so a broken peer cannot
// make the CLI allocate without limit.
type Reader struct {
	sc *bufio.Scanner
}

// NewReader wraps r for use with Reader.Next.
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), MaxMessageSize)
	return &Reader{sc: sc}
}

// Next decodes the next message into v. It returns io.EOF once the stream
// ends cleanly, and an error for truncated, malformed or oversized messages.
// A trailing fragment after a transport failure (e.g. connection reset)
// surfaces the transport error instead of a confusing decode error.
func (r *Reader) Next(v any) error {
	if !r.sc.Scan() {
		err := r.sc.Err()
		if err == nil {
			return io.EOF
		}
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("message exceeds %d bytes", MaxMessageSize)
		}
		return err
	}
	line := r.sc.Bytes()
	if err := json.Unmarshal(line, v); err == nil {
		return nil
	} else if serr := r.sc.Err(); serr != nil {
		// This token is the leftover before the connection broke: the
		// transport error is the real cause of the garbage.
		if errors.Is(serr, bufio.ErrTooLong) {
			return fmt.Errorf("message exceeds %d bytes", MaxMessageSize)
		}
		return serr
	} else {
		return fmt.Errorf("decode message: %w", err)
	}
}
