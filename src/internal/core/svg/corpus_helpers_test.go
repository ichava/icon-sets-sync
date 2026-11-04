package svg

// Helpers shared by the corpus tests. They lived in corpus_test.go, beside the
// live lxml cross-check that was removed with the Python implementation.

import (
	"path/filepath"
	"sort"
	"testing"
)

// packsRoot locates the sibling pack checkouts. The toolkit sits beside them.
func packsRoot(t *testing.T) string {
	t.Helper()
	// From src/internal/core/svg -> up five is the directory holding the
	// toolkit checkout and its sibling packs.
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func dedupeSorted(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, i := range items {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// diffFirst returns up to five entries in a that are absent from b, which is
