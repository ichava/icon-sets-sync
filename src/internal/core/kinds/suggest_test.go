package kinds

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// --- fixtures -------------------------------------------------------------

// svgDoc builds an SVG with a viewBox, n extra path elements, a set of literal
// fill colours, and enough padding to reach at least size bytes.
func svgDoc(viewBox string, extraElements int, colours []string, size int) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"`)
	if viewBox != "" {
		fmt.Fprintf(&b, ` viewBox="%s"`, viewBox)
	}
	b.WriteString(`>`)
	for i := 0; i < extraElements; i++ {
		b.WriteString(`<path d="M0 0h1"/>`)
	}
	for _, c := range colours {
		fmt.Fprintf(&b, `<rect fill="%s" width="1" height="1"/>`, c)
	}
	if pad := size - b.Len() - len("</svg>"); pad > 0 {
		b.WriteString(`<!--` + strings.Repeat("x", pad-7) + `-->`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

var nineColours = []string{"#111111", "#222222", "#333333", "#444444", "#555555", "#666666", "#777777", "#888888", "#999999"}

// scene is a real illustration: wide, many colours, big.
func scene() string { return svgDoc("0 0 1200 800", 40, nineColours, 48_000) }

// flag is the case the bytes/elements requirement exists for: 4:3 and
// multicolour, but small and simple.
func flag() string { return svgDoc("0 0 640 480", 3, nineColours[:5], 0) }

// glyph is an ordinary icon.
func glyph() string { return svgDoc("0 0 24 24", 1, nil, 0) }

type packFiles map[string]string // item path (no .svg) -> document

func writePack(t *testing.T, config string, files packFiles) (svgDir string) {
	t.Helper()
	svgDir = t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(svgDir, "config.json"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for p, doc := range files {
		full := filepath.Join(svgDir, "files", filepath.FromSlash(p)+".svg")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return svgDir
}

func scan(t *testing.T, config string, files packFiles, mode Mode) *ScanResult {
	t.Helper()
	res, err := Scan("acme/scenes", writePack(t, config, files), mode)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func paths(recs []Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Path)
	}
	sort.Strings(out)
	return out
}

// --- measurement ----------------------------------------------------------

func TestMeasure(t *testing.T) {
	doc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0, 0, 640 480" style="fill:#ABC">
	  <g fill="#FFFFFF" stroke="RGB(255, 0, 0)"><path d="M0 0" fill="#ffffffaa"/></g>
	  <linearGradient><stop stop-color="#112233"/><stop style="stop-color:#112233;opacity:.5"/></linearGradient>
	  <rect fill="none"/><rect fill="currentColor"/><rect fill="transparent"/><rect fill="inherit"/>
	  <rect fill="url(#g)"/><rect fill="var(--x)"/><rect stroke="Red"/>
	</svg>`
	m, err := Measure([]byte(doc), int64(len(doc)))
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasViewBox || m.Width != 640 || m.Height != 480 {
		t.Errorf("viewBox = %v %v x %v", m.HasViewBox, m.Width, m.Height)
	}
	if m.Elements != 13 {
		t.Errorf("elements = %d, want 13", m.Elements)
	}
	if m.Bytes != int64(len(doc)) {
		t.Errorf("bytes = %d", m.Bytes)
	}
	// #ABC and the ffffff pair collapse to hex6 and de-duplicate; the alpha
	// byte is dropped; rgb() converts; a name is kept; the exclusions are not
	// colours.
	want := []string{"#112233", "#aabbcc", "#ff0000", "#ffffff", "red"}
	got := append([]string(nil), m.Colours...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("colours = %v, want %v", got, want)
	}
}

func TestMeasureViewBoxVariants(t *testing.T) {
	for in, want := range map[string][3]float64{
		`viewBox="0 0 24 24"`:       {1, 24, 24},
		`viewBox="0,0,24.5,12"`:     {1, 24.5, 12},
		`viewBox="  0   0  10 20 "`: {1, 10, 20},
		`viewBox="0 0 24"`:          {0, 0, 0},
		`viewBox="0 0 0 24"`:        {0, 0, 0},
		`viewBox="a b c d"`:         {0, 0, 0},
		`width="24" height="24"`:    {0, 0, 0}, // the contract names viewBox only
		``:                          {0, 0, 0},
	} {
		doc := `<svg xmlns="http://www.w3.org/2000/svg" ` + in + `><path d="M0 0"/></svg>`
		m, err := Measure([]byte(doc), int64(len(doc)))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		has := 0.0
		if m.HasViewBox {
			has = 1
		}
		if got := [3]float64{has, m.Width, m.Height}; got != want {
			t.Errorf("%s: got %v, want %v", in, got, want)
		}
	}
}

func TestMeasureRejectsNonSVG(t *testing.T) {
	for _, doc := range []string{``, `not xml`, `<svg><path></svg>`} {
		if _, err := Measure([]byte(doc), int64(len(doc))); err == nil {
			t.Errorf("Measure(%q) succeeded", doc)
		}
	}
}

// --- signals --------------------------------------------------------------

func TestSignalThresholds(t *testing.T) {
	colours := func(n int) []string { return nineColours[:n] }
	cases := []struct {
		name string
		m    Metrics
		id   string
		fire bool
	}{
		{"aspect 1.25 is inside", Metrics{HasViewBox: true, Width: 125, Height: 100}, SignalAspect, false},
		{"aspect 1.26 is outside", Metrics{HasViewBox: true, Width: 126, Height: 100}, SignalAspect, true},
		{"aspect 0.8 is inside", Metrics{HasViewBox: true, Width: 80, Height: 100}, SignalAspect, false},
		{"aspect 0.79 is outside", Metrics{HasViewBox: true, Width: 79, Height: 100}, SignalAspect, true},
		{"no viewBox has no aspect", Metrics{}, SignalAspect, false},
		{"3 colours", Metrics{Colours: colours(3)}, SignalPalette, false},
		{"4 colours", Metrics{Colours: colours(4)}, SignalPalette, true},
		{"20000 bytes", Metrics{Bytes: 20_000}, SignalBytes, false},
		{"20001 bytes", Metrics{Bytes: 20_001}, SignalBytes, true},
		{"150 elements", Metrics{Elements: 150}, SignalElements, false},
		{"151 elements", Metrics{Elements: 151}, SignalElements, true},
		{"canvas 256", Metrics{HasViewBox: true, Width: 256, Height: 256}, SignalCanvas, false},
		{"canvas 257 wide", Metrics{HasViewBox: true, Width: 257, Height: 10}, SignalCanvas, true},
		{"canvas 257 tall", Metrics{HasViewBox: true, Width: 10, Height: 257}, SignalCanvas, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fired := false
			for _, s := range tc.m.Signals() {
				if s.ID == tc.id {
					fired = true
				}
			}
			if fired != tc.fire {
				t.Errorf("%s fired = %v, want %v", tc.id, fired, tc.fire)
			}
		})
	}
}

func TestSignalValuesAndOrder(t *testing.T) {
	m := Metrics{
		Bytes: 48_211, Elements: 200, HasViewBox: true, Width: 1600, Height: 900,
		Colours: nineColours,
	}
	got := m.Signals()
	want := []Signal{
		{SignalAspect, 1.78}, {SignalPalette, 9}, {SignalBytes, 48_211},
		{SignalElements, 200}, {SignalCanvas, 1600},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("signals = %+v, want %+v", got, want)
	}
}

func TestQualifies(t *testing.T) {
	sig := func(ids ...string) []Signal {
		var out []Signal
		for _, id := range ids {
			out = append(out, Signal{ID: id, Value: 1})
		}
		return out
	}
	for _, tc := range []struct {
		name string
		in   []Signal
		want bool
	}{
		{"none", nil, false},
		{"two incl. bytes", sig(SignalBytes, SignalAspect), false},
		{"three incl. bytes", sig(SignalAspect, SignalPalette, SignalBytes), true},
		{"three incl. elements", sig(SignalAspect, SignalElements, SignalCanvas), true},
		{"three without bytes or elements", sig(SignalAspect, SignalPalette, SignalCanvas), false},
		{"all five", sig(SignalAspect, SignalPalette, SignalBytes, SignalElements, SignalCanvas), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Qualifies(tc.in); got != tc.want {
				t.Errorf("Qualifies = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHeuristicConstantsAreTheContracts(t *testing.T) {
	if HeuristicAspectMin != 0.8 || HeuristicAspectMax != 1.25 || HeuristicPaletteMin != 4 ||
		HeuristicBytesMax != 20_000 || HeuristicElementsMax != 150 || HeuristicCanvasMax != 256 ||
		HeuristicMinSignals != 3 || HeuristicGroupMin != 5 {
		t.Fatal("a heuristic constant drifted from contract section 3; core's IchavaConstants::KIND_HEURISTIC_* must match")
	}
}

// --- scanning -------------------------------------------------------------

func TestFlagsProduceNoSuggestionAndASceneProducesOne(t *testing.T) {
	res := scan(t, "", packFiles{
		"flags/de":    flag(),
		"flags/fr":    flag(),
		"glyphs/home": glyph(),
		"glyphs/hero": scene(),
	}, ModeDefault)

	if res.Items != 4 {
		t.Fatalf("items = %d, want 4", res.Items)
	}
	if got := paths(res.Records); !reflect.DeepEqual(got, []string{"glyphs/hero"}) {
		t.Fatalf("records for %v, want only the scene", got)
	}
	r := res.Records[0]
	if r.Package != "acme/scenes" || r.Suggested != Illustration || r.Current != (Current{Icon, SourceDefault}) {
		t.Errorf("record = %+v", r)
	}
	if !reflect.DeepEqual(r.Accept, map[string]any{"items": map[string]any{"glyphs/hero": map[string]any{"kind": "illustration"}}}) {
		t.Errorf("accept = %#v", r.Accept)
	}
	ids := map[string]bool{}
	for _, s := range r.Signals {
		ids[s.ID] = true
	}
	for _, id := range []string{SignalAspect, SignalPalette, SignalBytes, SignalCanvas} {
		if !ids[id] {
			t.Errorf("signal %s missing from %+v", id, r.Signals)
		}
	}
}

func TestOnlyDefaultSourcedItemsAreSuggestedUnlessAll(t *testing.T) {
	files := packFiles{
		"a/by-default":  scene(),
		"b/by-category": scene(),
		"c/by-rule":     scene(),
		"d/by-item":     scene(),
		"e/by-pack":     scene(),
	}
	config := func(pack string) string {
		kind := ""
		if pack != "" {
			kind = `"kind": "` + pack + `",`
		}
		return `{"package": {` + kind + `"name": "acme/scenes"},
		 "metadata": {"data": {
		   "categories": {"b": {"kind": "icon"}},
		   "kinds": {"rules": [{"match": "c/**", "kind": "icon"}]},
		   "items": {"d/by-item": {"kind": "icon"}}}}}`
	}

	// No pack kind: e/by-pack resolves from default, like a/by-default.
	res := scan(t, config(""), files, ModeDefault)
	if got := paths(res.Records); !reflect.DeepEqual(got, []string{"a/by-default", "e/by-pack"}) {
		t.Errorf("default mode, no pack kind: %v", got)
	}

	// An explicit pack kind of icon is source pack: only --suggest-kinds=all.
	res = scan(t, config("icon"), files, ModeDefault)
	if got := paths(res.Records); len(got) != 0 {
		t.Errorf("default mode, explicit pack kind: %v, want none", got)
	}
	res = scan(t, config("icon"), files, ModeAll)
	if got := paths(res.Records); !reflect.DeepEqual(got, []string{"a/by-default", "e/by-pack"}) {
		t.Errorf("all mode: %v; category, rule and item are human decisions and stay out", got)
	}
	for _, r := range res.Records {
		if r.Current.Source != SourcePack {
			t.Errorf("%s current source = %s", r.Path, r.Current.Source)
		}
	}
}

func TestAnItemAlreadyResolvedIllustrationIsNeverSuggested(t *testing.T) {
	res := scan(t, `{"package": {"kind": "illustration"}}`, packFiles{"a/b": scene()}, ModeAll)
	if len(res.Records) != 0 {
		t.Errorf("records: %+v", res.Records)
	}
}

func TestGroupingAtFiveItemsAndEightyPercent(t *testing.T) {
	folder := func(prefix string, scenes, glyphs int) packFiles {
		f := packFiles{}
		for i := 0; i < scenes; i++ {
			f[fmt.Sprintf("%s/s%d", prefix, i)] = scene()
		}
		for i := 0; i < glyphs; i++ {
			f[fmt.Sprintf("%s/g%d", prefix, i)] = glyph()
		}
		return f
	}

	t.Run("5 of 5 become one rule", func(t *testing.T) {
		res := scan(t, "", folder("spot", 5, 0), ModeDefault)
		if len(res.Records) != 1 {
			t.Fatalf("records = %d, want 1: %v", len(res.Records), paths(res.Records))
		}
		r := res.Records[0]
		if r.Path != "spot" || r.Suggested != Illustration {
			t.Errorf("record = %+v", r)
		}
		wantAccept := map[string]any{"kinds": map[string]any{"rules": []any{
			map[string]any{"match": "spot/**", "kind": "illustration"},
		}}}
		if !reflect.DeepEqual(r.Accept, wantAccept) {
			t.Errorf("accept = %#v", r.Accept)
		}
		if len(r.Signals) == 0 {
			t.Error("a group record carries no signals")
		}
	})
	t.Run("4 of 5 is exactly 80 percent and groups", func(t *testing.T) {
		res := scan(t, "", folder("spot", 4, 1), ModeDefault)
		if got := paths(res.Records); !reflect.DeepEqual(got, []string{"spot"}) {
			t.Errorf("records: %v", got)
		}
	})
	t.Run("3 of 5 stays per item", func(t *testing.T) {
		res := scan(t, "", folder("spot", 3, 2), ModeDefault)
		if got := paths(res.Records); !reflect.DeepEqual(got, []string{"spot/s0", "spot/s1", "spot/s2"}) {
			t.Errorf("records: %v", got)
		}
	})
	t.Run("4 of 4 is under the five-item floor", func(t *testing.T) {
		res := scan(t, "", folder("spot", 4, 0), ModeDefault)
		if len(res.Records) != 4 {
			t.Errorf("records: %v", paths(res.Records))
		}
	})
	t.Run("folders group independently", func(t *testing.T) {
		files := folder("spot", 5, 0)
		for k, v := range folder("misc", 2, 4) {
			files[k] = v
		}
		res := scan(t, "", files, ModeDefault)
		if got := paths(res.Records); !reflect.DeepEqual(got, []string{"misc/s0", "misc/s1", "spot"}) {
			t.Errorf("records: %v", got)
		}
	})
}

func TestThemeTwinsAreNotItems(t *testing.T) {
	config := `{"metadata": {"data": {"categories": {"illustrations": {"theme_variants": {"dark": "-dark"}}}}}}`
	res := scan(t, config, packFiles{
		"illustrations/office":      scene(),
		"illustrations/office-dark": scene(),
		"illustrations/lonely-dark": scene(), // no light sibling: an item in its own right
		"illustrations/-dark":       scene(), // the stem is the suffix itself: an item
	}, ModeDefault)

	if res.Items != 3 {
		t.Errorf("items = %d, want 3 (office, lonely-dark, -dark)", res.Items)
	}
	for _, p := range paths(res.Records) {
		if p == "illustrations/office-dark" {
			t.Error("a theme twin was suggested as an item")
		}
	}
}

func TestUnparseableFilesAreSkippedAndListed(t *testing.T) {
	res := scan(t, "", packFiles{"a/broken": `<svg><path></svg>`, "a/fine": glyph()}, ModeDefault)
	if res.Items != 2 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "a/broken") {
		t.Errorf("items %d skipped %v", res.Items, res.Skipped)
	}
}

func TestSymlinksAndOtherFilesAreIgnored(t *testing.T) {
	svgDir := writePack(t, "", packFiles{"a/real": scene()})
	files := filepath.Join(svgDir, "files")
	if err := os.Symlink(filepath.Join(files, "a", "real.svg"), filepath.Join(files, "a", "link.svg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(files, "a", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Scan("acme/scenes", svgDir, ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	if res.Items != 1 {
		t.Errorf("items = %d, want 1: a symlink or a non-svg was read", res.Items)
	}
}

func TestIssuesFromTheConfigAreCarried(t *testing.T) {
	res := scan(t, `{"package": {"kind": "banana"}}`, packFiles{"a/b": glyph()}, ModeDefault)
	if len(res.Issues) != 1 || res.Issues[0].Code != IssueInvalidValue {
		t.Errorf("issues = %+v", res.Issues)
	}
}

func TestScanErrors(t *testing.T) {
	if _, err := Scan("p", filepath.Join(t.TempDir(), "missing"), ModeDefault); err == nil {
		t.Error("a missing directory scanned")
	}
	dir := writePack(t, `{not json`, packFiles{"a/b": glyph()})
	if _, err := Scan("p", dir, ModeDefault); err == nil {
		t.Error("a malformed config scanned")
	}
}

// The heuristic suggests; it never writes. Scan twice over a pack that yields
// suggestions and compare every file under the pack before and after.
func TestScanNeverWritesAnything(t *testing.T) {
	config := `{"package": {"name": "acme/scenes"}}`
	svgDir := writePack(t, config, packFiles{"a/hero": scene(), "a/home": glyph()})
	before := snapshot(t, svgDir)

	for _, mode := range []Mode{ModeDefault, ModeAll} {
		res, err := Scan("acme/scenes", svgDir, mode)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Records) == 0 {
			t.Fatal("no suggestions to prove the point with")
		}
	}
	if after := snapshot(t, svgDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("a scan changed the pack:\nbefore %v\nafter  %v", before, after)
	}
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 4 {
		t.Fatalf("snapshot saw only %d entries", len(out))
	}
	return out
}

// --- output ---------------------------------------------------------------

func TestRecordJSONShapeMatchesTheContract(t *testing.T) {
	rec := Record{
		Package: "acme/scenes", Path: "glyphs/hero-banner",
		Current:   Current{Icon, SourceDefault},
		Suggested: Illustration,
		Signals:   []Signal{{SignalAspect, 1.78}, {SignalBytes, 48_211}, {SignalPalette, 9}},
		Accept:    map[string]any{"items": map[string]any{"glyphs/hero-banner": map[string]any{"kind": "illustration"}}},
	}
	got, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"package":"acme/scenes","path":"glyphs/hero-banner",` +
		`"current":{"kind":"icon","source":"default"},"suggested":"illustration",` +
		`"signals":[{"id":"aspect","value":1.78},{"id":"bytes","value":48211},{"id":"palette","value":9}],` +
		`"accept":{"items":{"glyphs/hero-banner":{"kind":"illustration"}}}}`
	if string(got) != want {
		t.Errorf("record JSON\n got %s\nwant %s", got, want)
	}
}

func TestWriteRecords(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build", "kind-suggestions", "scenes.json")

	if err := WriteRecords(out, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("empty list wrote %q, err %v; want []", data, err)
	}

	recs := []Record{{Package: "p", Path: "a", Current: Current{Icon, SourceDefault}, Suggested: Illustration,
		Signals: []Signal{{SignalBytes, 30000}}, Accept: map[string]any{}}}
	if err := WriteRecords(out, recs); err != nil {
		t.Fatal(err)
	}
	var back []Record
	data, _ = os.ReadFile(out)
	if err := json.Unmarshal(data, &back); err != nil || len(back) != 1 || back[0].Path != "a" {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("no trailing newline")
	}
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"": ModeDefault, "default": ModeDefault, "all": ModeAll} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseMode("everything"); err == nil {
		t.Error("an unknown mode parsed")
	}
}
