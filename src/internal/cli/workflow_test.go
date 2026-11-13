package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The reusable CI-mode workflow, read as data: its triggers, permissions,
// limits and pinning are the spec, so a change to any of them fails here
// rather than in a pack repository on a Monday morning. No `act`, no network.

const syncPackWorkflow = "../../../.github/workflows/sync-pack.yml"

type workflowStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	If   string            `yaml:"if"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type workflowJob struct {
	RunsOn         string            `yaml:"runs-on"`
	TimeoutMinutes int               `yaml:"timeout-minutes"`
	Permissions    map[string]string `yaml:"permissions"`
	Concurrency    struct {
		Group            string `yaml:"group"`
		CancelInProgress bool   `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Env   map[string]string `yaml:"env"`
	Steps []workflowStep    `yaml:"steps"`
}

type workflow struct {
	On          map[string]yaml.Node   `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

func loadSyncPack(t *testing.T) (workflow, workflowJob, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(syncPackWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	var wf workflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	job, ok := wf.Jobs["sync"]
	if !ok || len(job.Steps) < 5 {
		t.Fatalf("no sync job with its steps: %d jobs, %d steps", len(wf.Jobs), len(job.Steps))
	}
	return wf, job, string(raw)
}

func TestSyncPackIsOnlyCallable(t *testing.T) {
	wf, _, _ := loadSyncPack(t)
	var keys []string
	for k := range wf.On {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// The caller owns schedule and dispatch; push never runs a sync.
	if strings.Join(keys, ",") != "workflow_call" {
		t.Fatalf("triggers %v, want only workflow_call", keys)
	}
}

func TestSyncPackPermissionsAreLeast(t *testing.T) {
	wf, job, raw := loadSyncPack(t)
	if len(wf.Permissions) != 1 || wf.Permissions["contents"] != "read" {
		t.Errorf("workflow permissions %v, want contents: read only", wf.Permissions)
	}
	want := map[string]string{"contents": "read", "issues": "write"}
	if len(job.Permissions) != len(want) {
		t.Errorf("job permissions %v, want %v", job.Permissions, want)
	}
	for k, v := range want {
		if job.Permissions[k] != v {
			t.Errorf("job permission %s = %q, want %q", k, job.Permissions[k], v)
		}
	}

	var minted int
	for _, s := range job.Steps {
		if !strings.HasPrefix(s.Uses, "actions/create-github-app-token@") {
			continue
		}
		minted++
		var perms []string
		for k, v := range s.With {
			if strings.HasPrefix(k, "permission-") {
				perms = append(perms, k+"="+v)
			}
		}
		sort.Strings(perms)
		if strings.Join(perms, ",") != "permission-contents=write,permission-pull-requests=write" {
			t.Errorf("Refresh Bot token asks for %v, want contents and pull-requests write only", perms)
		}
	}
	if minted != 1 {
		t.Errorf("%d Refresh Bot tokens minted, want 1", minted)
	}
	if strings.Contains(raw, "secrets.GITHUB_TOKEN") {
		t.Error("use github.token for the issue; the Refresh Bot does every repository write")
	}
}

func TestSyncPackLimits(t *testing.T) {
	_, job, _ := loadSyncPack(t)
	if !strings.HasPrefix(job.RunsOn, "ubuntu-") {
		t.Errorf("runs-on %q, want Linux", job.RunsOn)
	}
	if job.TimeoutMinutes <= 0 || job.TimeoutMinutes > 30 {
		t.Errorf("timeout-minutes %d, want 1-30", job.TimeoutMinutes)
	}
	if !job.Concurrency.CancelInProgress || job.Concurrency.Group == "" {
		t.Errorf("concurrency %+v, want a group with cancel-in-progress", job.Concurrency)
	}
	if job.Env["GOPRIVATE"] != "github.com/ichava/*" || job.Env["GONOSUMDB"] != "github.com/ichava/*" {
		t.Errorf("env %v, want GOPRIVATE and GONOSUMDB for github.com/ichava/*", job.Env)
	}
}

// The toolkit is built from source at the caller's pinned ref, and the first
// step refuses anything that is not a tag or a full SHA.
func TestSyncPackBuildsFromAPinnedRef(t *testing.T) {
	_, job, raw := loadSyncPack(t)
	if strings.Contains(raw, "docker build") || strings.Contains(raw, "releases/download") {
		t.Error("the binary is built from source, not pulled as an image or a release asset")
	}
	first := job.Steps[0]
	if first.Env["REF"] != "${{ inputs.toolkit-ref }}" || !strings.Contains(first.Run, "exit 1") {
		t.Fatalf("first step %q does not guard toolkit-ref", first.Name)
	}
	guard := regexp.MustCompile(`'(\^[^']+\$)'`).FindStringSubmatch(first.Run)
	if guard == nil {
		t.Fatal("no ref pattern in the guard step")
	}
	re := regexp.MustCompile(guard[1])
	for ref, ok := range map[string]bool{
		"v0.2.0": true, "v1.10.3-rc.2": true, strings.Repeat("a1", 20): true,
		"main": false, "v1": false, "feat/x": false, "abc123": false, "": false,
	} {
		if re.MatchString(ref) != ok {
			t.Errorf("ref %q accepted=%v, want %v", ref, !ok, ok)
		}
	}
	var checkedOut, built bool
	for _, s := range job.Steps {
		if s.With["repository"] == "ichava/icon-sets-sync" {
			checkedOut = s.With["ref"] == "${{ inputs.toolkit-ref }}"
		}
		if strings.Contains(s.Run, "go -C .icon-sets-sync/src build") {
			built = true
		}
	}
	if !checkedOut || !built {
		t.Errorf("toolkit checked out at the pinned ref %v, built from source %v", checkedOut, built)
	}
}

