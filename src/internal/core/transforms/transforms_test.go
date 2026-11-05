package transforms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// tree builds a fetched tree and a context pointing at it.
func tree(t *testing.T, files map[string]string) (*pipeline.Context, string) {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := &pipeline.Context{
		PipelineName: "test", WorkingDir: root,
		Config: map[string]any{}, Extras: map[string]any{}, Metrics: map[string]any{},
	}
	ctx.SetFetchedPath(root)
	return ctx, root
}

func TestSubsetToNarrowsTheTree(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"icons/home.svg": "<svg/>",
		"package.json":   "{}",
	})

	if err := (SubsetTo{Subdir: "icons"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := ctx.FetchedPath()
	if got != filepath.Join(root, "icons") {
		t.Errorf("fetched_path = %s", got)
	}
}

func TestSubsetToOnAMissingSubdirIsAnError(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (SubsetTo{Subdir: "nope"}).Execute(ctx); err == nil {
		t.Fatal("expected an error: a missing source_path means the config is wrong, " +
			"and silently syncing the whole tarball would be worse")
	}
}

func TestSubsetToWithNoSubdirIsANoOp(t *testing.T) {
	ctx, root := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (SubsetTo{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := ctx.FetchedPath(); got != root {
		t.Errorf("fetched_path = %s, want it unchanged", got)
	}
}

func TestSanitiseRewritesOnlyWhatChanged(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"clean.svg": `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`,
		"dirty.svg": `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0" onload="x()"/></svg>`,
	})

	cleanBefore, _ := os.ReadFile(filepath.Join(root, "clean.svg"))

	if err := (Sanitise{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	cleanAfter, _ := os.ReadFile(filepath.Join(root, "clean.svg"))
	if string(cleanBefore) != string(cleanAfter) {
		t.Errorf("an already-clean file was rewritten:\n%s\n%s", cleanBefore, cleanAfter)
	}

	dirty, _ := os.ReadFile(filepath.Join(root, "dirty.svg"))
	if strings.Contains(string(dirty), "onload") {
		t.Errorf("the handler survived: %s", dirty)
	}

	metrics, _ := ctx.Metrics["sanitise"].(map[string]any)
	if metrics["scanned"] != 2 || metrics["cleaned"] != 1 || metrics["violations"] != 1 {
		t.Errorf("metrics = %v", metrics)
	}
}

// TestSanitiseLeavesAnUnparseableFileAlone matters because the alternative is
// worse either way: dropping it silently shrinks the pack, and rewriting a
// guess at it corrupts an icon nobody asked us to fix.
func TestSanitiseLeavesAnUnparseableFileAlone(t *testing.T) {
	broken := `<svg xmlns="http://www.w3.org/2000/svg"><path d=M0 0/></svg>`
	ctx, root := tree(t, map[string]string{"broken.svg": broken})

	if err := (Sanitise{}).Execute(ctx); err != nil {
		t.Fatalf("one unparseable file must not fail the whole transform: %v", err)
	}

	after, _ := os.ReadFile(filepath.Join(root, "broken.svg"))
	if string(after) != broken {
		t.Errorf("the file was modified:\n%s", after)
	}
	metrics, _ := ctx.Metrics["sanitise"].(map[string]any)
	if metrics["unparsable"] != 1 {
		t.Errorf("metrics = %v, want unparsable=1", metrics)
	}
}

func TestSanitiseStrictFailsOnAViolation(t *testing.T) {
	ctx, _ := tree(t, map[string]string{
		"a.svg": `<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`,
	})
	if err := (Sanitise{Strict: true}).Execute(ctx); err == nil {
		t.Fatal("strict mode must fail on a violation")
	}
}

func TestSlugify(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"Arrow Up.svg":    "<svg/>",
		"already-ok.svg":  "<svg/>",
		"WEIRD__name.SVG": "<svg/>",
	})

	if err := (Slugify{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"arrow-up.svg", "already-ok.svg", "weird-name.svg"} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			entries, _ := os.ReadDir(root)
			var got []string
			for _, e := range entries {
				got = append(got, e.Name())
			}
			t.Errorf("%s missing; tree is %v", want, got)
		}
	}
}

