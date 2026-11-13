// Package text holds the small string helpers several packages used to write
// for themselves (G-04).
package text

// OrDefault returns v, or fallback when v is empty.
func OrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// OrDash returns s, or "-" when s is empty: how a table shows a missing value.
func OrDash(s string) string { return OrDefault(s, "-") }
