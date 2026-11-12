package app

import "testing"

// The worker pool is sized from a value an API request can set, so it is bounded.
func TestWorkerCountIsBounded(t *testing.T) {
	for _, c := range []struct{ requested, packs, want int }{
		{0, 5, 1},
		{-3, 5, 1},
		{4, 10, 4},
		{20, 3, 3},
		{1 << 30, 1000, MaxConcurrency},
		{1 << 30, 0, 1},
	} {
		if got := workerCount(c.requested, c.packs); got != c.want {
			t.Errorf("workerCount(%d, %d) = %d, want %d", c.requested, c.packs, got, c.want)
		}
	}
}
