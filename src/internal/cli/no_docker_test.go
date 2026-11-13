package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// icon-sets-sync is a Go binary built from source. The Docker image and the
// Python implementation before it are gone: no pack has run the image since
// sync-pack.yml began building from source, and nothing in this repository
// may bring either back without this test being changed on purpose.

const repoRoot = "../../.."

// minWorkflowFiles is the number of workflow files on 2026-11-04 (release,
// release-please, sync-pack, tests). A glob that stops matching fails here
// instead of passing over nothing.
const minWorkflowFiles = 4

// minRepoFiles is a floor on the files the walk visits; the tree held well
// over 300 on 2026-11-04. A walk rooted in the wrong place visits a handful.
const minRepoFiles = 200

// skipDirs are never ours to police: git's own store, Composer's vendor
// directory, generated output, local tool state and stale bytecode caches.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "build": true, "node_modules": true,
	".claude": true, "__pycache__": true, ".pytest_cache": true, ".ruff_cache": true,
	".mypy_cache": true, ".venv": true,
}

// dockerFile matches a file that only exists to build or run a container.
var dockerFile = regexp.MustCompile(`(?i)^(dockerfile(\..*)?|.*\.dockerfile|\.dockerignore|(docker-)?compose(\.[a-z0-9_-]+)?\.ya?ml)$`)

// dockerInWorkflow matches a workflow that builds, runs or publishes an image.
var dockerInWorkflow = regexp.MustCompile(`(?m)uses:\s*['"]?docker/|\bdocker\s+(build|buildx|run|push|compose|login)\b|\bghcr\.io\b`)

func TestNoDockerOrPythonSurface(t *testing.T) {
	visited := 0
	err := filepath.WalkDir(filepath.FromSlash(repoRoot), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != filepath.FromSlash(repoRoot) && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		visited++
		rel, _ := filepath.Rel(filepath.FromSlash(repoRoot), path)
		switch {
		case dockerFile.MatchString(d.Name()):
			t.Errorf("%s is a container build or compose file; icon-sets-sync builds from source", rel)
		case strings.HasSuffix(d.Name(), ".py"):
			t.Errorf("%s is Python; the implementation is Go", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	if visited < minRepoFiles {
		t.Fatalf("visited %d files, want at least %d: the walk is not looking where it should", visited, minRepoFiles)
	}
}

func TestNoWorkflowBuildsOrPublishesAnImage(t *testing.T) {
	dir := filepath.Join(filepath.FromSlash(repoRoot), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		read++
		if strings.Contains(strings.ToLower(name), "docker") {
			t.Errorf("%s: a Docker workflow; the image is gone", name)
		}
		if m := dockerInWorkflow.Find(raw); m != nil {
			t.Errorf("%s: %q builds, runs or publishes an image; build the binary from source", name, m)
		}
	}
	if read < minWorkflowFiles {
		t.Fatalf("read %d workflow files, want at least %d", read, minWorkflowFiles)
	}

	dependabot, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), ".github", "dependabot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`package-ecosystem:\s*['"]?docker`).Match(dependabot) {
		t.Error("dependabot.yml still watches a docker ecosystem")
	}

	makefile, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?i)\bdocker\b`).Match(makefile) {
		t.Error("the Makefile runs or mentions Docker; every target runs on the host")
	}
}

// release.yml publishes a release on a pushed version tag, or on request for a tag already
// pushed, and does nothing else: its triggers are exactly those two, and its one job calls the
// shared simtabi/.github go-release.yml (owner, 2026-11-13). A branch push, a schedule, a pull
// request or a release event must never start it.
func TestReleaseRunsOnAVersionTagOrOnRequest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		On struct {
			Push *struct {
				Tags     []string `yaml:"tags"`
				Branches []string `yaml:"branches"`
			} `yaml:"push"`
			Dispatch *struct {
				Inputs map[string]struct {
					Required bool `yaml:"required"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
		Jobs map[string]struct {
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	var all struct {
		On map[string]any `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for k := range all.On {
		triggers = append(triggers, k)
	}
	sort.Strings(triggers)
	if strings.Join(triggers, ",") != "push,workflow_dispatch" {
		t.Fatalf("release.yml triggers %v, want exactly push (version tags) and workflow_dispatch", triggers)
	}
	if wf.On.Push == nil || len(wf.On.Push.Branches) > 0 || len(wf.On.Push.Tags) == 0 {
		t.Fatal("release.yml must push-trigger on version tags only, never on a branch")
	}
	for _, tag := range wf.On.Push.Tags {
		if !strings.HasPrefix(tag, "v[0-9]+.") {
			t.Errorf("tag filter %q is not a version tag", tag)
		}
	}
	if wf.On.Dispatch == nil {
		t.Fatal("release.yml has no workflow_dispatch")
	}
	if in, ok := wf.On.Dispatch.Inputs["tag"]; !ok || !in.Required {
		t.Fatal("release.yml has no required tag input on workflow_dispatch")
	}
	job, ok := wf.Jobs["release"]
	if !ok || !strings.HasPrefix(job.Uses, "simtabi/.github/.github/workflows/go-release.yml@") {
		t.Fatalf("the release job must call the shared go-release.yml; it uses %q", job.Uses)
	}
	if job.With["tag"] != "${{ inputs.tag || github.ref_name }}" {
		t.Errorf("the release job passes tag %q", job.With["tag"])
	}
}

// minGitHubFiles is the number of files under .github/ on 2026-11-04: four
// workflows, dependabot.yml and scripts/ui-a11y.sh.
const minGitHubFiles = 6

// pythonCall matches a python or python3 invocation, not the word in prose.
var pythonCall = regexp.MustCompile(`(?m)(^|[\s;&|(` + "`" + `"'])python[0-9.]*(\s|$)`)

// No workflow or script runs Python: the changelog check and close are
// src/internal/tools/changelog, and the accessibility job's fixture upstream
// is src/internal/tools/staticserve.
func TestNoWorkflowOrScriptInvokesPython(t *testing.T) {
	read := 0
	err := filepath.WalkDir(filepath.Join(filepath.FromSlash(repoRoot), ".github"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		read++
		if loc := pythonCall.FindIndex(raw); loc != nil {
			line := 1 + strings.Count(string(raw[:loc[0]]), "\n")
			t.Errorf("%s:%d invokes Python; use a Go tool under src/internal/tools", path, line)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking .github: %v", err)
	}
	if read < minGitHubFiles {
		t.Fatalf("read %d files under .github, want at least %d", read, minGitHubFiles)
	}
}

// otherChannels are the GoReleaser blocks that publish somewhere other than the
// GitHub release: package managers, images and arbitrary publishers. The GitHub
// release is the only channel (owner, 2026-11-13).
var otherChannels = []string{"brews", "homebrew_casks", "scoops", "dockers", "docker_manifests", "kos", "snapcrafts", "winget", "aurs", "nix", "publishers", "blobs", "uploads", "artifactories", "chocolateys", "npms"}

func TestGoReleaserPublishesNowhereElse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), "src", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["release"]; !ok {
		t.Fatal("src/.goreleaser.yaml has no release block; the GitHub release is the channel")
	}
	for _, k := range otherChannels {
		if _, ok := cfg[k]; ok {
			t.Errorf("src/.goreleaser.yaml publishes through %q; binaries go to the GitHub release only", k)
		}
	}
}
