// Package sinks writes the in-flight tree to its destination.
//
// Order matters and is set by the recipes: Filesystem, then VersionStamp, then
// GitBranch. Stamping after the commit would record a version that never
// shipped, and stamping into this repository rather than the pack repo is what
// made the sync non-convergent before (V49) -- the bump went into the runner's
// throwaway checkout and every run saw the same stale value.
package sinks

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/ichava/icon-sets-sync/src/internal/core/text"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// Filesystem copies the refreshed SVGs into the pack repo.
type Filesystem struct {
	pipeline.SinkKind

	// Root is the destination, with {pack_root} and {version} interpolated.
	Root string

	// Incremental keeps what is already there. Off by default, which means
	// the destination is wiped first -- that is what removes an icon upstream
	// has deleted. An incremental sink would leave it in the pack forever.
	Incremental bool

	// MinRetention refuses to replace a populated destination with
	// substantially fewer files, as a fraction of what is already there.
	// Zero disables the check.
	//
	// Because the wipe is the point, it is also the hazard. Observed while
	// building the aggregator refresh: iconoir's upstream reorganised into
	// per-weight directories, so a refresh matching one of them would have
	// replaced 1,671 committed icons with 1,383 -- deleting 288 without
	// anything failing. A pack losing a sixth of a set to an upstream
	// reshuffle should stop the run and ask, not proceed quietly on a Monday
	// morning cron.
	MinRetention float64

	// RefuseRemovals fails the sink, before the wipe, when any icon the
	// destination ships today is absent from the incoming tree. MinRetention
	// only catches a large shrink by count; this catches one icon, and a swap
	// of one for another that leaves the count unchanged. Off by default.
	RefuseRemovals bool

	// Force lets RefuseRemovals through, for an upstream that really did retire
	// icons. The removals are logged either way.
	Force bool

	Log *slog.Logger
}

func (Filesystem) Name() string { return "Filesystem" }

func (s Filesystem) Execute(ctx *pipeline.Context) error {
	source, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Filesystem sink: fetched_path not set; needs a source upstream")
	}

	version, _ := ctx.FetchedVersion()
	target := config.Interpolate(s.Root, map[string]string{
		"version":   version,
		"pack_root": ctx.ConfigString(pipeline.ConfigPackRoot),
	})
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	log := s.Log
	if log == nil {
		log = ctx.Logger()
	}

	// Hashed before the wipe, so the diff is what this refresh changes, and a
	// dry run, which still runs this sink, reports it too.
	before, err := hashSVGs(target)
	if err != nil {
		return err
	}

	if !s.Incremental {
		if _, err := os.Stat(target); err == nil {
			err := s.guardRetention(source, target, log)
			if err == nil {
				err = s.guardRemovals(source, target, before, log)
			}
			if err != nil {
				var review *ReviewError
				if !ctx.DryRun() || !errors.As(err, &review) {
					return err
				}
				// A dry run reports, it does not fail: it records every icon a
				// real run would refuse, and the run reads "needs review".
				ctx.Metric("review", review.metric())
				log.Warn("filesystem: a real run would stop for review", "target", target,
					"renamed", review.Renamed(), "removed", review.Removed(), "reason", review.Reason)
			}
		}
	}

	if ctx.DryRun() {
		// Report the diff a real run would make and write nothing: the
		// fetched tree already sits in the pipeline's temporary working
		// directory, and that is the only place a dry run may write. The
		// guards above still run, and what they would refuse is recorded
		// as a review rather than failing the dry run.
		return s.reportDryRun(ctx, source, target, before, log)
	}

	if !s.Incremental {
		if _, err := os.Stat(target); err == nil {
			log.Info("filesystem: wiping", "target", target)
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	var files []string
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".svg") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)

	copied := 0
	for _, src := range files {
		rel, err := filepath.Rel(source, src)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyFile(src, dest); err != nil {
			return err
		}
		copied++
	}

	after, err := hashSVGs(source)
	if err != nil {
		return err
	}
	d := diffTrees(before, after, !s.Incremental)

	ctx.Metric("filesystem", map[string]any{
		"copied": copied, "target": target,
		"added": len(d.added), "removed": len(d.removed), "changed": len(d.changed),
		"added_names": capNames(d.added), "removed_names": capNames(d.removed), "changed_names": capNames(d.changed),
	})
	ctx.SetString(pipeline.KeyFilesystemTgt, target)
	log.Info("filesystem", "copied", copied, "target", target)
	return nil
}

// reportDryRun records the same metric a real run does, with copied 0 and
// would_write the number of SVGs a real run would copy.
func (s Filesystem) reportDryRun(ctx *pipeline.Context, source, target string, before map[string][32]byte, log *slog.Logger) error {
	after, err := hashSVGs(source)
	if err != nil {
		return err
	}
	d := diffTrees(before, after, !s.Incremental)
	ctx.Metric("filesystem", map[string]any{
		"copied": 0, "would_write": len(after), "dry_run": true, "target": target,
		"added": len(d.added), "removed": len(d.removed), "changed": len(d.changed),
		"added_names": capNames(d.added), "removed_names": capNames(d.removed), "changed_names": capNames(d.changed),
	})
	ctx.SetString(pipeline.KeyFilesystemTgt, target)
	log.Info("filesystem: dry run, pack left untouched", "would_write", len(after), "target", target,
		"added", len(d.added), "removed", len(d.removed), "changed", len(d.changed))
	return nil
}

