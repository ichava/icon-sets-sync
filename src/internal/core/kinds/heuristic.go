package kinds

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// The row 7 heuristic (contract section 3). It looks at an SVG's shape and
// suggests that a human reclassify it. It never decides anything: no code path
// here writes a kind anywhere.
//
// These constants are identical in core (IchavaConstants::KIND_HEURISTIC_*).
const (
	// HeuristicAspectMin and HeuristicAspectMax bound the viewBox width/height
	// ratio an icon is expected to sit inside.
	HeuristicAspectMin = 0.8
	HeuristicAspectMax = 1.25
	// HeuristicPaletteMin is the number of distinct literal colours that fires.
	HeuristicPaletteMin = 4
	// HeuristicBytesMax and HeuristicElementsMax fire when exceeded.
	HeuristicBytesMax    = 20_000
	HeuristicElementsMax = 150
	// HeuristicCanvasMax fires when the larger viewBox side exceeds it.
	HeuristicCanvasMax = 256
	// HeuristicMinSignals is how many signals must fire to suggest.
	HeuristicMinSignals = 3
	// HeuristicGroupMin is how many items a folder needs before its suggestions
	// collapse into one rule (at least 80% of them suggested).
	HeuristicGroupMin = 5
)

// Signal ids.
const (
	SignalAspect   = "aspect"
	SignalPalette  = "palette"
	SignalBytes    = "bytes"
	SignalElements = "elements"
	SignalCanvas   = "canvas"
)

// Metrics are the measured facts about one SVG.
type Metrics struct {
	Bytes    int64
	Elements int
	// HasViewBox is true only for a viewBox with four numbers and a positive
	// width and height. Width and height attributes are not consulted.
	HasViewBox    bool
	Width, Height float64
	// Colours are the distinct literal paint colours, normalised: hex as
	// lowercase #rrggbb (alpha dropped), rgb()/rgba() converted to the same, a
	// named colour as its lowercase name. none, transparent, currentColor,
	// inherit and anything that is not a literal (url(), var(), hsl()) are not
	// colours here.
	Colours []string
}

// Measure reads an SVG without building a tree and without fetching anything:
// Go's decoder does not resolve entities or load DTDs. size is the file size,
// which may exceed len(raw) when the caller reads only a prefix.
func Measure(raw []byte, size int64) (Metrics, error) {
	m := Metrics{Bytes: size}
	if len(bytes.TrimSpace(raw)) == 0 {
		return m, errors.New("empty document")
	}

	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }

	seen := map[string]bool{}
	// Strict decoding, like the SVG policy's parser: a file that policy would
	// refuse is skipped here and reported, not measured on a guess.
	var open []string
	depth, roots := 0, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, fmt.Errorf("parsing svg: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			open = append(open, t.Name.Local)
			m.Elements++
			if depth == 1 {
				roots++
				if t.Name.Local != "svg" {
					return m, fmt.Errorf("parsing svg: root element is %q", t.Name.Local)
				}
			}
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "viewBox":
					if depth == 1 {
						m.Width, m.Height, m.HasViewBox = parseViewBox(a.Value)
					}
				case "fill", "stroke", "stop-color":
					addColour(seen, &m, a.Value)
				case "style":
					for _, decl := range strings.Split(a.Value, ";") {
						prop, val, ok := strings.Cut(decl, ":")
						switch strings.ToLower(strings.TrimSpace(prop)) {
						case "fill", "stroke", "stop-color":
							if ok {
								addColour(seen, &m, val)
							}
						}
					}
				}
			}
		case xml.EndElement:
			if len(open) == 0 || open[len(open)-1] != t.Name.Local {
				return m, fmt.Errorf("parsing svg: unexpected closing tag %q", t.Name.Local)
			}
			open = open[:len(open)-1]
			depth--
		}
	}
	if len(open) != 0 {
		return m, fmt.Errorf("parsing svg: %q is never closed", open[len(open)-1])
	}
	if roots != 1 {
		return m, fmt.Errorf("parsing svg: %d root elements", roots)
	}
	return m, nil
}

