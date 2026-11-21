package tests_test

import (
	"fmt"
	"testing"

	"github.com/webdevshayan/agent-droid/core/snapshot"
)

// FuzzParseXML throws arbitrary bytes at the snapshot parser.
// Invariants: Parse never panics; on success every element carries a valid
// bounds rect, refs are exactly @e1..@eN in order and unique, every emitted
// ref resolves against the same list, and the element cap holds.
func FuzzParseXML(f *testing.F) {
	for _, s := range []string{
		sampleXML,
		`<hierarchy><node bounds="[0,0][10,10]"/></hierarchy>`,
		`<hierarchy><node bounds="garbage"><node bounds="[0,0][1,1]" clickable="true"/></node></hierarchy>`,
		`<?xml version="1.0"?><hierarchy><node text="&amp;" bounds="[0,0][1,1]"/></hierarchy>`,
		`<hierarchy><node text="` + strings_Repeat(4096) + `" bounds="[0,0][1,1]"/></hierarchy>`,
		`<hierarchy><node bounds="[0,0][1,1]"><node bounds="[0,0][1,1]"><node bounds="[0,0][1,1]"/></node></node></hierarchy>`,
		`hello`,
		`<hierarchy>`,
		`<node/>`,
		`<hierarchy><node bounds="[0,0][1,1]" clickable="weird" enabled="weird"/></hierarchy>`,
		"&#x0;",
		"\xef\xbb\xbf<hierarchy><node bounds=\"[0,0][1,1]\"/></hierarchy>",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > snapshot.MaxSnapshotBytes {
			t.Skip() // the size-guard path is covered by the memory tests
		}
		els, err := snapshot.Parse(data)
		if err != nil {
			return
		}
		if len(els) > snapshot.MaxElements {
			t.Fatalf("element cap breached: %d > %d", len(els), snapshot.MaxElements)
		}
		for i, el := range els {
			want := fmt.Sprintf("@e%d", i+1)
			if el.Ref != want {
				t.Fatalf("ref mismatch at %d: got %q want %q", i, el.Ref, want)
			}
			if !el.Bounds.Valid() {
				t.Fatalf("Parse emitted invalid bounds %+v", el.Bounds)
			}
			if _, err := snapshot.Resolve(els, el.Ref); err != nil {
				t.Fatalf("own ref %s unresolvable: %v", el.Ref, err)
			}
		}
	})
}

// FuzzParseRef asserts the ref parser never panics and never returns an
// out-of-range index for arbitrary strings.
func FuzzParseRef(f *testing.F) {
	for _, s := range []string{
		"@e1", "@e999999999", "@e0", "@e01", "@e1000000000", "", "@", "e",
		"@e99999999999999999999", "@e-1", "@e+1", "@ex", "@E1", "@e1.5",
		"@e18446744073709551617", "@e\uFFFD",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := snapshot.ParseRef(s)
		if err != nil {
			return
		}
		if n < 1 || n > 999999999 {
			t.Fatalf("ParseRef(%q) = %d, outside 1..999999999", s, n)
		}
	})
}

func strings_Repeat(n int) string {
	return string(make([]byte, n))
}
