package transforms

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// listTree returns every file under dir, slash-separated and sorted.
func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestSplitVariants covers each upstream shape the v0.1.0 packs need, one
// row per shape, against the layout the pack committed.
func TestSplitVariants(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		variants []SplitVariant
		want     []string
		// bodies pins which upstream file each output came from, so a split
		// that names files right and copies the wrong ones fails.
		bodies map[string]string
	}{
		{
			// Ionicons: one flat directory, the variant in a name suffix, and
			// the unsuffixed name (logos included) is the filled variant.
			name: "suffix in one directory, bare name takes the rest",
			files: map[string]string{
				"bell.svg": "f", "bell-outline.svg": "o", "bell-sharp.svg": "s", "logo-github.svg": "l",
			},
			variants: []SplitVariant{
				{Path: "outline", Suffix: "-outline"}, {Path: "sharp", Suffix: "-sharp"}, {Path: "filled"},
			},
			want:   []string{"filled/bell.svg", "filled/logo-github.svg", "outline/bell.svg", "sharp/bell.svg"},
			bodies: map[string]string{"filled/bell.svg": "f", "outline/bell.svg": "o", "sharp/bell.svg": "s"},
		},
		{
			// Heroicons: the variant is the directory, names are bare.
			name: "directory per variant",
			files: map[string]string{
				"24/outline/bell.svg": "o", "24/solid/bell.svg": "s", "20/solid/bell.svg": "m", "package.json": "{}",
			},
			variants: []SplitVariant{
				{Path: "outline", From: "24/outline"}, {Path: "solid", From: "24/solid"}, {Path: "mini", From: "20/solid"},
			},
			want:   []string{"mini/bell.svg", "outline/bell.svg", "solid/bell.svg"},
			bodies: map[string]string{"mini/bell.svg": "m", "solid/bell.svg": "s"},
		},
		{
			// Phosphor and Material Symbols: directory and suffix at once.
			name: "directory and suffix together",
			files: map[string]string{
				"regular/bell.svg": "r", "thin/bell-thin.svg": "t",
				"outlined/home.svg": "h", "outlined/home-fill.svg": "hf",
			},
			variants: []SplitVariant{
				{Path: "regular", From: "regular"}, {Path: "thin", From: "thin", Suffix: "-thin"},
				{Path: "outlined", From: "outlined"}, {Path: "outlined-fill", From: "outlined", Suffix: "-fill"},
			},
			want:   []string{"outlined-fill/home.svg", "outlined/home.svg", "regular/bell.svg", "thin/bell.svg"},
			bodies: map[string]string{"outlined-fill/home.svg": "hf", "outlined/home.svg": "h"},
		},
		{
			// Fluent: only the named suffixes are kept, which is the subset.
			name: "unclaimed files are dropped",
			files: map[string]string{
				"access_time_24_regular.svg": "r", "access_time_24_filled.svg": "f",
				"access_time_20_regular.svg": "x", "access_time_24_light.svg": "x",
			},
			variants: []SplitVariant{{Path: "regular", Suffix: "_24_regular"}, {Path: "filled", Suffix: "_24_filled"}},
			want:     []string{"filled/access_time.svg", "regular/access_time.svg"},
		},
		{
			// Carbon: 32/ has watson-health/ and Q/ sub-folders, kept as name
			// prefixes; 16/ is read flat.
			name: "flatten_prefix keeps sub-folders as a name prefix",
			files: map[string]string{
				"32/add.svg": "a", "32/watson-health/view.svg": "w", "32/Q/barrier.svg": "q",
				"16/add.svg": "a16", "16/extra/skip.svg": "x",
			},
			variants: []SplitVariant{
				{Path: "size-32", From: "32", FlattenPrefix: true}, {Path: "size-16", From: "16"},
			},
			want: []string{"size-16/add.svg", "size-32/Q-barrier.svg", "size-32/add.svg", "size-32/watson-health-view.svg"},
		},
		{
			// The longest suffix wins: a name ending in -fill-sharp is the
			// fill-sharp variant, not the sharp one.
			name:  "longest suffix wins",
			files: map[string]string{"a-sharp.svg": "s", "a-fill-sharp.svg": "fs"},
			variants: []SplitVariant{
				{Path: "sharp", Suffix: "-sharp"}, {Path: "fill-sharp", Suffix: "-fill-sharp"},
			},
			want:   []string{"fill-sharp/a.svg", "sharp/a.svg"},
			bodies: map[string]string{"fill-sharp/a.svg": "fs", "sharp/a.svg": "s"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := tree(t, tc.files)
			if err := (SplitVariants{Variants: tc.variants}).Execute(ctx); err != nil {
				t.Fatal(err)
			}
			out, _ := ctx.FetchedPath()
			got := listTree(t, out)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("got %v\nwant %v", got, tc.want)
			}
			for name, body := range tc.bodies {
				b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != body {
					t.Errorf("%s holds %q, want %q: it came from the wrong upstream file", name, b, body)
				}
			}
		})
	}
}

// TestSplitVariantsRefuses lists the configurations and upstreams that must
// stop a run rather than write a partial or merged pack.
func TestSplitVariantsRefuses(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		variants []SplitVariant
		wantErr  string
	}{
		{
			name:     "a variant claiming nothing (upstream renamed its suffix)",
			files:    map[string]string{"bell.svg": "", "bell-outline.svg": ""},
			variants: []SplitVariant{{Path: "filled"}, {Path: "sharp", Suffix: "-sharp"}},
			wantErr:  "matched no files",
		},
		{
			name:     "two names reducing to one",
			files:    map[string]string{"32/a-b.svg": "", "32/a/b.svg": ""},
			variants: []SplitVariant{{Path: "x", From: "32", FlattenPrefix: true}},
			wantErr:  "written twice",
		},
		{
			name:     "a missing source directory",
			files:    map[string]string{"a.svg": ""},
			variants: []SplitVariant{{Path: "x", From: "nope"}},
			wantErr:  "not found",
		},
		{
			name:     "variants of one directory disagreeing on flatten_prefix",
			files:    map[string]string{"a.svg": ""},
			variants: []SplitVariant{{Path: "x", FlattenPrefix: true}, {Path: "y", Suffix: "-y"}},
			wantErr:  "disagree",
		},
		{
			name:     "two variants with one directory and suffix",
			files:    map[string]string{"a.svg": ""},
			variants: []SplitVariant{{Path: "x"}, {Path: "y"}},
			wantErr:  "only go one way",
		},
		{
			name:     "a path leaving the pack",
			files:    map[string]string{"a.svg": ""},
			variants: []SplitVariant{{Path: "../x"}},
			wantErr:  "inside the pack",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := tree(t, tc.files)
			err := (SplitVariants{Variants: tc.variants}).Execute(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestSplitVariantsLeavesTheCallerConfigAlone guards against writing through
// the shared slice: the recipe builds the transform once from the pack config.
func TestSplitVariantsLeavesTheCallerConfigAlone(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"a.svg": ""})
	vs := []SplitVariant{{Path: "/x/"}}
	if err := (SplitVariants{Variants: vs}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if vs[0].Path != "/x/" {
		t.Errorf("Path rewritten to %q", vs[0].Path)
	}
}