func parseViewBox(v string) (w, h float64, ok bool) {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if len(fields) != 4 {
		return 0, 0, false
	}
	nums := make([]float64, 4)
	for i, f := range fields {
		n, err := strconv.ParseFloat(f, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, 0, false
		}
		nums[i] = n
	}
	if nums[2] <= 0 || nums[3] <= 0 {
		return 0, 0, false
	}
	return nums[2], nums[3], true
}

func addColour(seen map[string]bool, m *Metrics, raw string) {
	c, ok := normaliseColour(raw)
	if !ok || seen[c] {
		return
	}
	seen[c] = true
	m.Colours = append(m.Colours, c)
}

// normaliseColour returns the colour a paint value names, if it names one.
func normaliseColour(raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	v = strings.TrimSpace(strings.TrimSuffix(v, "!important"))

	switch v {
	case "", "none", "transparent", "currentcolor", "inherit", "initial", "unset", "revert":
		return "", false
	}

	switch {
	case strings.HasPrefix(v, "#"):
		hex := v[1:]
		for _, r := range hex {
			if !strings.ContainsRune("0123456789abcdef", r) {
				return "", false
			}
		}
		switch len(hex) {
		case 3, 4:
			return "#" + string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]}), true
		case 6, 8:
			return "#" + hex[:6], true
		}
		return "", false

	case strings.HasPrefix(v, "rgb(") || strings.HasPrefix(v, "rgba("):
		open := strings.IndexByte(v, '(')
		end := strings.IndexByte(v, ')')
		if end < open {
			return "", false
		}
		parts := strings.FieldsFunc(v[open+1:end], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return "", false
		}
		var rgb [3]int
		for i := 0; i < 3; i++ {
			s, pct := strings.CutSuffix(parts[i], "%")
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return "", false
			}
			if pct {
				n = n * 255 / 100
			}
			rgb[i] = int(math.Round(math.Min(255, math.Max(0, n))))
		}
		return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2]), true
	}

	// A bare word is a named colour. Anything with punctuation (url(), var(),
	// hsl(), context-fill) is not a literal this heuristic counts.
	for _, r := range v {
		if r < 'a' || r > 'z' {
			return "", false
		}
	}
	return v, true
}

// Signal is one fired signal with the value that fired it.
type Signal struct {
	ID    string  `json:"id"`
	Value float64 `json:"value"`
}

// Signals lists the signals that fire for these metrics, in the contract's
// order. Aspect and canvas need a viewBox; the other three do not.
func (m Metrics) Signals() []Signal {
	var out []Signal
	if m.HasViewBox {
		if ratio := m.Width / m.Height; ratio < HeuristicAspectMin || ratio > HeuristicAspectMax {
			out = append(out, Signal{SignalAspect, round2(ratio)})
		}
	}
	if len(m.Colours) >= HeuristicPaletteMin {
		out = append(out, Signal{SignalPalette, float64(len(m.Colours))})
	}
	if m.Bytes > HeuristicBytesMax {
		out = append(out, Signal{SignalBytes, float64(m.Bytes)})
	}
	if m.Elements > HeuristicElementsMax {
		out = append(out, Signal{SignalElements, float64(m.Elements)})
	}
	if m.HasViewBox {
		if side := math.Max(m.Width, m.Height); side > HeuristicCanvasMax {
			out = append(out, Signal{SignalCanvas, round2(side)})
		}
	}
	return out
}

// Qualifies applies the suggestion rule: enough signals, and at least one of
// them is bytes or elements. That requirement keeps flags out (4:3,
// multicolour, small).
func Qualifies(signals []Signal) bool {
	if len(signals) < HeuristicMinSignals {
		return false
	}
	for _, s := range signals {
		if s.ID == SignalBytes || s.ID == SignalElements {
			return true
		}
	}
	return false
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
