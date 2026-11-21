package snapshot

import (
	"fmt"
	"strings"
)

// ParseRef validates an element ref and returns its 1-based index.
//
// Refs are strictly "@e" + digits (no leading zeros, at most 9 digits), so
// the index can never overflow an int on any platform and hostile strings
// like "@e99999999999999999999" fail cleanly instead of wrapping around.
func ParseRef(s string) (int, error) {
	if len(s) < 3 || s[0] != '@' || s[1] != 'e' {
		return 0, fmt.Errorf("invalid element ref %q: want @eN (e.g. @e1)", s)
	}
	digits := s[2:]
	if len(digits) > 9 {
		return 0, fmt.Errorf("invalid element ref %q: index too large", s)
	}
	n := 0
	for i := 0; i < len(digits); i++ {
		d := digits[i]
		if d < '0' || d > '9' {
			return 0, fmt.Errorf("invalid element ref %q: want @eN (e.g. @e1)", s)
		}
		n = n*10 + int(d-'0')
	}
	if digits[0] == '0' {
		return 0, fmt.Errorf("invalid element ref %q: index must start at 1", s)
	}
	return n, nil
}

// AssignRefs numbers the elements in document order: @e1..@eN.
func AssignRefs(elements []Element) {
	for i := range elements {
		elements[i].Ref = fmt.Sprintf("@e%d", i+1)
	}
}

// Resolve maps a ref like "@e3" onto the parsed element list.
func Resolve(elements []Element, ref string) (Element, error) {
	n, err := ParseRef(ref)
	if err != nil {
		return Element{}, err
	}
	if n > len(elements) {
		return Element{}, fmt.Errorf("element %s not found: last snapshot has %d tappable element(s); run `agentdroid snapshot` to refresh", ref, len(elements))
	}
	return elements[n-1], nil
}

// Format renders elements as one stable line per ref — the contract agents
// parse. Empty fields are omitted; flags only appear when true.
func Format(elements []Element) string {
	var b strings.Builder
	for _, e := range elements {
		b.WriteString(e.Ref)
		b.WriteString("  ")
		b.WriteString(e.Class)
		if e.Text != "" {
			fmt.Fprintf(&b, "  text=%q", e.Text)
		}
		if e.ContentDesc != "" {
			fmt.Fprintf(&b, "  desc=%q", e.ContentDesc)
		}
		if e.ResourceID != "" {
			fmt.Fprintf(&b, "  id=%s", e.ResourceID)
		}
		if e.Package != "" {
			fmt.Fprintf(&b, "  pkg=%s", e.Package)
		}
		if e.Clickable {
			b.WriteString("  clickable")
		}
		if e.Scrollable {
			b.WriteString("  scrollable")
		}
		if !e.Enabled {
			b.WriteString("  disabled")
		}
		fmt.Fprintf(&b, "  bounds=[%d,%d][%d,%d]", e.Bounds.X1, e.Bounds.Y1, e.Bounds.X2, e.Bounds.Y2)
		cx, cy := e.Bounds.Center()
		fmt.Fprintf(&b, "  center=(%d,%d)\n", cx, cy)
	}
	return b.String()
}
