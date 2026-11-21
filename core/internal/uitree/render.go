package uitree

import (
	"fmt"
	"strings"
)

// RenderList renders elements as one line per element, in the compact form
// used by `agentdroid snapshot`. The output is stable and grep-friendly:
// ref, class, labels, id, bounds and interaction flags, separated by two
// spaces.
func RenderList(els []*Element) string {
	var b strings.Builder
	for _, el := range els {
		n := el.Node
		b.WriteString(el.Ref)
		b.WriteString("  ")
		b.WriteString(ShortClass(n.Class))
		if n.Text != "" {
			fmt.Fprintf(&b, "  text=%q", n.Text)
		}
		if n.ContentDesc != "" {
			fmt.Fprintf(&b, "  desc=%q", n.ContentDesc)
		}
		if n.ResourceID != "" {
			fmt.Fprintf(&b, "  id=%s", n.ResourceID)
		}
		fmt.Fprintf(&b, "  bounds=%d,%d %dx%d", n.Bounds[0], n.Bounds[1], n.Bounds[2], n.Bounds[3])
		var flags []string
		if n.Clickable {
			flags = append(flags, "clickable")
		}
		if n.Focusable {
			flags = append(flags, "focusable")
		}
		if len(flags) > 0 {
			b.WriteString("  ")
			b.WriteString(strings.Join(flags, ","))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// RenderTree renders the hierarchy as an indented tree with element refs
// inline, used by `agentdroid snapshot --tree`.
func RenderTree(root *Node) string {
	refs := make(map[*Node]string)
	for _, el := range Flatten(root) {
		refs[el.Node] = el.Ref
	}
	var b strings.Builder
	describe(root, refs[root], &b)
	b.WriteString("\n")
	for i, c := range root.Children {
		renderBranch(c, "", i == len(root.Children)-1, refs, &b)
	}
	return b.String()
}

func renderBranch(n *Node, prefix string, last bool, refs map[*Node]string, b *strings.Builder) {
	branch, next := "├── ", prefix+"│   "
	if last {
		branch, next = "└── ", prefix+"    "
	}
	b.WriteString(prefix + branch)
	describe(n, refs[n], b)
	b.WriteString("\n")
	for i, c := range n.Children {
		renderBranch(c, next, i == len(n.Children)-1, refs, b)
	}
}

func describe(n *Node, ref string, b *strings.Builder) {
	b.WriteString(ShortClass(n.Class))
	if ref != "" {
		b.WriteString(" " + ref)
	}
	if n.Text != "" {
		fmt.Fprintf(b, " text=%q", n.Text)
	}
	if n.ContentDesc != "" {
		fmt.Fprintf(b, " desc=%q", n.ContentDesc)
	}
	if n.ResourceID != "" {
		fmt.Fprintf(b, " id=%s", n.ResourceID)
	}
	fmt.Fprintf(b, " bounds=%d,%d %dx%d", n.Bounds[0], n.Bounds[1], n.Bounds[2], n.Bounds[3])
}

// ShortClass strips the Java package from a view class name, so renders read
// "Button" instead of "android.widget.Button".
func ShortClass(class string) string {
	if i := strings.LastIndex(class, "."); i >= 0 {
		return class[i+1:]
	}
	return class
}
