// Package snapshot parses uiautomator UI dumps into Element lists and
// assigns stable positional refs (@e1..@eN).
//
// The dump is untrusted input: it comes off a device (or from a fuzzer),
// so Parse is streaming (no unbounded recursion), enforces hard size,
// depth and element-count caps, and never panics on malformed XML.
package snapshot

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Hard limits that keep a hostile dump from exhausting memory.
const (
	// MaxSnapshotBytes caps input size before anything is parsed.
	MaxSnapshotBytes = 20 << 20 // 20 MiB — a real uiautomator dump is < 2 MiB
	// MaxElements caps how many refs one dump may produce.
	MaxElements = 10000
	// MaxDepth caps XML nesting; the decoder loop is iterative, this
	// bounds work per dump rather than guarding the Go stack.
	MaxDepth = 64
)

// Rect is a parsed bounds attribute, [x1,y1][x2,y2].
type Rect struct {
	X1, Y1, X2, Y2 int
}

// Valid reports whether the rect is a sane, tappable screen region.
func (r Rect) Valid() bool {
	return r.X1 >= 0 && r.Y1 >= 0 && r.X2 >= r.X1 && r.Y2 >= r.Y1
}

// Center is the tap point for the element.
func (r Rect) Center() (int, int) {
	return r.X1 + (r.X2-r.X1)/2, r.Y1 + (r.Y2-r.Y1)/2
}

// Element is one tappable node of the UI hierarchy.
type Element struct {
	Ref         string // "@eN", assigned by AssignRefs
	Class       string
	ResourceID  string
	Text        string
	ContentDesc string
	Package     string
	Clickable   bool
	Scrollable  bool
	Enabled     bool
	Bounds      Rect
}

// Parse converts a uiautomator XML dump into elements, in document order,
// each with a valid bounds rect. Nodes with missing/malformed bounds are
// skipped (they are not tappable) but their children are still walked.
// The input is never mutated.
func Parse(data []byte) ([]Element, error) {
	if len(data) > MaxSnapshotBytes {
		return nil, fmt.Errorf("snapshot dump is %d bytes, over the %d-byte limit; refusing to parse", len(data), MaxSnapshotBytes)
	}
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(data) == 0 {
		return nil, errors.New("snapshot dump is empty")
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true

	var elements []Element
	depth := 0
	sawRoot := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("snapshot: invalid XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !sawRoot {
				// A real uiautomator dump always roots at <hierarchy>.
				// Enforcing that turns garbage input into a clean error
				// instead of a silent "no elements" result.
				if t.Name.Local != "hierarchy" {
					return nil, fmt.Errorf("snapshot: not a uiautomator dump (root element is <%s>, want <hierarchy>)", t.Name.Local)
				}
				sawRoot = true
				continue
			}
			if t.Name.Local != "node" {
				continue
			}
			depth++
			if depth > MaxDepth {
				return nil, fmt.Errorf("snapshot: XML nesting deeper than %d levels", MaxDepth)
			}
			el, ok := elementFromAttrs(t.Attr)
			if !ok {
				continue
			}
			if len(elements) >= MaxElements {
				return nil, fmt.Errorf("snapshot has more than %d tappable elements; refusing to assign refs", MaxElements)
			}
			elements = append(elements, el)
		case xml.EndElement:
			if t.Name.Local == "node" && depth > 0 {
				depth--
			}
		}
	}

	if !sawRoot {
		return nil, errors.New("snapshot: dump is not XML (no root element found)")
	}

	AssignRefs(elements)
	return elements, nil
}

// elementFromAttrs extracts one element from a <node> attribute list.
// It returns false when the bounds attribute is missing or malformed —
// such nodes cannot be tapped and get no ref.
func elementFromAttrs(attrs []xml.Attr) (Element, bool) {
	var e Element
	var bounds string
	for _, a := range attrs {
		switch a.Name.Local {
		case "class":
			e.Class = a.Value
		case "resource-id":
			e.ResourceID = a.Value
		case "text":
			e.Text = a.Value
		case "content-desc":
			e.ContentDesc = a.Value
		case "package":
			e.Package = a.Value
		case "clickable":
			e.Clickable = a.Value == "true"
		case "scrollable":
			e.Scrollable = a.Value == "true"
		case "enabled":
			e.Enabled = a.Value != "false"
		case "bounds":
			bounds = a.Value
		}
	}
	r, ok := parseBounds(bounds)
	if !ok {
		return Element{}, false
	}
	e.Bounds = r
	return e, true
}

// parseBounds strictly parses "[x1,y1][x2,y2]".
func parseBounds(s string) (Rect, bool) {
	if len(s) < 2 || !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return Rect{}, false
	}
	parts := strings.Split(s[1:len(s)-1], "][")
	if len(parts) != 2 {
		return Rect{}, false
	}
	a := strings.Split(parts[0], ",")
	b := strings.Split(parts[1], ",")
	if len(a) != 2 || len(b) != 2 {
		return Rect{}, false
	}
	var v [4]int
	nums := [...]string{a[0], a[1], b[0], b[1]}
	for i, ns := range nums {
		n, err := strconv.Atoi(ns)
		if err != nil {
			return Rect{}, false
		}
		v[i] = n
	}
	r := Rect{v[0], v[1], v[2], v[3]}
	if !r.Valid() {
		return Rect{}, false
	}
	return r, true
}
