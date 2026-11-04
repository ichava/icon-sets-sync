package svg

import (
	"strings"
	"testing"
)

func TestDefaultCapsRefuseOversizedInput(t *testing.T) {
	tooManyNodes := `<svg xmlns="http://www.w3.org/2000/svg">` +
		strings.Repeat(`<g/>`, 60000) + `</svg>`
	tooManyBytes := `<svg xmlns="http://www.w3.org/2000/svg"><title>` +
		strings.Repeat("a", 9<<20) + `</title></svg>`

	for name, doc := range map[string]string{"nodes": tooManyNodes, "bytes": tooManyBytes} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := SanitiseBytes([]byte(doc), false); err == nil {
				t.Fatal("oversized document was accepted")
			}
		})
	}

	// Positive control: an ordinary document is unaffected.
	ok := `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`
	if _, _, err := SanitiseBytes([]byte(ok), false); err != nil {
		t.Fatalf("ordinary document refused: %v", err)
	}
}

func TestCapsAreFieldsOnThePolicy(t *testing.T) {
	doc := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g/><g/><g/></svg>`) // 4 elements

	for _, tc := range []struct {
		name     string
		bytes    int
		nodes    int
		wantFail bool
	}{
		{"at the node cap", 0, 4, false},
		{"one over the node cap", 0, 3, true},
		{"at the byte cap", len(doc), 0, false},
		{"one over the byte cap", len(doc) - 1, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := *MustLoad() // a copy: the loaded policy is shared
			p.MaxBytes, p.MaxNodes = tc.bytes, tc.nodes
			_, _, err := p.Sanitise(doc, false)
			if (err != nil) != tc.wantFail {
				t.Fatalf("err = %v, wantFail %v", err, tc.wantFail)
			}
		})
	}

	if p := MustLoad(); p.MaxBytes != DefaultMaxBytes || p.MaxNodes != DefaultMaxNodes {
		t.Errorf("loaded policy caps = %d/%d, want the defaults", p.MaxBytes, p.MaxNodes)
	}
}
