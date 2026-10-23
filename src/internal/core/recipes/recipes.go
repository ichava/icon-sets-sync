// Package recipes composes a pack's pipelines.
//
// A recipe builds a pipeline; it does not run one. The caller runs it, which
// is what lets the TUI show the stages before anything is fetched and lets a
// dry run skip the sinks that commit.
package recipes

import (
	"fmt"
	"log/slog"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
	"github.com/ichava/icon-sets-sync/src/internal/core/sources"
	"github.com/ichava/icon-sets-sync/src/internal/core/transforms"
)

// Recipe names a pack config's `recipe` field may carry.
const (
	RecipeAuto      = "auto"
	RecipeGeneric   = "generic"
	RecipeEmojiSets = "emoji-sets"
	RecipeBundled   = "bundled"
)

// Options carries what a recipe needs beyond the pack config.
type Options struct {
	// DryRun drops the committing sinks: the version stamp and the git branch.
	// The filesystem sinks still run, so a dry run shows the real diff in the
	// pack's working tree.
	DryRun bool

	// ConfigDir is where the bundled pack's set manifest lives.
	ConfigDir string

	// Settings is this pack's resolved settings. Nil means the defaults.
	Settings *settings.Resolved

	// Git overrides the git/gh runner, for tests. Nil means real subprocesses.
	Git gitx.Runner

	// Logger receives every pipeline's and every stage's log lines. Nil means
	// slog.Default(), which is what the CLI wants; a server running several
	// syncs at once passes one per run so their lines stay apart.
	Logger *slog.Logger

	// Observer is told when each stage starts and finishes. May be nil.
	Observer pipeline.Observer
}

func (o Options) resolved() settings.Resolved {
	if o.Settings == nil {
		return settings.Resolve()
	}
	return *o.Settings
}

// Build returns the pipelines for a pack, in the order they must run.
//
// Dispatch is on the pack's `recipe` field when it names one, else on the pack
// NAME first and the source type second -- reversing those breaks
// icon-sets-emoji, which declares source.type "url" because that is how its
// *version* is discovered while its assets come from three separate upstreams
// that only the named recipe knows about.
func Build(pack *config.PackConfig, version string, opts Options) ([]*pipeline.Pipeline, error) {
	built, err := build(pack, version, opts)
	if err != nil {
		return nil, err
	}
	for _, p := range built {
		p.Logger(opts.Logger).Observer(opts.Observer)
	}
	return built, nil
}

func build(pack *config.PackConfig, version string, opts Options) ([]*pipeline.Pipeline, error) {
	switch Select(pack) {
	case RecipeEmojiSets:
		return buildEmojiSets(pack, version, opts)

	case RecipeBundled:
		// A native per-set refresh, not the `iconify` one the pack config
		// asks for -- see buildBundled for why that would have been a rewrite
		// rather than a refresh.
		return buildBundled(pack, opts)

	case RecipeGeneric:
		p, err := buildGeneric(pack, version, opts)
		if err != nil {
			return nil, err
		}
		return []*pipeline.Pipeline{p}, nil
	}

	return nil, fmt.Errorf("%s: no recipe wired for source.type=%q; set `recipe`, or use an npm or github-* source",
		pack.Pack, pack.Source.Type)
}

// Select names the recipe a pack runs, or "" when none applies.
func Select(pack *config.PackConfig) string {
	switch pack.Recipe {
	case RecipeGeneric, RecipeEmojiSets, RecipeBundled:
		return pack.Recipe
	}
	switch {
	case pack.Name == "icon-sets-emoji":
		return RecipeEmojiSets
	case pack.Name == "icon-sets-bundled":
		return RecipeBundled
	case fetchable(pack.Source.Type):
		return RecipeGeneric
	}
	return ""
}

// packConfig is the config overlay every recipe supplies to its stages.
func packConfig(pack *config.PackConfig) map[string]any {
	old := pack.CurrentVersion
	if vendored := config.ReadVendoredVersion(pack); vendored != "" {
		// Prefer what the pack actually ships, so the commit message names the
		// version being replaced rather than this repository's stale fallback.
		old = vendored
	}
	if old == "" {
		old = "unknown"
	}
	return map[string]any{
		pipeline.ConfigPack:       pack.Pack,
		pipeline.ConfigPackRoot:   pack.PackRoot,
		pipeline.ConfigOldVersion: old,
	}
}