// A licence change opens an issue and fails; it never reaches a pull request.
func TestSyncPackTurnsALicenceChangeIntoAnIssue(t *testing.T) {
	_, job, _ := loadSyncPack(t)
	var issue *workflowStep
	for i, s := range job.Steps {
		if strings.Contains(s.Run, "gh issue create") {
			issue = &job.Steps[i]
		}
	}
	if issue == nil {
		t.Fatal("no step opens an issue")
	}
	if issue.If != "steps.sync.outputs.licence-changed == 'true'" {
		t.Errorf("issue step runs if %q", issue.If)
	}
	if !strings.Contains(issue.Run, "exit 1") || strings.Contains(issue.Run, "gh pr") {
		t.Error("the licence step must fail the job and open no pull request")
	}
	if issue.Env["GH_TOKEN"] != "${{ github.token }}" {
		t.Errorf("issue opened with %q, want github.token", issue.Env["GH_TOKEN"])
	}
}

// Every flag the workflow passes must exist in the CLI it builds, or the job
// fails at its first real run.
func TestSyncPackCallsFlagsTheCLIHas(t *testing.T) {
	_, job, _ := loadSyncPack(t)
	var run string
	for _, s := range job.Steps {
		if s.Name == "Run the sync" {
			run = s.Run
		}
	}
	if run == "" {
		t.Fatal("no Run the sync step")
	}
	flags := regexp.MustCompile(`--[a-z][a-z-]+`).FindAllString(run, -1)
	if len(flags) < 5 {
		t.Fatalf("found only %v in the run step", flags)
	}
	root := NewRootCommand()
	cmd, _, err := root.Find([]string{"run"})
	if err != nil || cmd.Name() != "sync" {
		t.Fatalf("run does not resolve to sync: %v", err)
	}
	for _, f := range flags {
		name := strings.TrimPrefix(f, "--")
		if cmd.Flags().Lookup(name) == nil && root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("the workflow passes %s, which the CLI does not have", f)
		}
	}
}
