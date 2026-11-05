package svg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// testdata/presentation-vectors.json is the vector table every sanitiser in
// the ecosystem runs: byte-identical here and in core, react-icon-browser and
// js-icon-components. TestPresentationAttributeValueGuards is where its first
// 27 rows came from and stays as it was; this test runs the whole shared file,
// including the marker rows added when <marker> was allowed after the US and UM
// flags shipped without their stars.

type sharedVectors struct {
	Cases []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Keep  bool   `json:"keep"`
	} `json:"cases"`
	Documents []struct {
		Label  string `json:"label"`
		SVG    string `json:"svg"`
		Clean  bool   `json:"clean"`
		Expect []struct {
			Element   string `json:"element"`
			Attribute string `json:"attribute"`
			Present   bool   `json:"present"`
		} `json:"expect"`
	} `json:"documents"`
}

func loadSharedVectors(t *testing.T) sharedVectors {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "presentation-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v sharedVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	// A table that stopped loading, or lost rows, must not pass.
	if len(v.Cases) != 35 || len(v.Documents) != 6 {
		t.Fatalf("shared table has %d cases and %d documents, want 35 and 6", len(v.Cases), len(v.Documents))
	}
	return v
}

func TestSharedVectorCases(t *testing.T) {
	p := MustLoad()
	for _, c := range loadSharedVectors(t).Cases {
		if got := p.AttributeAllowed(c.Name, c.Value, false); got != c.Keep {
			t.Errorf("AttributeAllowed(%q, %q) = %v, want %v", c.Name, c.Value, got, c.Keep)
		}
	}
}

func TestSharedVectorDocuments(t *testing.T) {
	for _, d := range loadSharedVectors(t).Documents {
		out, removed, err := SanitiseBytes([]byte(d.SVG), false)
		if err != nil {
			t.Fatalf("%s: %v", d.Label, err)
		}
		if clean := len(removed) == 0; clean != d.Clean {
			t.Errorf("%s: removed %v, want clean=%v\n%s", d.Label, removed, d.Clean, out)
		}

		root, err := Parse(out, false)
		if err != nil {
			t.Fatalf("%s: output does not parse: %v", d.Label, err)
		}
		for _, e := range d.Expect {
			if got := hasElementWith(root, e.Element, e.Attribute); got != e.Present {
				t.Errorf("%s: <%s> %s present = %v, want %v\n%s", d.Label, e.Element, e.Attribute, got, e.Present, out)
			}
		}
	}
}

// hasElementWith reports whether any element named local exists, or, with an
// attribute name, whether any such element carries it.
func hasElementWith(n *Node, local, attr string) bool {
	if n.Kind != ElementNode {
		return false
	}
	if n.Local == local {
		if attr == "" {
			return true
		}
		for _, a := range n.Attrs {
			if !a.IsNamespaceDecl && a.Name() == attr {
				return true
			}
		}
	}
	for _, c := range n.Children {
		if hasElementWith(c, local, attr) {
			return true
		}
	}
	return false
}