// gitBranch builds the committing sink from a git-branch sink config and the
// resolved settings.
//
// A field written on the sink is a pack-level value: it beats the default, and
// gives way to the global config, the environment and flags -- the same
// precedence as the pack's own `settings` block. So a scheduled run can be
// pointed at another base branch with ICHAVA_GIT_BASE without editing four
// pack configs.
func gitBranch(pack *config.PackConfig, sink *config.SinkConfig, opts Options) sinks.GitBranch {
	r := opts.resolved()
	if sink == nil {
		sink = &config.SinkConfig{Type: "git-branch"}
	}
	pick := func(key, onSink string) string {
		if onSink != "" && !r.FromAbove(key) {
			return onSink
		}
		return r.String(key)
	}
	openPR := r.Bool("git.open_pr")
	if sink.OpenPR != nil && !r.FromAbove("git.open_pr") {
		openPR = *sink.OpenPR
	}

	git := gitx.New()
	git.Author = r.String("git.author")
	if opts.Git != nil {
		git.Run = opts.Git
	}

	return sinks.GitBranch{
		RepoRoot:      orDefault(sink.RepoRoot, pack.PackRoot),
		Branch:        pick("git.branch", sink.Branch),
		CommitSubject: pick("git.commit_subject", sink.CommitSubject),
		CommitBody:    pick("git.commit_body", sink.CommitBody),
		Base:          pick("git.base", sink.Base),
		OpenPR:        openPR,
		Git:           git,
	}
}

// firstSink returns the pack's first sink of a type, or nil.
func firstSink(pack *config.PackConfig, typ string) *config.SinkConfig {
	for i := range pack.Sinks {
		if pack.Sinks[i].Type == typ {
			return &pack.Sinks[i]
		}
	}
	return nil
}

// buildEmojiSets composes the three-upstream emoji pack.
//
// Twemoji from npm, OpenMoji colour and black from one GitHub archive each,
// all categorised against the same Unicode taxonomy, then a fourth
// commit-only pipeline so the three asset runs land in a single commit rather
// than three.
//
// The Unicode and OpenMoji versions are settings (emoji.unicode_version,
// emoji.openmoji_version), defaulting to releases that exist. unicode_version
// once defaulted ahead of what Unicode had published, so every taxonomy fetch
// 404'd and the pack shipped a tagged release containing zero SVGs.
func buildEmojiSets(pack *config.PackConfig, twemojiVersion string, opts Options) ([]*pipeline.Pipeline, error) {
	if twemojiVersion == "" {
		return nil, fmt.Errorf("emoji-sets: a twemoji version is required")
	}
	r := opts.resolved()
	unicodeVersion := r.String("emoji.unicode_version")
	openmojiVersion := r.String("emoji.openmoji_version")

	filesRoot := pack.PackRoot + "/resources/assets/svg/files"
	cfg := packConfig(pack)
	cldr := sources.UnicodeCldr{UnicodeVersion: unicodeVersion}

	twemoji := pipeline.Named(fmt.Sprintf("emoji-sets:twemoji@%s", twemojiVersion)).
		Config(cfg).
		Source(sources.Compound{
			First:  cldr,
			Second: sources.NpmTarball{Package: "@twemoji/svg", Version: twemojiVersion},
		}).
		// Twemoji ships a class on every path; it is the one upstream where
		// stripping it is wanted, so the pack's own styling applies.
		Transform(transforms.Sanitise{AlsoStripClass: true}).
		Transform(transforms.Categorise{By: "cldr"}).
		Transform(transforms.Indexer{Targets: []string{"codepoints", "names"}}).
		Sink(sinks.Filesystem{Root: filesRoot + "/twemoji"})

	openmoji := func(name, subdir string) *pipeline.Pipeline {
		return pipeline.Named(fmt.Sprintf("emoji-sets:%s@%s", name, openmojiVersion)).
			Config(cfg).
			Source(sources.Compound{
				First: cldr,
				Second: sources.GithubArchive{
					// OpenMoji tags without a `v`, so the built-in template
					// would ask for a tag that does not exist.
					ArchiveURL: "https://github.com/hfg-gmuend/openmoji/archive/refs/tags/{version}.zip",
					Owner:      "hfg-gmuend", Repo: "openmoji",
					Version: openmojiVersion,
				},
			}).
			Transform(transforms.SubsetTo{Subdir: subdir}).
			Transform(transforms.Sanitise{}).
			Transform(transforms.Categorise{By: "cldr"}).
			Sink(sinks.Filesystem{Root: filesRoot + "/" + name})
	}

	pipelines := []*pipeline.Pipeline{
		twemoji,
		openmoji("openmoji-color", "color/svg"),
		openmoji("openmoji-black", "black/svg"),
	}

	if !opts.DryRun {
		pipelines = append(pipelines,
			pipeline.Named("emoji-sets:commit").
				Config(cfg).
				// Carries the twemoji version forward. Each pipeline has its
				// own context, so without this the stamp finds no version and
				// silently does nothing -- which is what the Python did, and
				// why this pack never converged.
				Source(sources.Static{Version: twemojiVersion}).
				Sink(sinks.VersionStamp{Pack: pack}).
				Sink(gitBranch(pack, firstSink(pack, "git-branch"), opts)))
	}
	return pipelines, nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
