package transforms

import (
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// TestExclude covers the three ways a pack keeps an upstream icon out.
func TestExclude(t *testing.T) {
	simpleIcons := `[
		{"slug": "dotnet", "license": {"type": "CC0-1.0"}},
		{"slug": "angular", "license": {"type": "CC-BY-4.0"}},
		{"slug": "git", "license": {"type": "CC-BY-3.0"}},
		{"slug": "github"}
	]`

	cases := []struct {
		name  string
		files map[string]string
		t     Exclude
		want  []string
	}{
		{
			// Octicons: GitHub's marks, in every size directory.
			name: "names, wherever they occur",
			files: map[string]string{
				"16px/mark-github.svg": "", "24px/mark-github.svg": "", "16px/bell.svg": "", "24px/bell.svg": "",
			},
			t:    Exclude{Names: []string{"mark-github", "hubot"}},
			want: []string{"16px/bell.svg", "24px/bell.svg"},
		},
		{
			// Game Icons: directories whose licence is not stated.
			name: "directories, at any depth",
			files: map[string]string{
				"lorc/axe.svg": "", "badges/anvil.svg": "", "deep/various-artists/x.svg": "",
			},
			t:    Exclude{Dirs: []string{"badges", "various-artists"}},
			want: []string{"lorc/axe.svg"},
		},
		{
			// Simple Icons: the data file says which logos are not CC0. An
			// entry with no licence is covered by the upstream's own CC0.
			name: "metadata, read from the upstream root",
			files: map[string]string{
				"package/data/simple-icons.json": simpleIcons,
				"package/icons/dotnet.svg":       "", "package/icons/angular.svg": "",
				"package/icons/git.svg": "", "package/icons/github.svg": "",
			},
			t: Exclude{Metadata: &ExcludeMetadata{
				File: "data/simple-icons.json", Name: "slug", Field: "license.type", Allow: []string{"CC0-1.0"},
			}},
			want: []string{"data/simple-icons.json", "icons/dotnet.svg", "icons/github.svg"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, root := tree(t, tc.files)
			if strings.HasPrefix(tc.name, "metadata") {
				ctx.SetFetchedPath(root + "/package")
				// FindLicences records the root before source_path narrows it.
				ctx.SetString(pipeline.KeyUpstreamRoot, root+"/package")
				ctx.SetFetchedPath(root + "/package/icons")
			}
			if err := tc.t.Execute(ctx); err != nil {
				t.Fatal(err)
			}
			got := listTree(t, root)
			if strings.HasPrefix(tc.name, "metadata") {
				got = listTree(t, root+"/package")
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("got %v\nwant %v", got, tc.want)
			}
		})
	}
}

// TestExcludeRefuses: each of these would let an icon through, or exclude
// nothing while looking like it worked.
func TestExcludeRefuses(t *testing.T) {
	meta := func(data string) (*pipeline.Context, Exclude) {
		ctx, root := tree(t, map[string]string{"data.json": data, "icons/a.svg": ""})
		ctx.SetString(pipeline.KeyUpstreamRoot, root)
		ctx.SetFetchedPath(root + "/icons")
		return ctx, Exclude{Metadata: &ExcludeMetadata{File: "data.json", Name: "slug", Field: "license", Allow: []string{"CC0-1.0"}}}
	}

	cases := []struct {
		name    string
		run     func() error
		wantErr string
	}{
		{"no rule at all", func() error {
			ctx, _ := tree(t, map[string]string{"a.svg": ""})
			return Exclude{}.Execute(ctx)
		}, "drop nothing"},
		{"an empty data file", func() error {
			ctx, ex := meta(`[]`)
			return ex.Execute(ctx)
		}, "lists no items"},
		{"an excluded item with no name", func() error {
			ctx, ex := meta(`[{"license": "MIT"}]`)
			return ex.Execute(ctx)
		}, "no slug"},
		{"a data file that is not an array", func() error {
			ctx, ex := meta(`{"icons": []}`)
			return ex.Execute(ctx)
		}, "JSON array"},
		{"a data file outside the upstream", func() error {
			ctx, ex := meta(`[]`)
			ex.Metadata.File = "../../etc/passwd"
			return ex.Execute(ctx)
		}, "outside the upstream"},
		{"no upstream root recorded", func() error {
			ctx, _ := tree(t, map[string]string{"a.svg": ""})
			return Exclude{Metadata: &ExcludeMetadata{File: "d.json", Name: "n", Field: "f"}}.Execute(ctx)
		}, "upstream_root not set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}
