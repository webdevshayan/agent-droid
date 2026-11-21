package tests_test

import (
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/snapshot"
)

const sampleXML = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy rotation="0">
  <node index="0" text="" resource-id="" class="android.widget.FrameLayout" package="com.example" content-desc="" clickable="false" enabled="true" scrollable="false" bounds="[0,0][1080,2400]">
    <node index="0" text="Settings" resource-id="com.example:id/title" class="android.widget.TextView" clickable="false" bounds="[54,100][1026,180]"/>
    <node index="1" text="" resource-id="" class="android.widget.Button" content-desc="Back" clickable="true" enabled="false" bounds="[0,0][54,100]"/>
  </node>
</hierarchy>`

func TestParseGolden(t *testing.T) {
	els, err := snapshot.Parse([]byte(sampleXML))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(els) != 3 {
		t.Fatalf("got %d elements, want 3", len(els))
	}
	e0 := els[0]
	if e0.Ref != "@e1" || e0.Class != "android.widget.FrameLayout" || e0.Package != "com.example" {
		t.Fatalf("element 0 wrong: %+v", e0)
	}
	if e0.Bounds != (snapshot.Rect{X1: 0, Y1: 0, X2: 1080, Y2: 2400}) {
		t.Fatalf("element 0 bounds wrong: %+v", e0.Bounds)
	}
	e1 := els[1]
	if e1.Ref != "@e2" || e1.Text != "Settings" || e1.ResourceID != "com.example:id/title" {
		t.Fatalf("element 1 wrong: %+v", e1)
	}
	if x, y := e1.Bounds.Center(); x != 540 || y != 140 {
		t.Fatalf("element 1 center = (%d,%d), want (540,140)", x, y)
	}
	e2 := els[2]
	if e2.Ref != "@e3" || !e2.Clickable || e2.Enabled {
		t.Fatalf("element 2 wrong: %+v", e2)
	}
}

func TestParseFormat(t *testing.T) {
	els, err := snapshot.Parse([]byte(sampleXML))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	out := snapshot.Format(els)
	for _, want := range []string{
		"@e1  android.widget.FrameLayout",
		`@e2  android.widget.TextView  text="Settings"  id=com.example:id/title`,
		"@e3  android.widget.Button  desc=\"Back\"  clickable  disabled",
		"center=(540,1200)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Format() missing %q in:\n%s", want, out)
		}
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("Format() produced %d lines, want 3:\n%s", len(lines), out)
	}
}

func TestParseSkipsBadBoundsButWalksChildren(t *testing.T) {
	xml := `<hierarchy><node class="A" bounds="garbage"><node class="B" bounds="[0,0][10,10]"/></node></hierarchy>`
	els, err := snapshot.Parse([]byte(xml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(els) != 1 || els[0].Class != "B" || els[0].Ref != "@e1" {
		t.Fatalf("want only child B with ref @e1, got %+v", els)
	}
}

func TestParseEdgeInputs(t *testing.T) {
	t.Run("not xml", func(t *testing.T) {
		_, err := snapshot.Parse([]byte("hello, this is not xml"))
		if err == nil || !strings.Contains(err.Error(), "not XML") {
			t.Fatalf("want not-XML error, got %v", err)
		}
	})
	t.Run("wrong root element", func(t *testing.T) {
		_, err := snapshot.Parse([]byte("<foo/>"))
		if err == nil || !strings.Contains(err.Error(), "want <hierarchy>") {
			t.Fatalf("wrong root must be a clean error, got %v", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		_, err := snapshot.Parse(nil)
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("want empty error, got %v", err)
		}
	})
	t.Run("empty hierarchy", func(t *testing.T) {
		els, err := snapshot.Parse([]byte("<hierarchy/>"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(els) != 0 {
			t.Fatalf("want 0 elements, got %d", len(els))
		}
	})
	t.Run("bom prefix", func(t *testing.T) {
		els, err := snapshot.Parse(append([]byte{0xEF, 0xBB, 0xBF}, []byte(`<hierarchy><node class="A" bounds="[0,0][1,1]"/></hierarchy>`)...))
		if err != nil || len(els) != 1 {
			t.Fatalf("BOM-prefixed XML must parse, got els=%d err=%v", len(els), err)
		}
	})
	t.Run("negative bounds skipped", func(t *testing.T) {
		els, err := snapshot.Parse([]byte(`<hierarchy><node bounds="[-1,0][10,10]"/><node bounds="[0,0][10,10]"/></hierarchy>`))
		if err != nil || len(els) != 1 || els[0].Ref != "@e1" {
			t.Fatalf("negative-bounds node must be skipped, got els=%d err=%v", len(els), err)
		}
	})
	t.Run("overflow bounds skipped", func(t *testing.T) {
		els, err := snapshot.Parse([]byte(`<hierarchy><node bounds="[999999999999999,0][10,10]"/><node bounds="[0,0][10,10]"/></hierarchy>`))
		if err != nil || len(els) != 1 {
			t.Fatalf("overflow-bounds node must be skipped without panic, got els=%d err=%v", len(els), err)
		}
	})
	t.Run("zero area is valid", func(t *testing.T) {
		els, err := snapshot.Parse([]byte(`<hierarchy><node bounds="[5,5][5,5]"/></hierarchy>`))
		if err != nil || len(els) != 1 {
			t.Fatalf("zero-area rect is a valid tap point, got els=%d err=%v", len(els), err)
		}
	})
	t.Run("undefined entity rejected", func(t *testing.T) {
		_, err := snapshot.Parse([]byte(`<hierarchy><node text="&bogus;" bounds="[0,0][1,1]"/></hierarchy>`))
		if err == nil {
			t.Fatal("strict parser must reject undefined entities")
		}
	})
}

func TestParseRefTable(t *testing.T) {
	for ref, want := range map[string]int{
		"@e1":         1,
		"@e9":         9,
		"@e10":        10,
		"@e123456789": 123456789,
		"@e999999999": 999999999,
	} {
		got, err := snapshot.ParseRef(ref)
		if err != nil || got != want {
			t.Fatalf("ParseRef(%q) = %d, %v; want %d, nil", ref, got, err, want)
		}
	}
	for _, ref := range []string{
		"", "@", "@e", "e1", "@E1", "@e0", "@e01", "@e1x", "@e1.5", "@e-1",
		"@e+1", "@e 1", "@ e1", "@e①", "@e1000000000", "@e99999999999999999999",
		"@\ne1", "@e1\n", "  @e1", "@e1 ",
	} {
		if n, err := snapshot.ParseRef(ref); err == nil {
			t.Fatalf("ParseRef(%q) = %d, must be an error", ref, n)
		}
	}
}

func TestResolve(t *testing.T) {
	els, err := snapshot.Parse([]byte(sampleXML))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	el, err := snapshot.Resolve(els, "@e2")
	if err != nil || el.Text != "Settings" {
		t.Fatalf("Resolve(@e2) = %+v, %v", el, err)
	}
	if _, err := snapshot.Resolve(els, "@e4"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Resolve(@e4) must report not found, got %v", err)
	}
	if _, err := snapshot.Resolve(els, "@e0"); err == nil {
		t.Fatal("Resolve(@e0) must fail")
	}
	if _, err := snapshot.Resolve(nil, "@e1"); err == nil || !strings.Contains(err.Error(), "0 tappable") {
		t.Fatalf("Resolve on empty list must mention 0 tappable elements, got %v", err)
	}
}
