package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

func TestRequestRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	req := Request{ID: 7, Cmd: CmdType, X: 30, Y: 40, Text: `hello "world"`}
	if err := WriteMessage(&buf, &req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	var got Request
	if err := NewReader(&buf).Next(&got); err != nil {
		t.Fatalf("read request: %v", err)
	}
	if got != req {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, req)
	}
}

func TestResponseRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	resp := Response{
		ID:  3,
		OK:  true,
		PID: 42,
		Tree: &uitree.Node{
			Class:  "android.widget.Button",
			Text:   "Hi",
			Bounds: uitree.Bounds{1, 2, 3, 4},
		},
		PNG: []byte{0x89, 'P', 'N', 'G'},
	}
	if err := WriteMessage(&buf, &resp); err != nil {
		t.Fatalf("write response: %v", err)
	}
	var got Response
	if err := NewReader(&buf).Next(&got); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got.ID != resp.ID || !got.OK || got.PID != resp.PID {
		t.Fatalf("scalar fields mismatch: %+v", got)
	}
	if got.Tree == nil || got.Tree.Class != "android.widget.Button" || got.Tree.Bounds != resp.Tree.Bounds {
		t.Fatalf("tree mismatch: %+v", got.Tree)
	}
	if !bytes.Equal(got.PNG, resp.PNG) {
		t.Fatalf("png mismatch: %v", got.PNG)
	}
}

func TestReaderRejectsGarbage(t *testing.T) {
	r := NewReader(strings.NewReader("not json\n"))
	var req Request
	if err := r.Next(&req); err == nil {
		t.Fatal("expected decode error for garbage line")
	}
}

func TestReaderRejectsOversizedLine(t *testing.T) {
	line := strings.Repeat("x", MaxMessageSize+1) + "\n"
	r := NewReader(strings.NewReader(line))
	var req Request
	if err := r.Next(&req); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestReaderEOF(t *testing.T) {
	var req Request
	if err := NewReader(strings.NewReader("")).Next(&req); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestReaderTruncatedMessage(t *testing.T) {
	r := NewReader(strings.NewReader(`{"id":1,"cmd":"tap"`))
	var req Request
	if err := r.Next(&req); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("expected truncation error, got %v", err)
	}
}
