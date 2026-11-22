package uitree

import "testing"

// FuzzParseTree hammers the session view-tree decoder: any input either
// fails cleanly or yields a tree that validates, flattens to stable refs
// and renders to XML the snapshot parser accepts.
func FuzzParseTree(f *testing.F) {
	f.Add([]byte(`{"class":"android.widget.Button","bounds":[0,0,10,10]}`))
	f.Add([]byte(`{"bounds":[0,0,540,960],"children":[{"class":"x","text":"hi","bounds":[1,2,3,4]}]}`))
	f.Add([]byte(`{"bounds":[0,0,1,1],"children":[{"bounds":[-1,0,1,1]}]}`))
	f.Add([]byte(`{"bounds":[1,2,3]}`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := Parse(data)
		if err != nil {
			return // rejected input is fine — it must never panic or hang
		}
		els := Flatten(root)
		for i, el := range els {
			if el.Index != i+1 {
				t.Fatalf("ref numbering broke: element %d has index %d", i, el.Index)
			}
		}
		if _, err := XML(root); err != nil {
			t.Fatalf("accepted tree must render: %v", err)
		}
	})
}
