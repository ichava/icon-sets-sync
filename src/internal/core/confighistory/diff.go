package confighistory

import (
	"fmt"
	"strings"
)

// maxDiffLines bounds the quadratic diff below. A config file is a few
// hundred lines; anything past this is reported as a whole-file change.
const maxDiffLines = 4000

// Unified returns a unified diff of a against b, with three lines of context,
// or "" when they are equal. Config files are small, so a plain LCS is fine.
func Unified(nameA, nameB, a, b string) string {
	if a == b {
		return ""
	}
	la, lb := splitLines(a), splitLines(b)
	var ops []op
	if len(la) > maxDiffLines || len(lb) > maxDiffLines {
		for _, l := range la {
			ops = append(ops, op{'-', l})
		}
		for _, l := range lb {
			ops = append(ops, op{'+', l})
		}
	} else {
		ops = lcsOps(la, lb)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", nameA, nameB)
	const ctx = 3
	// Group the ops into hunks around the changes.
	i := 0
	ai, bi := 0, 0
	for i < len(ops) {
		if ops[i].kind == ' ' {
			i++
			ai++
			bi++
			continue
		}
		start := i
		for k := 0; k < ctx && start > 0 && ops[start-1].kind == ' '; k++ {
			start--
		}
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run-end > 2*ctx || run == len(ops) {
				end += min(ctx, run-end)
				break
			}
			end = run
		}
		// Line numbers of the hunk's first line in a and b.
		hA, hB := ai-(i-start), bi-(i-start)
		var nA, nB int
		var body strings.Builder
		for _, o := range ops[start:end] {
			body.WriteByte(o.kind)
			body.WriteString(o.text)
			body.WriteByte('\n')
			if o.kind != '+' {
				nA++
			}
			if o.kind != '-' {
				nB++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n%s", hA+1, nA, hB+1, nB, body.String())
		for _, o := range ops[i:end] {
			if o.kind != '+' {
				ai++
			}
			if o.kind != '-' {
				bi++
			}
		}
		i = end
	}
	return out.String()
}

type op struct {
	kind byte
	text string
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func lcsOps(a, b []string) []op {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	return ops
}