// guardRetention refuses a wipe that would shrink the destination too far.
//
// Counted before anything is removed, so a refusal leaves the pack exactly as
// it was.
func (s Filesystem) guardRetention(source, target string, log *slog.Logger) error {
	if s.MinRetention <= 0 {
		return nil
	}

	existing, err := countSVGs(target)
	if err != nil || existing == 0 {
		return nil //nolint:nilerr // nothing to protect
	}
	incoming, err := countSVGs(source)
	if err != nil {
		return err
	}

	ratio := float64(incoming) / float64(existing)
	if ratio >= s.MinRetention {
		if incoming < existing {
			// A small shrink is ordinary -- upstreams retire icons -- but it
			// is worth saying out loud, because nothing else in the run will.
			log.Warn("filesystem: destination shrinks",
				"target", target, "from", existing, "to", incoming)
		}
		return nil
	}

	return &ReviewError{Target: target, Reason: fmt.Sprintf(
		"refusing to replace %d icons in %s with %d (%.0f%% of what is there, floor is %.0f%%): "+
			"upstream has probably reorganised, so check the manifest path before letting this through",
		existing, target, incoming, ratio*100, s.MinRetention*100)}
}

// guardRemovals refuses a wipe that would delete a shipped icon. Compared by
// path relative to the root, which is how a pack addresses an icon; an icon
// whose bytes reappear at another path is reported as renamed to it.
func (s Filesystem) guardRemovals(source, target string, before map[string][32]byte, log *slog.Logger) error {
	if !s.RefuseRemovals {
		return nil
	}
	incoming, err := hashSVGs(source)
	if err != nil {
		return err
	}
	gone := removals(before, incoming)
	if len(gone) == 0 {
		return nil
	}
	review := &ReviewError{Target: target, Removals: gone}
	if s.Force {
		log.Warn("filesystem: removing shipped icons because the run is forced",
			"target", target, "renamed", review.Renamed(), "removed", review.Removed())
		return nil
	}
	return review
}

func countSVGs(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".svg") {
			n++
		}
		return nil
	})
	return n, err
}

// copyFile copies contents and mode. Only .svg files reach it -- anything else
// in the upstream tree is deliberately not carried into the pack.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// VersionStamp records the fetched version in the pack repo's own config.
//
// Into the pack repo, never back into this one. The GitBranch sink commits it
// alongside the assets, so the version and the SVGs it describes move in one
// commit and the next run compares against what actually shipped.
type VersionStamp struct {
	pipeline.SinkKind

	Pack *config.PackConfig
	Log  *slog.Logger
}

func (VersionStamp) Name() string { return "VersionStamp" }

func (s VersionStamp) Execute(ctx *pipeline.Context) error {
	log := s.Log
	if log == nil {
		log = ctx.Logger()
	}

	version, ok := ctx.FetchedVersion()
	if !ok || version == "" {
		ctx.Metric("version_stamp", map[string]any{"status": "no-version"})
		return nil
	}

	path, err := config.WriteVendoredVersion(s.Pack, version)
	if err != nil {
		return err
	}
	if path == "" {
		// Not a failure: a pack with no version file simply has nothing to
		// stamp, and the sync will re-detect the same version next run.
		log.Warn("version-stamp: pack ships no version file, so the sync will re-detect",
			"pack", s.Pack.Pack, "file", s.Pack.VersionFile, "version", version)
		ctx.Metric("version_stamp", map[string]any{"status": "no-file"})
		return nil
	}

	ctx.Metric("version_stamp", map[string]any{
		"status": "stamped", "version": version, "file": path,
	})
	log.Info("version-stamp", "pack", s.Pack.Pack, "version", version, "file", path)
	return nil
}

// GitBranch commits the refreshed pack and opens a pull request.
type GitBranch struct {
	pipeline.SinkKind

	RepoRoot      string
	Branch        string
	CommitSubject string
	CommitBody    string
	Base          string
	OpenPR        bool

	Git *gitx.Git
	Log *slog.Logger
}

func (GitBranch) Name() string { return "GitBranch" }

// Defaults matching the Python templates.
const (
	DefaultBranch        = "chore/sync-upstream-{version}"
	DefaultCommitSubject = "chore(upstream): bump {pack} to v{new}"
	DefaultCommitBody    = "Automated sync from upstream {pack} {old} -> {new}.\n\n" +
		"Generated by ichava/icon-sets-sync. See docs/maintainer-sync.md in that repository."
)

