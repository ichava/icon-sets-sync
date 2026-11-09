package sinks

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNeedsReview is what a guard returns when a refresh would take shipped
// icons away: the run stops for a person to decide, which is not a failure of
// the tool. errors.Is(err, ErrNeedsReview) holds for every *ReviewError.
var ErrNeedsReview = errors.New("needs review")

// Removal is one icon the pack ships today that a refresh would take away.
// RenamedTo is the incoming path of a byte-identical file, when there is one:
// upstream moved or renamed the icon rather than retiring it.
type Removal struct {
	Path      string `json:"path"`
	RenamedTo string `json:"renamed_to,omitempty"`
	// SameAs is set instead when the same bytes stay at a path that ships
	// today too: a duplicate dropped, not a rename (OpenMoji's black flags
	// share one outline, so content alone cannot say which one moved).
	SameAs string `json:"same_as,omitempty"`
}

// ReviewError is a guard's refusal, with every icon it concerns.
type ReviewError struct {
	Target   string
	Removals []Removal
	// Reason is set by a guard that refuses on a count rather than per icon
	// (MinRetention); Removals may then be empty.
	Reason string
}

func (e *ReviewError) Is(target error) bool { return target == ErrNeedsReview }

// Renamed and Removed count the two kinds.
func (e *ReviewError) Renamed() int {
	n := 0
	for _, r := range e.Removals {
		if r.RenamedTo != "" {
			n++
		}
	}
	return n
}

func (e *ReviewError) Removed() int { return len(e.Removals) - e.Renamed() }

func (e *ReviewError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	var b strings.Builder
	fmt.Fprintf(&b, "refusing to change %d icon(s) %s ships today (%d renamed, %d removed): ",
		len(e.Removals), e.Target, e.Renamed(), e.Removed())
	parts := make([]string, 0, len(e.Removals))
	for _, r := range e.Removals {
		switch {
		case r.RenamedTo != "":
			parts = append(parts, r.Path+" -> "+r.RenamedTo)
		case r.SameAs != "":
			parts = append(parts, r.Path+" (same bytes stay at "+r.SameAs+")")
		default:
			parts = append(parts, r.Path)
		}
	}
	b.WriteString(strings.Join(parts, ", "))
	b.WriteString("; a renamed icon has a byte-identical file at its new path, a removed one has none. " +
		"Consumers address icons by path, so check, then pass --force")
	return b.String()
}

// metric is the shape stored under Metrics["review"].
func (e *ReviewError) metric() map[string]any {
	return map[string]any{
		"target": e.Target, "reason": e.Reason, "message": e.Error(),
		"renamed": e.Renamed(), "removed": e.Removed(), "removals": e.Removals,
	}
}

// Review is a dry run's record of what a real run would refuse.
type Review struct {
	Target   string    `json:"target"`
	Reason   string    `json:"reason,omitempty"`
	Message  string    `json:"message"`
	Removals []Removal `json:"removals,omitempty"`
}

// ReviewOf reads the review a Filesystem sink recorded in a pipeline's
// metrics, if any.
func ReviewOf(metrics map[string]any) (Review, bool) {
	m, ok := metrics["review"].(map[string]any)
	if !ok {
		return Review{}, false
	}
	r := Review{}
	r.Target, _ = m["target"].(string)
	r.Reason, _ = m["reason"].(string)
	r.Message, _ = m["message"].(string)
	r.Removals, _ = m["removals"].([]Removal)
	return r, true
}

// removals lists every path in existing with no counterpart in incoming,
// sorted. A rename is named only when it is unambiguous: exactly one path
// new to incoming holds the same bytes. Otherwise, when those bytes stay at a
// path that ships today too, that path is named as SameAs.
func removals(existing, incoming map[string][32]byte) []Removal {
	newPaths := map[[32]byte][]string{}
	keptPaths := map[[32]byte][]string{}
	for p, sum := range incoming {
		if _, before := existing[p]; before {
			keptPaths[sum] = append(keptPaths[sum], p)
		} else {
			newPaths[sum] = append(newPaths[sum], p)
		}
	}
	var out []Removal
	for p, sum := range existing {
		if _, ok := incoming[p]; ok {
			continue
		}
		r := Removal{Path: p}
		switch cands := newPaths[sum]; {
		case len(cands) == 1:
			r.RenamedTo = cands[0]
		case len(keptPaths[sum]) > 0:
			kept := append([]string(nil), keptPaths[sum]...)
			sort.Strings(kept)
			r.SameAs = kept[0]
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
