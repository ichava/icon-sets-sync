package recipes

import (
	"encoding/json"
	"fmt"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/settings"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sinks"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sources"
	"github.com/ichava/maintainer-toolkit/src/internal/core/transforms"
)

// Emoji recipe defaults, kept under their old names. The settings registry
// owns the values now; these alias it so nothing reading the old constants
// can see a different number.
const (
	DefaultOpenMojiVersion = settings.DefaultOpenMojiVersion
	DefaultUnicodeVersion  = settings.DefaultUnicodeVersion
)

// fetchable reports whether a source type has an asset fetch strategy.
//
// url and script are version-discovery types only: a url source names an
// endpoint to poll, not a tree to download, and script was declared by the
// Python schema and never implemented by any runtime.
func fetchable(sourceType string) bool {
	switch sourceType {
	case "npm", "github-archive", "github-tag", "github-release":
		return true
	}
	return false
}

// buildGeneric composes one pipeline from the pack config alone: its source,
// its transforms, its sinks.
//
// With no `transforms` declared it is exactly the Python simple_npm recipe --
// fetch, narrow to source_path, sanitise, copy, stamp, commit -- which is what
// icon-sets-tabler and icon-sets-flag run. Declaring transforms replaces the
// default sanitise step with the declared list, in order.
func buildGeneric(pack *config.PackConfig, version string, opts Options) (*pipeline.Pipeline, error) {
	src, err := assetSource(pack.Source, version)
	if err != nil {
		return nil, err
	}

	declared, needsCLDR, err := buildTransforms(pack.Transforms)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pack.Pack, err)
	}
	if needsCLDR {
		// The categoriser reads the taxonomy from the context, so the
		// taxonomy fetch runs first and the asset source owns fetched_path.
		src = sources.Compound{
			First:  sources.UnicodeCldr{UnicodeVersion: opts.resolved().String("emoji.unicode_version")},
			Second: src,
		}
	}

	p := pipeline.Named(fmt.Sprintf("%s@%s", pack.Name, version)).
		Config(packConfig(pack)).
		Source(src)

	if pack.Source.SourcePath != "" {
		p.Transform(transforms.SubsetTo{Subdir: pack.Source.SourcePath})
	}
	if len(declared) == 0 {
		p.Transform(transforms.Sanitise{})
	}
	for _, t := range declared {
		p.Transform(t)
	}

	added := 0
	for i := range pack.Sinks {
		sink := &pack.Sinks[i]
		switch sink.Type {
		case "filesystem":
			p.Sink(sinks.Filesystem{
				Root:         sink.Root,
				Incremental:  sink.Incremental,
				MinRetention: sink.MinRetention,
			})
			added++

		case "git-branch":
			if opts.DryRun {
				continue
			}
			// Stamp before commit, always. The version has to be in the tree
			// the commit captures, or the pack ships assets whose recorded
			// version is the previous one and every later run re-proposes the
			// same refresh.
			p.Sink(sinks.VersionStamp{Pack: pack})
			p.Sink(gitBranch(pack, sink, opts))
			added += 2
		}
	}

	if added == 0 {
		if !opts.DryRun {
			return nil, fmt.Errorf("%s: declares no sinks, so a sync would fetch and then write nothing; add a filesystem and a git-branch sink, or pass --dry-run", pack.Pack)
		}
		// A dry run of a sink-less pack still has to run end to end to be
		// worth anything, so it counts what it would have written instead.
		p.Sink(sinks.Discard{})
	}
	return p, nil
}

// assetSource builds the fetch stage for a source config.
func assetSource(src config.SourceConfig, version string) (pipeline.Source, error) {
	switch src.Type {
	case "npm":
		if src.Package == "" {
			return nil, fmt.Errorf("npm source: source.package is required")
		}
		return sources.NpmTarball{Package: src.Package, Version: version}, nil

	case "github-archive", "github-tag", "github-release":
		// All three fetch the tagged archive. They differ in how the latest
		// version is discovered, which is the checker's business, not this.
		if src.ArchiveURL == "" && (src.Owner == "" || src.Repo == "") {
			return nil, fmt.Errorf("%s source: needs archive_url, or owner and repo", src.Type)
		}
		return sources.GithubArchive{
			ArchiveURL: src.ArchiveURL, Owner: src.Owner, Repo: src.Repo, Version: version,
		}, nil
	}
	return nil, fmt.Errorf("source.type=%q has no fetch strategy; set `recipe`, or use an npm or github-* source", src.Type)
}

// transformConfig is the union of every transform's options. The schema has
// already rejected a key that does not belong to the declared type.
type transformConfig struct {
	Type string `json:"type"`

	Subdir string `json:"subdir"`

	AlsoStripClass bool `json:"also_strip_class"`
	Strict         bool `json:"strict"`
	ReportOnly     bool `json:"report_only"`

	By string `json:"by"`

	Targets []string `json:"targets"`

	Variants []struct {
		Path   string `json:"path"`
		Prefix string `json:"prefix"`
		Suffix string `json:"suffix"`
	} `json:"variants"`
	Separator string `json:"separator"`
}

// buildTransforms turns the pack's declared transforms into stages, and says
// whether any of them needs the Unicode taxonomy.
func buildTransforms(raw []map[string]any) ([]pipeline.Transform, bool, error) {
	var out []pipeline.Transform
	needsCLDR := false

	for i, m := range raw {
		data, err := json.Marshal(m)
		if err != nil {
			return nil, false, err
		}
		var tc transformConfig
		if err := json.Unmarshal(data, &tc); err != nil {
			return nil, false, fmt.Errorf("transforms[%d]: %w", i, err)
		}

		switch tc.Type {
		case "subset":
			out = append(out, transforms.SubsetTo{Subdir: tc.Subdir})
		case "sanitise":
			out = append(out, transforms.Sanitise{
				AlsoStripClass: tc.AlsoStripClass, Strict: tc.Strict, ReportOnly: tc.ReportOnly,
			})
		case "slugify":
			out = append(out, transforms.Slugify{})
		case "categorise":
			by := orDefault(tc.By, "passthrough")
			if by == "cldr" {
				needsCLDR = true
			}
			out = append(out, transforms.Categorise{By: by})
		case "indexer":
			out = append(out, transforms.Indexer{Targets: tc.Targets})
		case "flatten-variants":
			vs := make([]transforms.Variant, len(tc.Variants))
			for j, v := range tc.Variants {
				vs[j] = transforms.Variant{Subdir: v.Path, Prefix: v.Prefix, Suffix: v.Suffix}
			}
			out = append(out, transforms.FlattenVariants{Variants: vs, Separator: tc.Separator})
		default:
			return nil, false, fmt.Errorf("transforms[%d]: unknown type %q", i, tc.Type)
		}
	}
	return out, needsCLDR, nil
}
