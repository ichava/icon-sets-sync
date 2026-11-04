package text

import "testing"

func TestOrDefault(t *testing.T) {
	cases := []struct{ v, fallback, want string }{
		{"", "x", "x"},
		{"set", "x", "set"},
		{" ", "x", " "},
	}
	for _, c := range cases {
		if got := OrDefault(c.v, c.fallback); got != c.want {
			t.Errorf("OrDefault(%q, %q) = %q, want %q", c.v, c.fallback, got, c.want)
		}
	}
	if OrDash("") != "-" || OrDash("a") != "a" {
		t.Error("OrDash does not fall back to a dash")
	}
}
