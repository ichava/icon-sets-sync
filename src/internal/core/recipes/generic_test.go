package recipes

import (
	"strings"
	"testing"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/gitx"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/settings"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sinks"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sources"
	"github.com/ichava/maintainer-toolkit/src/internal/core/transforms"
)

// The settings registry duplicates these defaults to stay free of the
// pipeline layer. They must be the same strings, or a pack with no settings
// would commit differently from before the registry existed.
func TestSettingsDefaultsMatchTheirSources(t *testing.T) {
	pairs := []struct{ name, got, want string }{
		{"author", settings.DefaultAuthor, gitx.DefaultAuthor},
		{"branch", settings.DefaultBranch, sinks.DefaultBranch},
		{"subject", settings.DefaultCommitSubject, sinks.DefaultCommitSubject},
		{"body", settings.DefaultCommitBody, sinks.DefaultCommitBody},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("%s: settings %q, source %q", p.name, p.got, p.want)
		}
	}
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name   string
		pack   config.PackConfig
		expect string
	}{
		{"npm is generic", config.PackConfig{Name: "icon-sets-tabler", Source: config.SourceConfig{Type: "npm"}}, RecipeGeneric},
		{"github-release is generic", config.PackConfig{Name: "x", Source: config.SourceConfig{Type: "github-release"}}, RecipeGeneric},
		{"emoji by name", config.PackConfig{Name: "icon-sets-emoji", Source: config.SourceConfig{Type: "url"}}, RecipeEmojiSets},
		{"bundled by name", config.PackConfig{Name: "icon-sets-bundled", Source: config.SourceConfig{Type: "url"}}, RecipeBundled},
		{"url has none", config.PackConfig{Name: "x", Source: config.SourceConfig{Type: "url"}}, ""},
		{"script has none", config.PackConfig{Name: "x", Source: config.SourceConfig{Type: "script"}}, ""},
		{"explicit recipe wins over name", config.PackConfig{Name: "icon-sets-emoji", Recipe: RecipeGeneric, Source: config.SourceConfig{Type: "npm"}}, RecipeGeneric},
		{"auto is the default", config.PackConfig{Name: "icon-sets-emoji", Recipe: RecipeAuto}, RecipeEmojiSets},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Select(&tt.pack); got != tt.expect {
				t.Errorf("Select = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestGenericStagesFollowTheConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(p *config.PackConfig)
		dryRun bool
		want   string
	}{
		{
			name:   "no transforms is the simple npm recipe",
			mutate: func(*config.PackConfig) {},
			want:   "NpmTarball,SubsetTo,Sanitise,Filesystem,VersionStamp,GitBranch",
		},
		{
			name: "declared transforms replace the default sanitise, in order",
			mutate: func(p *config.PackConfig) {
				p.Transforms = []map[string]any{
					{"type": "slugify"},
					{"type": "sanitise", "also_strip_class": true},
				}
			},
			want: "NpmTarball,SubsetTo,Slugify,Sanitise,Filesystem,VersionStamp,GitBranch",
		},
		{
			name: "a cldr categoriser pulls in the taxonomy source",
			mutate: func(p *config.PackConfig) {
				p.Transforms = []map[string]any{{"type": "categorise", "by": "cldr"}, {"type": "indexer"}}
			},
			want: "Compound(UnicodeCldr,NpmTarball),SubsetTo,Categorise,Indexer,Filesystem,VersionStamp,GitBranch",
		},
		{
			name: "a github archive source",
			mutate: func(p *config.PackConfig) {
				p.Source = config.SourceConfig{Type: "github-release", Owner: "o", Repo: "r"}
			},
			want: "GithubArchive,Sanitise,Filesystem,VersionStamp,GitBranch",
		},
		{
			name:   "dry run keeps the filesystem sink only",
			mutate: func(*config.PackConfig) {},
			dryRun: true,
			want:   "NpmTarball,SubsetTo,Sanitise,Filesystem",
		},
		{
			name: "a dry run of a commit-only pack counts instead of failing",
			mutate: func(p *config.PackConfig) {
				p.Sinks = p.Sinks[1:]
			},
			dryRun: true,
			want:   "NpmTarball,SubsetTo,Sanitise,Discard",
		},
		{
			name: "flatten-variants",
			mutate: func(p *config.PackConfig) {
				p.Source.SourcePath = ""
				p.Transforms = []map[string]any{{
					"type":     "flatten-variants",
					"variants": []any{map[string]any{"path": "24/solid", "prefix": "s-"}},
				}}
			},
			want: "NpmTarball,FlattenVariants,Filesystem,VersionStamp,GitBranch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack := npmPack("icon-sets-demo")
			tt.mutate(pack)
			built, err := Build(pack, "1.2.3", Options{DryRun: tt.dryRun})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(stageNames(built[0]), ","); got != tt.want {
				t.Errorf("stages = %s\nwant     %s", got, tt.want)
			}
		})
	}
}