func (s GitBranch) Execute(ctx *pipeline.Context) error {
	log := s.Log
	if log == nil {
		log = ctx.Logger()
	}
	g := s.Git
	if g == nil {
		g = gitx.New()
	}

	version, _ := ctx.FetchedVersion()
	pack := ctx.ConfigString(pipeline.ConfigPack)
	if pack == "" {
		pack = ctx.PipelineName
	}
	old := ctx.ConfigString(pipeline.ConfigOldVersion)
	if old == "" {
		old = "unknown"
	}

	bindings := map[string]string{
		"version":   version,
		"pack_root": ctx.ConfigString(pipeline.ConfigPackRoot),
		"pack":      pack,
		"old":       old,
		"new":       version,
	}

	repo, err := filepath.Abs(config.Interpolate(s.RepoRoot, bindings))
	if err != nil {
		return err
	}

	runCtx := ctx.Context()

	dirty, err := g.HasUncommittedChanges(runCtx, repo)
	if err != nil {
		return err
	}
	if !dirty {
		// Nothing changed. Upstream published a version whose assets are
		// byte-identical to what the pack already ships, which happens and is
		// not an error.
		log.Info("git-branch: no diff, skipping", "repo", repo)
		ctx.Metric("git_branch", map[string]any{"status": "no-diff"})
		return nil
	}

	branch := config.Interpolate(text.OrDefault(s.Branch, DefaultBranch), bindings)
	subject := config.Interpolate(text.OrDefault(s.CommitSubject, DefaultCommitSubject), bindings)
	body := config.Interpolate(text.OrDefault(s.CommitBody, DefaultCommitBody), bindings)

	// Fetch the tracking ref before pushing. actions/checkout fetches only the
	// default branch, so refs/remotes/origin/<branch> does not exist on a
	// runner -- and --force-with-lease then has nothing to compare and git
	// rejects the push as "stale info". A missing branch upstream is fine, so
	// the error is logged rather than returned.
	if err := g.FetchBranch(runCtx, repo, branch); err != nil {
		log.Debug("git-branch: no existing remote branch to fetch", "branch", branch, "error", err)
	}

	if err := g.CreateBranch(runCtx, repo, branch); err != nil {
		return err
	}
	sha, err := g.CommitAll(runCtx, repo, subject, body)
	if err != nil {
		return err
	}
	if err := g.PushBranch(runCtx, repo, branch, true); err != nil {
		return err
	}

	prURL := ""
	if s.OpenPR {
		prURL, err = g.OpenPR(runCtx, repo, branch, subject, body, s.Base)
		if err != nil {
			return err
		}
	}

	ctx.Metric("git_branch", map[string]any{
		"status": "committed", "branch": branch, "sha": sha, "pr_url": prURL,
	})
	ctx.SetString(pipeline.KeyGitBranch, branch)
	ctx.SetString(pipeline.KeyGitSHA, sha)
	if prURL != "" {
		ctx.SetString(pipeline.KeyPRURL, prURL)
	}

	short := sha
	if len(short) > 8 {
		short = short[:8]
	}
	log.Info("git-branch: committed", "sha", short, "branch", branch, "pr", prURL)
	return nil
}

// Discard counts the SVGs a pipeline would have written, and writes nothing.
//
// A pipeline needs at least one sink to run. A dry run of a pack whose only
// sink commits has none left once the committing sinks are dropped, and
// failing it there would make --dry-run useless for exactly the packs a person
// most wants to inspect before letting them commit.
type Discard struct {
	pipeline.SinkKind
}

func (Discard) Name() string { return "Discard" }

func (Discard) Execute(ctx *pipeline.Context) error {
	source, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Discard sink: fetched_path not set; needs a source upstream")
	}
	n, err := countSVGs(source)
	if err != nil {
		return err
	}
	ctx.Metric("discard", map[string]any{"would_write": n})
	return nil
}

// maxDiffNames caps each list of names the Filesystem sink reports. The
// counts are always complete; the names are a sample a person can read.
const maxDiffNames = 50

// hashSVGs maps every SVG under dir, by slash-separated relative path, to the
// sha256 of its bytes. A directory that does not exist yet is empty.
func hashSVGs(dir string) (map[string][32]byte, error) {
	out := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == dir {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".svg") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sha256.Sum256(b)
		return nil
	})
	return out, err
}

type treeDiff struct{ added, removed, changed []string }

// diffTrees compares the destination before a refresh with what the refresh
// writes. withRemovals is false for an incremental sink, which deletes nothing.
func diffTrees(before, after map[string][32]byte, withRemovals bool) treeDiff {
	var d treeDiff
	for name, sum := range after {
		old, ok := before[name]
		switch {
		case !ok:
			d.added = append(d.added, name)
		case old != sum:
			d.changed = append(d.changed, name)
		}
	}
	if withRemovals {
		for name := range before {
			if _, ok := after[name]; !ok {
				d.removed = append(d.removed, name)
			}
		}
	}
	sort.Strings(d.added)
	sort.Strings(d.removed)
	sort.Strings(d.changed)
	return d
}

func capNames(names []string) []string {
	if names == nil {
		return []string{}
	}
	if len(names) > maxDiffNames {
		return names[:maxDiffNames]
	}
	return names
}
