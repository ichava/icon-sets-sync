package config

import (
	"math"
	"testing"
)

// TestSpliceSizeRefusesOverflow is go/allocation-size-overflow: the splice's
// capacity is len(data)-(end-start)+len(encoded), and an addition that wraps
// would allocate a slice far smaller than what is appended into it.
func TestSpliceSizeRefusesOverflow(t *testing.T) {
	for _, tc := range []struct{ total, start, end, insert int }{
		{math.MaxInt, 0, 0, 1},
		{math.MaxInt - 1, 0, 0, 5},
		{1, 0, 0, math.MaxInt},
		{maxSpliceBytes + 1, 0, 0, 0},
		{0, 0, 0, maxSpliceBytes + 1},
		{10, 5, 4, 0},
		{10, 0, 11, 0},
		{10, -1, 2, 0},
		{10, 0, 2, -1},
	} {
		if n, err := spliceSize(tc.total, tc.start, tc.end, tc.insert); err == nil {
			t.Errorf("spliceSize(%d, %d, %d, %d) = %d, want refused", tc.total, tc.start, tc.end, tc.insert, n)
		}
	}
	n, err := spliceSize(10, 2, 5, 4)
	if err != nil || n != 11 {
		t.Errorf("spliceSize(10, 2, 5, 4) = %d, %v; want 11", n, err)
	}
}