func TestGenericRefusesNoSinksOutsideADryRun(t *testing.T) {
	pack := npmPack("icon-sets-demo")
	pack.Sinks = nil
	_, err := Build(pack, "1.0.0", Options{})
	if err == nil || !strings.Contains(err.Error(), "declares no sinks") {
		t.Fatalf("err = %v, want a refusal naming the missing sinks", err)
	}
	if _, err := Build(pack, "1.0.0", Options{DryRun: true}); err != nil {
		t.Fatalf("a dry run of the same pack should build: %v", err)
	}
}

func TestGenericSourceErrors(t *testing.T) {
	tests := []struct {
		src     config.SourceConfig
		wantErr string
	}{
		{config.SourceConfig{Type: "github-archive"}, "owner and repo"},
		{config.SourceConfig{Type: "npm"}, "source.package"},
	}
	for _, tt := range tests {
		t.Run(tt.src.Type, func(t *testing.T) {
			pack := npmPack("icon-sets-demo")
			pack.Source = tt.src
			if _, err := Build(pack, "1.0.0", Options{}); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// findGitBranch returns the git sink of a built pipeline.
func findGitBranch(t *testing.T, p *pipeline.Pipeline) sinks.GitBranch {
	t.Helper()
	for _, s := range p.Stages() {
		if g, ok := s.(sinks.GitBranch); ok {
			return g
		}
	}
	t.Fatalf("no GitBranch in %v", stageNames(p))
	return sinks.GitBranch{}
}

// A field on the git-branch sink is a pack-level value: it beats the default
// and loses to the global config, the environment and flags.
func TestGitBranchFieldsFollowThePrecedence(t *testing.T) {
	no := false
	tests := []struct {
		name       string
		sinkBranch string
		sinkOpenPR *bool
		layer      settings.Layer
		value      settings.Values
		wantBranch string
		wantOpenPR bool
	}{
		{"default when nothing is set", "", nil, settings.LayerDefault, nil, settings.DefaultBranch, true},
		{"sink field beats default", "chore/from-sink", nil, settings.LayerDefault, nil, "chore/from-sink", true},
		{"pack settings lose to the sink field", "chore/from-sink", nil, settings.LayerPack,
			settings.Values{"git.branch": "chore/from-pack"}, "chore/from-sink", true},
		{"global beats the sink field", "chore/from-sink", nil, settings.LayerGlobal,
			settings.Values{"git.branch": "chore/from-global"}, "chore/from-global", true},
		{"env beats the sink field", "chore/from-sink", nil, settings.LayerEnv,
			settings.Values{"git.branch": "chore/from-env"}, "chore/from-env", true},
		{"flag beats the sink field", "chore/from-sink", nil, settings.LayerFlag,
			settings.Values{"git.branch": "chore/from-flag"}, "chore/from-flag", true},
		{"sink open_pr false is honoured", "", &no, settings.LayerDefault, nil, settings.DefaultBranch, false},
		{"a flag re-enables the pull request", "", &no, settings.LayerFlag,
			settings.Values{"git.open_pr": true}, settings.DefaultBranch, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack := npmPack("icon-sets-demo")
			pack.Sinks[1].Branch = tt.sinkBranch
			pack.Sinks[1].OpenPR = tt.sinkOpenPR

			r := settings.Resolve(settings.LayerValues{Layer: tt.layer, Values: tt.value})
			built, err := Build(pack, "1.0.0", Options{Settings: &r})
			if err != nil {
				t.Fatal(err)
			}
			g := findGitBranch(t, built[0])
			if g.Branch != tt.wantBranch {
				t.Errorf("Branch = %q, want %q", g.Branch, tt.wantBranch)
			}
			if g.OpenPR != tt.wantOpenPR {
				t.Errorf("OpenPR = %v, want %v", g.OpenPR, tt.wantOpenPR)
			}
		})
	}
}

func TestGitBranchCarriesTheResolvedGitSettings(t *testing.T) {
	r := settings.Resolve(settings.LayerValues{Layer: settings.LayerEnv, Values: settings.Values{
		"git.base":   "trunk",
		"git.author": "Robot <robot@example.com>",
	}})
	built, err := Build(npmPack("icon-sets-demo"), "1.0.0", Options{Settings: &r})
	if err != nil {
		t.Fatal(err)
	}
	g := findGitBranch(t, built[0])
	if g.Base != "trunk" || g.Git == nil || g.Git.Author != "Robot <robot@example.com>" {
		t.Fatalf("git sink = %+v (author %q)", g, g.Git.Author)
	}
	if g.CommitSubject != settings.DefaultCommitSubject {
		t.Errorf("CommitSubject = %q", g.CommitSubject)
	}
}

func TestEmojiVersionsComeFromSettings(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-emoji", Pack: "ichava/icon-sets-emoji", PackRoot: "/work/icon-sets-emoji",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
		Sinks:  []config.SinkConfig{{Type: "git-branch", Branch: "chore/emoji-{version}"}},
	}
	r := settings.Resolve(settings.LayerValues{Layer: settings.LayerFlag, Values: settings.Values{
		"emoji.unicode_version":  "15.1",
		"emoji.openmoji_version": "15.0.0",
	}})
	built, err := Build(emoji, "15.0.0", Options{Settings: &r})
	if err != nil {
		t.Fatal(err)
	}
	if got := built[1].Name(); got != "emoji-sets:openmoji-color@15.0.0" {
		t.Errorf("openmoji pipeline = %q, want the settings version", got)
	}
	compound, ok := built[0].Stages()[0].(sources.Compound)
	if !ok {
		t.Fatalf("first stage is %T", built[0].Stages()[0])
	}
	if cldr, _ := compound.First.(sources.UnicodeCldr); cldr.UnicodeVersion != "15.1" {
		t.Errorf("unicode version = %q, want 15.1", cldr.UnicodeVersion)
	}
	if g := findGitBranch(t, built[3]); g.Branch != "chore/emoji-{version}" {
		t.Errorf("emoji commit ignores the config's git-branch sink: %q", g.Branch)
	}
}

func TestBuildTransformsRejectsAnUnknownType(t *testing.T) {
	if _, _, err := buildTransforms([]map[string]any{{"type": "zip"}}); err == nil {
		t.Fatal("want an error")
	}
	got, cldr, err := buildTransforms([]map[string]any{{"type": "categorise"}})
	if err != nil || cldr {
		t.Fatalf("passthrough categorise needs no taxonomy: %v %v", cldr, err)
	}
	if c, ok := got[0].(transforms.Categorise); !ok || c.By != "passthrough" {
		t.Fatalf("got %#v", got[0])
	}
}
