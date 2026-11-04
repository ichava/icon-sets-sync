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
