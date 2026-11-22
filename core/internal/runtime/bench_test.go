package runtime

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

// bigTreeJSON builds a synthetic hierarchy with n nodes (a 20-wide root,
// each child fanning out n/20 leaves) for parser benchmarks.
func bigTreeJSON(n int) []byte {
	root := &uitree.Node{Class: "android.widget.FrameLayout", Bounds: [4]int{0, 0, 540, 960}}
	w := 20
	for i := 0; i < w && len(flattenCount(root)) < n; i++ {
		child := &uitree.Node{
			Class: "android.widget.LinearLayout", Bounds: [4]int{0, i * 48, 540, 48},
		}
		root.Children = append(root.Children, child)
		for j := 0; j < n/w; j++ {
			child.Children = append(child.Children, &uitree.Node{
				Class:      "android.widget.TextView",
				Text:       "row item",
				ResourceID: "demo:id/row",
				Bounds:     [4]int{0, i*48 + j, 540, 48},
			})
		}
	}
	b, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return b
}

func flattenCount(root *uitree.Node) []*uitree.Element { return uitree.Flatten(root) }

func BenchmarkParseTree(b *testing.B) {
	data := bigTreeJSON(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root, err := uitree.Parse(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(root.Children) == 0 {
			b.Fatal("empty tree")
		}
	}
}

func BenchmarkRenderXML(b *testing.B) {
	data := bigTreeJSON(2000)
	root, err := uitree.Parse(data)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := uitree.XML(root)
		if err != nil || len(out) == 0 {
			b.Fatal("empty render")
		}
	}
}

// writeApk creates a plausible APK path for fake-backend benchmarks.
func writeApk(b *testing.B) string {
	b.Helper()
	p := filepath.Join(b.TempDir(), "bench.apk")
	if err := os.WriteFile(p, []byte("PK\x03\x04 bench payload"), 0o644); err != nil {
		b.Fatal(err)
	}
	return p
}

func BenchmarkProtocolRoundtrip(b *testing.B) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go func() { _ = ServeConn(serverConn, serverConn, NewFakeBackend("")) }()
	c := &Client{conn: clientConn, br: protocol.NewReader(clientConn)}
	req := protocol.Request{Cmd: protocol.CmdLaunch, APK: writeApk(b)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := c.Call(req, time.Second)
		if err != nil || !resp.OK {
			b.Fatalf("call: %v %+v", err, resp)
		}
	}
}

func BenchmarkFakeScreenshot(b *testing.B) {
	f := NewFakeBackend("")
	if resp := f.Launch(writeApk(b)); !resp.OK {
		b.Fatal(resp.Error)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := f.Screenshot()
		if !resp.OK || len(resp.PNG) == 0 {
			b.Fatal(resp.Error)
		}
	}
}
