package sinks

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// gitIn runs git in dir as a person (not the sync's author).
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Person", "GIT_AUTHOR_EMAIL=person@example.invalid",
		"GIT_COMMITTER_NAME=Person", "GIT_COMMITTER_EMAIL=person@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// packWithRemote is a pack checkout on main with a bare origin, and a second
// clone a person works in.
func packWithRemote(t *testing.T) (pack, person string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	base := t.TempDir()
	remote := filepath.Join(base, "origin.git")
	gitIn(t, base, "init", "-q", "--bare", "-b", "main", remote)
	pack = filepath.Join(base, "pack")
	gitIn(t, base, "clone", "-q", remote, pack)
	write(t, filepath.Join(pack, "icons/a.svg"), "<svg>a1</svg>")
	write(t, filepath.Join(pack, "tests/guard.txt"), "old guard\n")
	gitIn(t, pack, "checkout", "-q", "-b", "main")
	gitIn(t, pack, "add", "-A")
	gitIn(t, pack, "commit", "-q", "-m", "Initial release")
	gitIn(t, pack, "push", "-q", "-u", "origin", "main")
	person = filepath.Join(base, "person")
	gitIn(t, base, "clone", "-q", remote, person)
	return pack, person
}

func runGitBranch(t *testing.T, pack, version string) (*pipeline.Context, error) {
	t.Helper()
	ctx := &pipeline.Context{PipelineName: "demo", WorkingDir: t.TempDir(),
		Config: map[string]any{}, Extras: map[string]any{}, Metrics: map[string]any{}}
	ctx.SetFetchedVersion(version)
	g := gitx.New()
	g.Env = []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	err := GitBranch{RepoRoot: pack, Branch: "chore/sync-upstream", Git: g}.Execute(ctx)
	return ctx, err
}

// The openmoji case (2026-11-09): a person added a commit to the sync's pull
// request branch, and the next run reset the branch to main and force-pushed
// over it, so the commit vanished from the pull request. A re-run now keeps
// every commit the sync did not make, replayed on top of its new one.
func TestARerunKeepsCommitsAPersonAddedToTheBranch(t *testing.T) {
	pack, person := packWithRemote(t)

	write(t, filepath.Join(pack, "icons/a.svg"), "<svg>a2</svg>")
	if _, err := runGitBranch(t, pack, "2.0.0"); err != nil {
		t.Fatal(err)
	}

	// A person fixes a test on the pull request branch.
	gitIn(t, person, "fetch", "-q", "origin")
	gitIn(t, person, "checkout", "-q", "-b", "chore/sync-upstream", "origin/chore/sync-upstream")
	write(t, filepath.Join(person, "tests/guard.txt"), "new guard\n")
	gitIn(t, person, "commit", "-q", "-am", "test: fix the guard")
	gitIn(t, person, "push", "-q", "origin", "chore/sync-upstream")

	// The sync runs again from main, with a newer upstream.
	gitIn(t, pack, "checkout", "-q", "main")
	write(t, filepath.Join(pack, "icons/a.svg"), "<svg>a3</svg>")
	ctx, err := runGitBranch(t, pack, "3.0.0")
	if err != nil {
		t.Fatal(err)
	}

	gitIn(t, person, "fetch", "-q", "origin")
	log := gitIn(t, person, "log", "--format=%an|%s", "origin/main..origin/chore/sync-upstream")
	lines := strings.Split(log, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "Person|test: fix the guard") ||
		!strings.Contains(lines[1], "ichava-sync-bot|") {
		t.Fatalf("the branch is now:\n%s", log)
	}
	if got := gitIn(t, person, "show", "origin/chore/sync-upstream:tests/guard.txt"); got != "new guard" {
		t.Errorf("the person's fix is gone: %q", got)
	}
	if got := gitIn(t, person, "show", "origin/chore/sync-upstream:icons/a.svg"); got != "<svg>a3</svg>" {
		t.Errorf("the new sync is missing: %q", got)
	}
	m, _ := ctx.Metrics["git_branch"].(map[string]any)
	if kept, _ := m["kept_commits"].([]string); len(kept) != 1 {
		t.Errorf("metric kept_commits = %v", m["kept_commits"])
	}
}

// A commit that no longer applies on the new sync is not dropped either:
// the run stops for a person, and the branch on the remote is untouched.
func TestARerunStopsWhenAPersonsCommitNoLongerApplies(t *testing.T) {
	pack, person := packWithRemote(t)
	write(t, filepath.Join(pack, "icons/a.svg"), "<svg>a2</svg>")
	if _, err := runGitBranch(t, pack, "2.0.0"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, person, "fetch", "-q", "origin")
	gitIn(t, person, "checkout", "-q", "-b", "chore/sync-upstream", "origin/chore/sync-upstream")
	write(t, filepath.Join(person, "icons/a.svg"), "<svg>hand-tuned</svg>")
	gitIn(t, person, "commit", "-q", "-am", "fix: hand-tune a")
	gitIn(t, person, "push", "-q", "origin", "chore/sync-upstream")
	before := gitIn(t, person, "rev-parse", "origin/chore/sync-upstream")

	gitIn(t, pack, "checkout", "-q", "main")
	write(t, filepath.Join(pack, "icons/a.svg"), "<svg>a3</svg>")
	_, err := runGitBranch(t, pack, "3.0.0")
	if err == nil || !errors.Is(err, ErrNeedsReview) || !strings.Contains(err.Error(), "fix: hand-tune a") {
		t.Fatalf("err = %v, want a review naming the commit", err)
	}
	gitIn(t, person, "fetch", "-q", "origin")
	if after := gitIn(t, person, "rev-parse", "origin/chore/sync-upstream"); after != before {
		t.Errorf("the remote branch moved from %s to %s", before, after)
	}
	// The checkout is left clean, on a branch, with nothing half-applied.
	if st := gitIn(t, pack, "status", "--porcelain"); st != "" {
		t.Errorf("checkout left dirty:\n%s", st)
	}
}

// A branch holding only the sync's own commits is replaced, as before.
func TestARerunReplacesTheSyncsOwnCommit(t *testing.T) {
	pack, person := packWithRemote(t)
	write(t, filepath.Join(pack, "icons/a.svg"), "<svg>a2</svg>")
	if _, err := runGitBranch(t, pack, "2.0.0"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, pack, "checkout", "-q", "main")
	write(t, filepath.Join(pack, "icons/a.svg"), "<svg>a3</svg>")
	if _, err := runGitBranch(t, pack, "3.0.0"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, person, "fetch", "-q", "origin")
	if n := gitIn(t, person, "rev-list", "--count", "origin/main..origin/chore/sync-upstream"); n != "1" {
		t.Errorf("%s commits on the branch, want the one new sync commit", n)
	}
}
