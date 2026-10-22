package gitx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type call struct {
	dir  string
	env  []string
	name string
	args []string
}

func (c call) line() string { return c.name + " " + strings.Join(c.args, " ") }

// fakeRunner records every command and answers from a script keyed by the
// command line's prefix.
type fakeRunner struct {
	calls   []call
	answers map[string]string
	fail    map[string]error
}

func (f *fakeRunner) Run(_ context.Context, dir string, env []string, name string, args ...string) (string, error) {
	c := call{dir, env, name, args}
	f.calls = append(f.calls, c)
	for prefix, err := range f.fail {
		if strings.HasPrefix(c.line(), prefix) {
			return "", err
		}
	}
	for prefix, out := range f.answers {
		if strings.HasPrefix(c.line(), prefix) {
			return out, nil
		}
	}
	return "", nil
}

func TestSplitAuthor(t *testing.T) {
	tests := []struct{ in, name, email string }{
		{DefaultAuthor, "ichava-sync-bot", "19682005+imanimanyara@users.noreply.github.com"},
		{"A B <a@b.c>", "A B", "a@b.c"},
		{"no-email", "no-email", ""},
		{"broken > <", "broken > <", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			n, e := splitAuthor(tt.in)
			if n != tt.name || e != tt.email {
				t.Errorf("got (%q, %q), want (%q, %q)", n, e, tt.name, tt.email)
			}
		})
	}
}

// The committer must be set as well as the author, or GitHub's email-privacy
// guard refuses the push after the refresh has already been done.
func TestCommitAllSetsAuthorAndCommitter(t *testing.T) {
	f := &fakeRunner{answers: map[string]string{"git rev-parse HEAD": "abc123"}}
	g := &Git{Run: f, Author: "Robot <robot@example.com>"}

	sha, err := g.CommitAll(context.Background(), "/repo", "subject", "body")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "abc123" {
		t.Errorf("sha = %q", sha)
	}

	want := []string{
		"git add -A",
		"git commit --author=Robot <robot@example.com> -m subject\n\nbody",
		"git rev-parse HEAD",
	}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %v", f.calls)
	}
	for i, w := range want {
		if got := f.calls[i].line(); got != w {
			t.Errorf("call %d = %q, want %q", i, got, w)
		}
		if f.calls[i].dir != "/repo" {
			t.Errorf("call %d ran in %q", i, f.calls[i].dir)
		}
	}
	env := strings.Join(f.calls[1].env, ";")
	if !strings.Contains(env, "GIT_COMMITTER_NAME=Robot") || !strings.Contains(env, "GIT_COMMITTER_EMAIL=robot@example.com") {
		t.Errorf("committer env = %v", f.calls[1].env)
	}
}

func TestCommitWithoutABodyIsTheSubjectAlone(t *testing.T) {
	f := &fakeRunner{}
	if _, err := (&Git{Run: f}).CommitAll(context.Background(), "/r", "only", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[1].args[len(f.calls[1].args)-1]; got != "only" {
		t.Errorf("message = %q", got)
	}
	if !strings.Contains(f.calls[1].line(), "--author="+DefaultAuthor) {
		t.Errorf("an unset author must fall back to the default: %q", f.calls[1].line())
	}
}

func TestPushBranch(t *testing.T) {
	tests := []struct {
		lease bool
		want  string
	}{
		{true, "git push -u origin chore/x --force-with-lease"},
		{false, "git push -u origin chore/x"},
	}
	for _, tt := range tests {
		f := &fakeRunner{}
		if err := (&Git{Run: f}).PushBranch(context.Background(), "/r", "chore/x", tt.lease); err != nil {
			t.Fatal(err)
		}
		if got := f.calls[0].line(); got != tt.want {
			t.Errorf("lease=%v: %q, want %q", tt.lease, got, tt.want)
		}
	}
}

// Only an OPEN pull request counts. Matching a merged one is how the Python
// reported "already open" and exited 0 with nothing tracking the branch.
func TestOpenPR(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		base     string
		wantURL  string
		wantCall string
	}{
		{"reuses the open one", "https://github.com/o/r/pull/1", "", "https://github.com/o/r/pull/1", ""},
		{"creates against main by default", "", "", "https://github.com/o/r/pull/2",
			"gh pr create --title t --body b --base main --head chore/x"},
		{"creates against a configured base", "", "trunk", "https://github.com/o/r/pull/2",
			"gh pr create --title t --body b --base trunk --head chore/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRunner{answers: map[string]string{
				"gh pr list":   tt.existing,
				"gh pr create": "https://github.com/o/r/pull/2",
			}}
			url, err := (&Git{Run: f}).OpenPR(context.Background(), "/r", "chore/x", "t", "b", tt.base)
			if err != nil {
				t.Fatal(err)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if !strings.Contains(f.calls[0].line(), "--state open") {
				t.Errorf("the lookup must filter to open PRs: %q", f.calls[0].line())
			}
			if tt.wantCall != "" {
				if len(f.calls) < 2 || f.calls[1].line() != tt.wantCall {
					t.Errorf("create call = %v, want %q", f.calls, tt.wantCall)
				}
			} else if len(f.calls) != 1 {
				t.Errorf("an existing PR must not be re-created: %v", f.calls)
			}
		})
	}
}

func TestPRForBranchSwallowsALookupFailure(t *testing.T) {
	f := &fakeRunner{fail: map[string]error{"gh pr list": errors.New("no gh")}}
	if got := (&Git{Run: f}).PRForBranch(context.Background(), "/r", "b"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	tests := []struct {
		out  string
		want bool
	}{
		{"", false},
		{" M file.svg", true},
	}
	for _, tt := range tests {
		f := &fakeRunner{answers: map[string]string{"git status --porcelain": tt.out}}
		got, err := (&Git{Run: f}).HasUncommittedChanges(context.Background(), "/r")
		if err != nil || got != tt.want {
			t.Errorf("out %q: got %v, %v", tt.out, got, err)
		}
	}
}

func TestCreateAndFetchBranch(t *testing.T) {
	f := &fakeRunner{}
	g := &Git{Run: f}
	if err := g.CreateBranch(context.Background(), "/r", "chore/sync-upstream-1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := g.FetchBranch(context.Background(), "/r", "chore/sync-upstream-1.0.0"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"git checkout -B chore/sync-upstream-1.0.0",
		"git fetch origin +refs/heads/chore/sync-upstream-1.0.0:refs/remotes/origin/chore/sync-upstream-1.0.0",
	}
	for i, w := range want {
		if got := f.calls[i].line(); got != w {
			t.Errorf("call %d = %q, want %q", i, got, w)
		}
	}
}
