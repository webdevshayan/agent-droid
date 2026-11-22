package uitree

import (
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/snapshot"
)

// The XML renderer must produce dumps the CLI's snapshot parser accepts,
// with refs assigned exactly as for a real uiautomator dump.

func xmlTree() *Node {
	return &Node{
		Class:  "android.widget.FrameLayout",
		Bounds: Bounds{0, 0, 540, 960},
		Children: []*Node{
			{Class: "android.widget.TextView", Text: "AgentDroid Demo", Bounds: Bounds{20, 40, 500, 48}},
			{Class: "android.widget.EditText", ContentDesc: "Email", ResourceID: "demo:id/email",
				Bounds: Bounds{20, 150, 500, 56}, Clickable: true, Focusable: true},
			{Class: "android.widget.EditText", ContentDesc: "Password", ResourceID: "demo:id/password",
				Bounds: Bounds{20, 220, 500, 56}, Clickable: true, Focusable: true},
			{Class: "android.widget.TextView", Text: `Ready & <set>`, ResourceID: "demo:id/status", Bounds: Bounds{20, 340, 500, 40}},
			{Class: "android.widget.Button", Text: "Sign In", ResourceID: "demo:id/sign_in",
				Bounds: Bounds{20, 420, 500, 56}, Clickable: true},
		},
	}
}

func TestXMLRoundTripsThroughSnapshotParse(t *testing.T) {
	raw, err := XML(xmlTree())
	if err != nil {
		t.Fatalf("XML: %v", err)
	}
	els, err := snapshot.Parse(raw)
	if err != nil {
		t.Fatalf("snapshot.Parse rejected a rendered dump: %v\n%s", err, raw)
	}
	if len(els) != 6 { // root + 5 children — every bounded node earns a ref
		t.Fatalf("got %d elements, want 6:\n%s", len(els), raw)
	}
	// Document order, exactly like adb.
	wantIDs := []string{"", "", "demo:id/email", "demo:id/password", "demo:id/status", "demo:id/sign_in"}
	for i, id := range wantIDs {
		if els[i].ResourceID != id {
			t.Fatalf("element %d id = %q, want %q", i+1, els[i].ResourceID, id)
		}
	}
	// Bounds converted [x,y,w,h] -> [x1,y1][x2,y2]; centers tappable.
	x, y := els[2].Bounds.Center()
	if x != 270 || y != 178 {
		t.Fatalf("email center = (%d,%d), want (270,178)", x, y)
	}
	x, y = els[5].Bounds.Center()
	if x != 270 || y != 448 {
		t.Fatalf("button center = (%d,%d), want (270,448)", x, y)
	}
	// Refs are assigned by the shared pipeline.
	if els[0].Ref != "@e1" || els[5].Ref != "@e6" {
		t.Fatalf("refs = %q..%q, want @e1..@e6", els[0].Ref, els[5].Ref)
	}
}

func TestXMLEscapesMetacharacters(t *testing.T) {
	raw, err := XML(xmlTree())
	if err != nil {
		t.Fatalf("XML: %v", err)
	}
	if !strings.Contains(string(raw), "Ready &amp; &lt;set&gt;") {
		t.Fatalf("metacharacters not escaped:\n%s", raw)
	}
	// And the escaped text survives parsing verbatim.
	els, err := snapshot.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if els[4].Text != `Ready & <set>` {
		t.Fatalf("text mangled: %q", els[4].Text)
	}
}

func TestXMLRejectsBrokenTrees(t *testing.T) {
	if _, err := XML(nil); err == nil {
		t.Fatal("nil tree must be rejected")
	}
	neg := &Node{Class: "x", Bounds: Bounds{0, 0, 10, 10}, Children: []*Node{{Bounds: Bounds{-1, 0, 1, 1}}}}
	if _, err := XML(neg); err == nil {
		t.Fatal("negative bounds must be rejected")
	}
	// Deeper than the snapshot parser ever accepts.
	deep := &Node{Bounds: Bounds{0, 0, 1, 1}}
	cur := deep
	for i := 0; i < maxXMLDepth+2; i++ {
		next := &Node{Bounds: Bounds{0, 0, 1, 1}}
		cur.Children = []*Node{next}
		cur = next
	}
	if _, err := XML(deep); err == nil || !strings.Contains(err.Error(), "deeper") {
		t.Fatalf("over-deep tree must be rejected, got %v", err)
	}
}
