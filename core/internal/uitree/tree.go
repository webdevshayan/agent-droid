// Package uitree models the view hierarchy a runtime adapter reports for the
// running app, and derives stable element references (@e1, @e2, ...) that
// the CLI resolves into tap targets.
package uitree

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Bounds is a view's [x, y, width, height] rectangle. Decoding is strict:
// adapters must send exactly four numbers, so a malformed frame can never
// silently become a zero-sized box.
type Bounds [4]int

// UnmarshalJSON enforces the exact shape of Bounds on the wire.
func (b *Bounds) UnmarshalJSON(data []byte) error {
	var raw []int
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != 4 {
		return fmt.Errorf("bounds must be [x, y, width, height] (4 numbers), got %d", len(raw))
	}
	*b = Bounds{raw[0], raw[1], raw[2], raw[3]}
	return nil
}

// Node is one view in the reported hierarchy. Bounds are [x, y, width,
// height] in pixels with the origin at the top-left of the rendered frame.
type Node struct {
	Class       string  `json:"class,omitempty"`
	Text        string  `json:"text,omitempty"`
	ContentDesc string  `json:"content_desc,omitempty"`
	ResourceID  string  `json:"resource_id,omitempty"`
	Bounds      Bounds  `json:"bounds"`
	Clickable   bool    `json:"clickable,omitempty"`
	Focusable   bool    `json:"focusable,omitempty"`
	Children    []*Node `json:"children,omitempty"`
}

// Parse decodes a JSON view hierarchy and validates its geometry.
func Parse(data []byte) (*Node, error) {
	var root Node
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse ui tree: %w", err)
	}
	if err := Validate(&root); err != nil {
		return nil, err
	}
	return &root, nil
}

// Validate rejects impossible geometry (negative origins, negative sizes) so
// downstream ref resolution never operates on broken bounds.
func Validate(root *Node) error { return root.validate("root") }

func (n *Node) validate(path string) error {
	if n.Bounds[0] < 0 || n.Bounds[1] < 0 {
		return fmt.Errorf("ui tree %s: negative origin in bounds %v", path, n.Bounds)
	}
	if n.Bounds[2] < 0 || n.Bounds[3] < 0 {
		return fmt.Errorf("ui tree %s: negative size in bounds %v", path, n.Bounds)
	}
	for i, c := range n.Children {
		if err := c.validate(fmt.Sprintf("%s/children[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

// Center returns the center point of the node's bounds — the default tap
// target for an element.
func (n *Node) Center() (int, int) {
	return n.Bounds[0] + n.Bounds[2]/2, n.Bounds[1] + n.Bounds[3]/2
}

// Element is a view worth exposing to the driver of a test session, with the
// stable reference assigned for the current screen.
type Element struct {
	Ref   string // "@e1" style
	Index int    // 1-based
	Node  *Node
	Path  string // location in the tree, e.g. root/children[2]
}

var refRe = regexp.MustCompile(`^@e(0|[1-9][0-9]*)$`)

// ValidRef reports whether ref is a well-formed element reference.
func ValidRef(ref string) bool { return refRe.MatchString(ref) }

// Interesting reports whether a node earns an element ref: anything an agent
// can observe (labels, descriptions, ids) or interact with
// (clickable/focusable views).
func Interesting(n *Node) bool {
	return n.Clickable || n.Focusable || n.Text != "" || n.ContentDesc != "" || n.ResourceID != ""
}

// Flatten walks the tree depth-first (pre-order) and assigns refs @e1, @e2,
// ... to interesting nodes. The order is stable for a given tree, so refs
// are reproducible across snapshots of an unchanged screen.
func Flatten(root *Node) []*Element {
	var els []*Element
	var walk func(n *Node, path string)
	walk = func(n *Node, path string) {
		if Interesting(n) {
			els = append(els, &Element{
				Ref:   fmt.Sprintf("@e%d", len(els)+1),
				Index: len(els) + 1,
				Node:  n,
				Path:  path,
			})
		}
		for i, c := range n.Children {
			walk(c, fmt.Sprintf("%s/children[%d]", path, i))
		}
	}
	walk(root, "root")
	return els
}

// Resolve returns the element for ref, or an error that names the ref and
// the size of the current listing, so an agent knows to re-snapshot.
func Resolve(els []*Element, ref string) (*Element, error) {
	if !ValidRef(ref) {
		return nil, fmt.Errorf("invalid element reference %q (expected @e1 style)", ref)
	}
	idx, _ := strconv.Atoi(strings.TrimPrefix(ref, "@e"))
	if idx < 1 || idx > len(els) {
		return nil, fmt.Errorf("unknown element %s (current screen has %d elements; run agentdroid snapshot)", ref, len(els))
	}
	return els[idx-1], nil
}