const cldrSample = `# group: Smileys & Emotion

# subgroup: face-smiling
1F600                                      ; fully-qualified     # 😀 E1.0 grinning face
1F603                                      ; fully-qualified     # 😃 E0.6 grinning face with big eyes
263A FE0F                                  ; fully-qualified     # ☺️ E0.6 smiling face
263A                                       ; unqualified         # ☺ E0.6 smiling face

# group: Animals & Nature

# subgroup: animal-mammal
1F436                                      ; fully-qualified     # 🐶 E0.6 dog face
`

func TestParseCLDR(t *testing.T) {
	records := ParseCLDR(cldrSample)

	// Four fully-qualified rows; the unqualified one is skipped because it is
	// the same emoji with fewer selectors and would collide on output name.
	if len(records) != 4 {
		t.Fatalf("got %d records, want 4: %+v", len(records), records)
	}

	if records[0].Key() != "1f600" || records[0].Group != "Smileys & Emotion" {
		t.Errorf("first record = %+v", records[0])
	}
	// A multi-codepoint sequence joins with a hyphen, matching the filenames
	// upstream ships.
	if records[2].Key() != "263a-fe0f" {
		t.Errorf("multi-codepoint key = %q", records[2].Key())
	}
	// The group heading carries forward to the rows beneath it.
	if records[3].Group != "Animals & Nature" {
		t.Errorf("last record group = %q", records[3].Group)
	}
}

func TestSlugifyGroupAndName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Smileys & Emotion", "smileys-emotion"},
		{"Animals & Nature", "animals-nature"},
		{"Travel & Places", "travel-places"},
	} {
		if got := SlugifyGroup(tc.in); got != tc.want {
			t.Errorf("SlugifyGroup(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"grinning face", "grinning-face"},
		{"flag: Japan", "flag-japan"},
		{"keycap: *", "keycap"},
	} {
		if got := SlugifyName(tc.in); got != tc.want {
			t.Errorf("SlugifyName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCategoriseFilesByGroupAndRenamesToTheSlug(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"1F600.svg":     "<svg/>",
		"1F436.svg":     "<svg/>",
		"263A-FE0F.svg": "<svg/>",
		"DEADBEEF.svg":  "<svg/>", // not in the taxonomy
	})
	ctx.SetString(pipeline.KeyCLDRText, cldrSample)

	if err := (Categorise{By: "cldr"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"smileys-emotion/grinning-face.svg",
		"smileys-emotion/smiling-face.svg",
		"animals-nature/dog-face.svg",
	} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("%s missing", want)
		}
	}

	metrics, _ := ctx.Metrics["categorise"].(map[string]any)
	if metrics["copied"] != 3 || metrics["missing"] != 1 {
		t.Errorf("metrics = %v, want copied=3 missing=1", metrics)
	}
	if _, err := os.Stat(filepath.Join(root, "1F600.svg")); err == nil {
		t.Error("the flat original survived alongside the categorised copy")
	}
}

// TestCategoriseRefusesAnEmptyTaxonomy: with no records every file lands in
// "missing" and the pack is silently emptied.
func TestCategoriseRefusesAnEmptyTaxonomy(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"1F600.svg": "<svg/>"})
	ctx.SetString(pipeline.KeyCLDRText, "# nothing parseable here\n")

	if err := (Categorise{By: "cldr"}).Execute(ctx); err == nil {
		t.Fatal("an empty taxonomy must be an error, not a silent wipe")
	}
}

