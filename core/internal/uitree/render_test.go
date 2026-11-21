package uitree

import (
	"strings"
	"testing"
)

func TestRenderList(t *testing.T) {
	root, err := Parse([]byte(sampleTree))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := RenderList(Flatten(root))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], `@e1  TextView  text="AgentDroid Demo"`) {
		t.Fatalf("line 0: %q", lines[0])
	}
	if !strings.Contains(lines[1], "id=demo:id/email") || !strings.Contains(lines[1], "focusable") {
		t.Fatalf("line 1: %q", lines[1])
	}
	if !strings.Contains(lines[2], "clickable") || !strings.Contains(lines[2], "bounds=20,420 500x56") {
		t.Fatalf("line 2: %q", lines[2])
	}
}

func TestRenderTree(t *testing.T) {
	root, err := Parse([]byte(sampleTree))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := RenderTree(root)
	if !strings.Contains(out, "FrameLayout") {
		t.Fatalf("missing root class:\n%s", out)
	}
	if !strings.Contains(out, "@e3") {
		t.Fatalf("missing inline ref:\n%s", out)
	}
	// Four root children plus one nested branch under the ViewGroup.
	if got := strings.Count(out, "├──") + strings.Count(out, "└──"); got != 5 {
		t.Fatalf("unexpected branch count %d:\n%s", got, out)
	}
}

func TestShortClass(t *testing.T) {
	cases := map[string]string{
		"android.widget.Button": "Button",
		"Button":                "Button",
		"":                      "",
	}
	for in, want := range cases {
		if got := ShortClass(in); got != want {
			t.Fatalf("ShortClass(%q) = %q, want %q", in, got, want)
		}
	}
}
