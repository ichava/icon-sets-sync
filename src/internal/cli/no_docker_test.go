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

// release.yml publishes binaries only when a person dispatches it against a
// tag with confirm=release-go. A tag, a schedule, a pull request or a release
// event must never start it.
func TestReleaseRunsOnlyOnAConfirmedDispatch(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		On map[string]struct {
			Inputs map[string]struct {
				Required bool `yaml:"required"`
			} `yaml:"inputs"`
		} `yaml:"on"`
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for k := range wf.On {
		triggers = append(triggers, k)
	}
	sort.Strings(triggers)
	if strings.Join(triggers, ",") != "workflow_dispatch" {
		t.Fatalf("release.yml triggers %v, want only workflow_dispatch", triggers)
	}
	if in, ok := wf.On["workflow_dispatch"].Inputs["confirm"]; !ok || !in.Required {
		t.Fatal("release.yml has no required confirm input")
	}
	job, ok := wf.Jobs["release"]
	if !ok || len(job.Steps) == 0 {
		t.Fatal("release.yml has no release job")
	}
	first := job.Steps[0].Run
	if !strings.Contains(first, `"release-go"`) || !strings.Contains(first, `"tag"`) {
		t.Errorf("the first release step must refuse without confirm=release-go on a tag; it runs:\n%s", first)
	}
}
