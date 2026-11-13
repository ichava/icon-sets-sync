package confighistory

import (
	"strings"
	"testing"
)

// Hunk headers count lines correctly, so `patch` would accept the output.
func TestUnifiedHunkHeaders(t *testing.T) {
	a := strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"}, "\n") + "\n"
	b := strings.Replace(a, "2\n", "two\n", 1)
	b = strings.Replace(b, "11\n", "eleven\n", 1)
	d := Unified("a", "b", a, b)
	if !strings.Contains(d, "@@ -1,5 +1,5 @@") || !strings.Contains(d, "@@ -8,5 +8,5 @@") {
		t.Errorf("hunks:\n%s", d)
	}
}