func TestCategorisePassthroughIsANoOp(t *testing.T) {
	ctx, root := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (Categorise{By: "passthrough"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.svg")); err != nil {
		t.Error("passthrough moved a file")
	}
}

func TestCategoriseRejectsAnUnknownTaxonomy(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (Categorise{By: "astrology"}).Execute(ctx); err == nil {
		t.Fatal("expected an error naming the unknown taxonomy")
	}
}

func TestIndexerWritesBothLookups(t *testing.T) {
	ctx, root := tree(t, map[string]string{"x/a.svg": "<svg/>"})
	ctx.SetFetchedPath(filepath.Join(root, "x"))
	ctx.Extras[pipeline.KeyCLDRRecords] = ParseCLDR(cldrSample)

	if err := (Indexer{Targets: []string{"codepoints", "names"}}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "codepoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	var codepoints map[string]string
	if err := json.Unmarshal(raw, &codepoints); err != nil {
		t.Fatal(err)
	}
	if codepoints["1F600"] != "smileys-emotion/grinning-face" {
		t.Errorf("codepoints[1F600] = %q", codepoints["1F600"])
	}

	// The ampersand in a CLDR group name must not arrive HTML-escaped: on by
	// default Go would write & and the file would differ from the
	// Python's for no reason anyone could see in a diff.
	if strings.Contains(string(raw), `&`) {
		t.Error("output is HTML-escaped")
	}

	if _, err := os.Stat(filepath.Join(root, "names.json")); err != nil {
		t.Error("names.json missing")
	}
}

func TestIndexerNeedsRecords(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (Indexer{Targets: []string{"names"}}).Execute(ctx); err == nil {
		t.Fatal("expected an error naming the missing cldr_records")
	}
}

// OpenMoji names a file for its codepoints with the emoji-presentation selector
// left off for ~200 emoji (2600.svg, not 2600-fe0f.svg), while emoji-test.txt
// lists them fully qualified. Matched exactly they found no category and were
// silently dropped -- the sun, the wastebasket, every arrow.
func TestCategorisePreserveMatchesAnEmojiWhoseSelectorIsLeftOff(t *testing.T) {
	ctx, root := tree(t, map[string]string{"1F600.svg": "<svg/>", "263A.svg": "<svg/>"})
	ctx.SetString(pipeline.KeyCLDRText, cldrSample)

	if err := (Categorise{By: "cldr", Preserve: true}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "smileys-emotion", "smiling-face.svg")); err != nil {
		t.Errorf("263A.svg did not reach its category: %v", err)
	}
	m, _ := ctx.Metrics["categorise"].(map[string]any)
	if m["copied"] != 2 || m["missing"] != 0 || m["uncategorised"] != 0 {
		t.Errorf("metrics = %v", m)
	}
}

// TestCategorisePreserveKeepsUnmatchedIcons: an icon with no taxonomy entry
// (an OpenMoji extra, or an emoji newer than the taxonomy) is kept in an
// explicit bucket, under its upstream name, and counted -- never dropped.
func TestCategorisePreserveKeepsUnmatchedIcons(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"1F600.svg": "<svg/>", "1F436.svg": "<svg/>", "263A-FE0F.svg": "<svg/>",
		"E380.svg": "<svg/>",
	})
	ctx.SetString(pipeline.KeyCLDRText, cldrSample)

	if err := (Categorise{By: "cldr", Preserve: true, MaxUncategorised: 0.5}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, UncategorisedDir, "e380.svg")); err != nil {
		t.Errorf("the unmatched icon was not kept: %v", err)
	}
	m, _ := ctx.Metrics["categorise"].(map[string]any)
	if m["copied"] != 3 || m["uncategorised"] != 1 {
		t.Errorf("metrics = %v, want copied=3 uncategorised=1", m)
	}
}

