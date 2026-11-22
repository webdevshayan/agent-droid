package uitree

import (
	"fmt"
	"strings"
)

// maxXMLDepth mirrors snapshot.MaxDepth: the renderer refuses trees nested
// deeper than the CLI parser will ever accept, so a Dump can never produce
// XML that the snapshot pipeline then rejects.
const maxXMLDepth = 64

// XML renders the tree as a uiautomator-style dump — the exact format
// core/snapshot parses. Runtime sessions and adb therefore share one
// element pipeline and one @eN ref semantics: every node with sane bounds
// earns a ref, in document order, exactly as with a real device dump.
// The tree must be well-formed (Validate); all text content is escaped.
func XML(root *Node) ([]byte, error) {
	if root == nil {
		return nil, fmt.Errorf("render xml: empty view tree")
	}
	if err := Validate(root); err != nil {
		return nil, fmt.Errorf("render xml: %w", err)
	}
	var b strings.Builder
	b.WriteString("<?xml version='1.0' encoding='UTF-8' ?>\n")
	b.WriteString("<hierarchy rotation=\"0\">\n")
	if err := writeNode(&b, root, 1); err != nil {
		return nil, err
	}
	b.WriteString("</hierarchy>\n")
	return []byte(b.String()), nil
}

// writeNode emits one node and its subtree. Bounds convert from the wire's
// [x, y, width, height] to uiautomator's [x1,y1][x2,y2].
func writeNode(b *strings.Builder, n *Node, depth int) error {
	if depth > maxXMLDepth {
		return fmt.Errorf("render xml: view tree deeper than %d levels", maxXMLDepth)
	}
	fmt.Fprintf(b,
		"%s<node class=%q text=%q content-desc=%q resource-id=%q clickable=%q focusable=%q enabled=\"true\" scrollable=\"false\" bounds=\"[%d,%d][%d,%d]\"",
		strings.Repeat("  ", depth),
		esc(n.Class), esc(n.Text), esc(n.ContentDesc), esc(n.ResourceID),
		boolAttr(n.Clickable), boolAttr(n.Focusable),
		n.Bounds[0], n.Bounds[1], n.Bounds[0]+n.Bounds[2], n.Bounds[1]+n.Bounds[3])
	if len(n.Children) == 0 {
		b.WriteString("/>\n")
		return nil
	}
	b.WriteString(">\n")
	for _, c := range n.Children {
		if err := writeNode(b, c, depth+1); err != nil {
			return err
		}
	}
	fmt.Fprintf(b, "%s</node>\n", strings.Repeat("  ", depth))
	return nil
}

func boolAttr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// esc escapes the five XML specials so text can never break the dump.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
