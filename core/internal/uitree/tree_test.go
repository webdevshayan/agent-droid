package uitree

import (
	"strings"
	"testing"
)

const sampleTree = `{
  "class": "android.widget.FrameLayout",
  "bounds": [0, 0, 540, 960],
  "children": [
    {"class": "android.widget.TextView", "text": "AgentDroid Demo", "bounds": [20, 40, 500, 48]},
    {"class": "android.widget.EditText", "text": "", "content_desc": "Email",
     "resource_id": "demo:id/email", "bounds": [20, 150, 500, 56], "focusable": true},
    {"class": "android.view.ViewGroup", "bounds": [20, 220, 500, 56], "children": [
      {"class": "android.widget.Button", "text": "Sign In", "resource_id": "demo:id/sign_in",
       "bounds": [20, 420, 500, 56], "clickable": true}
    ]},
    {"class": "android.widget.TextView", "text": "Ready.", "resource_id": "demo:id/status",
     "bounds": [20, 340, 500, 40]}
  ]
}`

func TestParseAndFlatten(t *testing.T) {
	root, err := Parse([]byte(sampleTree))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	els := Flatten(root)
	// Title, email field, button (the unlabeled ViewGroup wrapper is
	// skipped), status line.
	if len(els) != 4 {
		t.Fatalf("expected 4 elements, got %d: %+v", len(els), els)
	}
	if els[0].Ref != "@e1" || els[0].Node.Text != "AgentDroid Demo" {
		t.Fatalf("unexpected first element: %+v", els[0])
	}
	if els[1].Ref != "@e2" || els[1].Node.ResourceID != "demo:id/email" {
		t.Fatalf("unexpected second element: %+v", els[1])
	}
	if els[2].Ref != "@e3" || els[2].Node.Text != "Sign In" || els[2].Path != "root/children[2]/children[0]" {
		t.Fatalf("unexpected third element: %+v", els[2])
	}
	if els[3].Ref != "@e4" || els[3].Node.Text != "Ready." {
		t.Fatalf("unexpected fourth element: %+v", els[3])
	}
}

func TestResolve(t *testing.T) {
	root, _ := Parse([]byte(sampleTree))
	els := Flatten(root)

	el, err := Resolve(els, "@e3")
	if err != nil || el.Node.Text != "Sign In" {
		t.Fatalf("resolve @e3: %v %+v", err, el)
	}

	if _, err := Resolve(els, "@e9"); err == nil || !strings.Contains(err.Error(), "unknown element @e9") {
		t.Fatalf("expected out-of-range error, got %v", err)
	}
	if _, err := Resolve(els, "e3"); err == nil || !strings.Contains(err.Error(), "invalid element reference") {
		t.Fatalf("expected invalid-ref error, got %v", err)
	}
	if _, err := Resolve(els, "@e0"); err == nil {
		t.Fatal("expected @e0 to be rejected")
	}
}

func TestCenter(t *testing.T) {
	n := &Node{Bounds: Bounds{100, 200, 80, 40}}
	x, y := n.Center()
	if x != 140 || y != 220 {
		t.Fatalf("center: got (%d,%d), want (140,220)", x, y)
	}
}

func TestParseRejectsBadGeometry(t *testing.T) {
	if _, err := Parse([]byte(`{"class":"X","bounds":[0,0,-5,10]}`)); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("expected negative-size error at root, got %v", err)
	}
	nested := `{"class":"X","bounds":[0,0,10,10],"children":[{"class":"Y","bounds":[-1,0,10,10]}]}`
	if _, err := Parse([]byte(nested)); err == nil || !strings.Contains(err.Error(), "children[0]") {
		t.Fatalf("expected error carrying the child path, got %v", err)
	}
}

func TestParseRejectsTruncatedBounds(t *testing.T) {
	if _, err := Parse([]byte(`{"class":"X","bounds":[1,2,3]}`)); err == nil || !strings.Contains(err.Error(), "4 numbers") {
		t.Fatalf("expected bounds-length error, got %v", err)
	}
}

func TestParseRejectsOverlongBounds(t *testing.T) {
	if _, err := Parse([]byte(`{"class":"X","bounds":[1,2,3,4,5]}`)); err == nil || !strings.Contains(err.Error(), "4 numbers") {
		t.Fatalf("expected bounds-length error, got %v", err)
	}
}

func TestParseRejectsFractionalBounds(t *testing.T) {
	if _, err := Parse([]byte(`{"class":"X","bounds":[1.5,2,3,4]}`)); err == nil {
		t.Fatalf("expected non-integer bounds error, got %v", err)
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	if _, err := Parse([]byte(`{"class":`)); err == nil {
		t.Fatal("expected json error")
	}
}

func TestUnlabeledTreeFlattensToNothing(t *testing.T) {
	root, err := Parse([]byte(`{"class":"android.widget.FrameLayout","bounds":[0,0,10,10]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if els := Flatten(root); len(els) != 0 {
		t.Fatalf("expected 0 elements, got %d", len(els))
	}
}

func TestValidRef(t *testing.T) {
	for _, ok := range []string{"@e1", "@e12", "@e999"} {
		if !ValidRef(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"@E1", "@e", "e1", "@e1x", "", "@e01"} {
		if ValidRef(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}