func TestCategorisePreserveRefusesAnImplausibleUncategorisedShare(t *testing.T) {
	files := map[string]string{"1F600.svg": "<svg/>", "A1.svg": "<svg/>", "A2.svg": "<svg/>", "A3.svg": "<svg/>"}

	ctx, _ := tree(t, files)
	ctx.SetString(pipeline.KeyCLDRText, cldrSample)
	if err := (Categorise{By: "cldr", Preserve: true, MaxUncategorised: 0.5}).Execute(ctx); err == nil {
		t.Fatal("75% uncategorised must fail the run: the taxonomy is probably the wrong release")
	}

	// Forced, it goes through, and still keeps everything.
	ctx, root := tree(t, files)
	ctx.SetString(pipeline.KeyCLDRText, cldrSample)
	if err := (Categorise{By: "cldr", Preserve: true, MaxUncategorised: 0.5, Force: true}).Execute(ctx); err != nil {
		t.Fatalf("a forced run must pass: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, UncategorisedDir, "a3.svg")); err != nil {
		t.Errorf("forced run lost an icon: %v", err)
	}
}

// TestCategoriseWithoutPreserveIsUnchanged pins the emoji pack's behaviour:
// exact match only, and an unmatched icon is dropped and counted as missing.
func TestCategoriseWithoutPreserveIsUnchanged(t *testing.T) {
	ctx, root := tree(t, map[string]string{"1F600.svg": "<svg/>", "263A.svg": "<svg/>", "E380.svg": "<svg/>"})
	ctx.SetString(pipeline.KeyCLDRText, cldrSample)

	if err := (Categorise{By: "cldr"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ := ctx.Metrics["categorise"].(map[string]any)
	if m["copied"] != 1 || m["missing"] != 2 {
		t.Errorf("metrics = %v, want copied=1 missing=2", m)
	}
	if _, ok := m["uncategorised"]; ok {
		t.Error("the legacy metric shape gained a key")
	}
	if _, err := os.Stat(filepath.Join(root, UncategorisedDir)); err == nil {
		t.Error("the legacy path created an uncategorised bucket")
	}
}

// Two emoji can slug to one name ("keycap: #" and "keycap: *"). Renaming the
// second over the first dropped an icon without a word.
func TestCategorisePreserveKeepsBothSidesOfASlugCollision(t *testing.T) {
	ctx, root := tree(t, map[string]string{"0023-FE0F-20E3.svg": "<svg id='hash'/>", "002A-FE0F-20E3.svg": "<svg id='star'/>"})
	ctx.SetString(pipeline.KeyCLDRText, `# group: Symbols

0023 FE0F 20E3 ; fully-qualified # # E0.6 keycap
002A FE0F 20E3 ; fully-qualified # * E2.0 keycap
`)
	if err := (Categorise{By: "cldr", Preserve: true}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(filepath.Join(root, "symbols", "keycap.svg"))
	if err != nil || !strings.Contains(string(kept), "star") {
		t.Errorf("the later emoji should keep the name, as before: %q %v", kept, err)
	}
	if _, err := os.Stat(filepath.Join(root, UncategorisedDir, "0023-fe0f-20e3.svg")); err != nil {
		t.Errorf("the displaced icon was dropped: %v", err)
	}
	m, _ := ctx.Metrics["categorise"].(map[string]any)
	if m["copied"] != 1 || m["uncategorised"] != 1 {
		t.Errorf("metrics = %v", m)
	}
}

func strayTree(t *testing.T, n int) (*pipeline.Context, string) {
	t.Helper()
	files := map[string]string{"clean.svg": `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`}
	for i := 0; i < n; i++ {
		files[filepath.Join("stray", strings.Repeat("x", i+1)+".svg")] =
			`<svg xmlns="http://www.w3.org/2000/svg"><g>d<path d="M0 0"/></g></svg>`
	}
	return tree(t, files)
}

func TestSanitiseDropsStrayTextAndCountsIt(t *testing.T) {
	ctx, root := strayTree(t, 2)
	if err := (Sanitise{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "stray", "x.svg"))
	if strings.Contains(string(got), ">d<") {
		t.Errorf("stray text survived: %s", got)
	}
	metrics, _ := ctx.Metrics["sanitise"].(map[string]any)
	if metrics["stray_text_files"] != 2 || metrics["stray_text_nodes"] != 2 {
		t.Errorf("metrics = %v", metrics)
	}
}

// A malformed upstream must not silently rewrite a pack: more than
// MaxStrayTextFiles files with stray text fails the run, unless forced.
func TestSanitiseRefusesTooManyStrayTextFiles(t *testing.T) {
	ctx, root := strayTree(t, DefaultMaxStrayTextFiles+1)
	before, _ := os.ReadFile(filepath.Join(root, "stray", "x.svg"))
	err := (Sanitise{}).Execute(ctx)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal naming --force", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "stray", "x.svg"))
	if string(before) != string(after) {
		t.Error("a refused run must write nothing")
	}

	ctx, _ = strayTree(t, DefaultMaxStrayTextFiles)
	if err := (Sanitise{}).Execute(ctx); err != nil {
		t.Errorf("exactly %d files is within the limit: %v", DefaultMaxStrayTextFiles, err)
	}

	ctx, _ = strayTree(t, DefaultMaxStrayTextFiles+1)
	if err := (Sanitise{Force: true}).Execute(ctx); err != nil {
		t.Errorf("a forced run goes through: %v", err)
	}

	ctx, _ = strayTree(t, DefaultMaxStrayTextFiles+1)
	if err := (Sanitise{ReportOnly: true}).Execute(ctx); err != nil {
		t.Errorf("report-only writes nothing, so it is not refused: %v", err)
	}
}

// TestSlugifyOptions is the table for the rules the v0.1.0 packs need beyond
// the default: directory names (Remix's `Game & Sports`) and dots inside a
// stem (gilbarbara/logos' `lucene.net`).
func TestSlugifyOptions(t *testing.T) {
	cases := []struct {
		name  string
		t     Slugify
		files []string
		want  []string
	}{
		{"default keeps dots", Slugify{}, []string{"lucene.net.svg", "Web.Dev-Icon.svg"}, []string{"lucene.net.svg", "web.dev-icon.svg"}},
		{"hyphenate_dots", Slugify{HyphenateDots: true}, []string{"lucene.net.svg", "daisyUI-icon.svg", "khan_academy.svg"},
			[]string{"daisyui-icon.svg", "khan-academy.svg", "lucene-net.svg"}},
		{"dirs", Slugify{Dirs: true}, []string{"Game & Sports/Ball.svg", "User & Faces/a/B c.svg"},
			[]string{"game-sports/ball.svg", "user-faces/a/b-c.svg"}},
		{"dirs off leaves directories", Slugify{}, []string{"Game & Sports/Ball.svg"}, []string{"Game & Sports/ball.svg"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for _, f := range tc.files {
				files[f] = "<svg/>"
			}
			ctx, root := tree(t, files)
			if err := tc.t.Execute(ctx); err != nil {
				t.Fatal(err)
			}
			got := listTree(t, root)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("got %v\nwant %v", got, tc.want)
			}
		})
	}
}

// TestSlugifyRefusesACollision: renaming onto an existing file would lose one
// icon at an unchanged count.
func TestSlugifyRefusesACollision(t *testing.T) {
	cases := []struct {
		name  string
		t     Slugify
		files []string
	}{
		{"two files", Slugify{HyphenateDots: true}, []string{"a.b.svg", "a-b.svg"}},
		{"two directories", Slugify{Dirs: true}, []string{"A B/x.svg", "a-b/y.svg"}},
		{"a name with nothing to keep", Slugify{HyphenateDots: true}, []string{"!.svg"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for _, f := range tc.files {
				files[f] = "<svg/>"
			}
			ctx, _ := tree(t, files)
			if err := tc.t.Execute(ctx); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// TestFlattenSlugsTheStemBeforeTheAffix is Health Icons: `!.svg` was
// committed as `-filled.svg`, which only slugging before the suffix produces.
func TestFlattenSlugsTheStemBeforeTheAffix(t *testing.T) {
	ctx, _ := tree(t, map[string]string{
		"filled/blood/!.svg":            "<svg/>",
		"filled/blood/Blood_A_N.svg":    "<svg/>",
		"outline/zoonoses/mosquito.svg": "<svg/>",
	})
	err := FlattenVariants{Slug: true, Variants: []Variant{
		{Subdir: "filled", Suffix: "-filled"}, {Subdir: "outline", Suffix: "-outline"},
	}}.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := ctx.FetchedPath()
	got := strings.Join(flattened(t, out), " ")
	if got != "-filled.svg blood-a-n-filled.svg mosquito-outline.svg" {
		t.Fatalf("got %v", got)
	}
}

// TestSanitiseDropUnparsable: with the option a file that cannot be checked
// never reaches the pack; without it the file is left alone, as before.
func TestSanitiseDropUnparsable(t *testing.T) {
	bad := `<!DOCTYPE svg [<!ENTITY ns_ai "http://ns.adobe.com/AdobeIllustrator/10.0/">]><svg xmlns:x="&ns_ai;"/>`
	for _, drop := range []bool{false, true} {
		ctx, root := tree(t, map[string]string{"bad.svg": bad, "good.svg": `<svg xmlns="http://www.w3.org/2000/svg"/>`})
		if err := (Sanitise{DropUnparsable: drop}).Execute(ctx); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(filepath.Join(root, "bad.svg"))
		if drop != os.IsNotExist(err) {
			t.Errorf("drop_unparsable=%v: bad.svg present=%v", drop, err == nil)
		}
		if _, err := os.Stat(filepath.Join(root, "good.svg")); err != nil {
			t.Errorf("drop_unparsable=%v removed a parsable file", drop)
		}
		m, _ := ctx.Metrics["sanitise"].(map[string]any)
		if want := map[bool]int{false: 0, true: 1}[drop]; m["dropped_unparsable"] != want {
			t.Errorf("drop_unparsable=%v: metric %v, want %d", drop, m["dropped_unparsable"], want)
		}
	}
}
