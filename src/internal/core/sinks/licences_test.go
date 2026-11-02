package sinks

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

func TestLicenceTexts(t *testing.T) {
	// Bytes chosen to catch any normalisation: CRLF, a non-ASCII copyright
	// holder, no trailing newline.
	mit := []byte("MIT License\r\n\r\nCopyright (c) 2020-2026 Paweł Kuna")
	notice := []byte("Apache NOTICE\nThis product includes software developed at Example.\n")

	cases := []struct {
		name    string
		files   map[string][]byte // upstream root files; nil map = FindLicences found none
		stale   []string          // already in the destination before the run
		missing string            // MissingReason
		want    []string          // destination afterwards; nil with no error = directory removed
		wantErr string
	}{
		{name: "copies the licence verbatim", files: map[string][]byte{"LICENSE": mit}, want: []string{"LICENSE"}},
		{name: "copies the NOTICE when upstream has one", files: map[string][]byte{"LICENSE": mit, "NOTICE": notice}, want: []string{"LICENSE", "NOTICE"}},
		{name: "drops a file upstream no longer ships", files: map[string][]byte{"LICENSE": mit}, stale: []string{"NOTICE"}, want: []string{"LICENSE"}},
		{name: "a missing licence fails, naming the set", files: map[string][]byte{}, wantErr: "demo-set: upstream npm:demo@1.0.0 ships no LICENSE"},
		{name: "a recorded missing licence passes and leaves no stale text", files: map[string][]byte{}, stale: []string{"LICENSE"}, missing: "upstream ships none; added by hand"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := t.TempDir()
			var paths []string
			for name, body := range c.files {
				p := filepath.Join(src, name)
				if err := os.WriteFile(p, body, 0o644); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, p)
			}
			sort.Strings(paths)

			dest := filepath.Join(t.TempDir(), "licenses", "demo-set")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, s := range c.stale {
				if err := os.WriteFile(filepath.Join(dest, s), []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			ctx := ctxFor(t, src)
			ctx.SetString(pipeline.KeyLicenceFiles, strings.Join(paths, "\n"))
			ctx.SetString(pipeline.KeyFetchedSource, "npm:demo@1.0.0")

			err := LicenceTexts{Root: dest, Set: "demo-set", MissingReason: c.missing}.Execute(ctx)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			if c.want == nil {
				if _, err := os.Stat(dest); !os.IsNotExist(err) {
					t.Fatalf("destination still exists (%v); a stale licence would describe a release it no longer matches", err)
				}
				return
			}
			entries, _ := os.ReadDir(dest)
			var got []string
			for _, e := range entries {
				got = append(got, e.Name())
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("destination holds %v, want %v", got, c.want)
			}
			for _, name := range c.want {
				b, _ := os.ReadFile(filepath.Join(dest, name))
				if !bytes.Equal(b, c.files[name]) {
					t.Errorf("%s is not byte-identical to upstream:\n got %q\nwant %q", name, b, c.files[name])
				}
			}
		})
	}
}

// Without FindLicences upstream of it the sink cannot know what to copy; that
// is a recipe wiring mistake and must not read as "no licence".
func TestLicenceTextsNeedsFindLicences(t *testing.T) {
	err := LicenceTexts{Root: t.TempDir(), Set: "x"}.Execute(ctxFor(t, t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "FindLicences must run") {
		t.Fatalf("err = %v", err)
	}
}

// A licence directory holding a SOURCE.md is hand-maintained: the sink must
// never delete or overwrite it, must report it as hand-supplied rather than
// missing, and must stop warning about a licence_missing it satisfies. An
// un-annotated directory keeps the old behaviour: replaced when upstream ships
// texts, removed when it ships none.
func TestLicenceTextsHandSupplied(t *testing.T) {
	ofl := []byte("Copyright (c) James Walsh\r\nSIL OPEN FONT LICENSE Version 1.1\n")
	source := []byte("# Source\n\nHand-supplied: upstream ships no licence file.\n")
	upstreamMIT := []byte("MIT License\n\nCopyright (c) upstream\n")

	cases := []struct {
		name      string
		upstream  map[string][]byte // files FindLicences found at the upstream root
		existing  map[string][]byte // destination contents before the run
		missing   string            // MissingReason
		want      map[string][]byte // destination afterwards; nil = removed
		status    string
		reconcile bool
		warn      string // substring of the one WARN line expected; "" = no WARN at all
		wantErr   string
	}{
		{
			name:     "hand-supplied folder survives a sync and stops the licence_missing warning",
			existing: map[string][]byte{"OFL.txt": ofl, "SOURCE.md": source},
			missing:  "npm academicons ships none",
			want:     map[string][]byte{"OFL.txt": ofl, "SOURCE.md": source},
			status:   LicenceStatusHandSupplied,
		},
		{
			name:      "hand-supplied folder is kept when upstream starts shipping a licence, with a warning to reconcile",
			upstream:  map[string][]byte{"LICENSE": upstreamMIT},
			existing:  map[string][]byte{"LICENSE": []byte("hand MIT text"), "SOURCE.md": source},
			missing:   "npm zondicons ships none",
			want:      map[string][]byte{"LICENSE": []byte("hand MIT text"), "SOURCE.md": source},
			status:    LicenceStatusHandSupplied,
			reconcile: true,
			warn:      "upstream now ships licence files; the hand-supplied folder is kept",
		},
		{
			name:     "hand-supplied folder without licence_missing still fails, and is kept",
			existing: map[string][]byte{"OFL.txt": ofl, "SOURCE.md": source},
			want:     map[string][]byte{"OFL.txt": ofl, "SOURCE.md": source},
			wantErr:  "holds a hand-supplied text (it is kept), but set licence_missing",
		},
		{
			name:     "an un-annotated stale folder is still removed when upstream ships none",
			existing: map[string][]byte{"OFL.txt": ofl},
			missing:  "npm pepicons ships none",
			status:   LicenceStatusMissing,
			warn:     "upstream ships none; recorded as missing",
		},
		{
			name:     "an un-annotated stale folder is still replaced by upstream's texts",
			upstream: map[string][]byte{"LICENSE": upstreamMIT},
			existing: map[string][]byte{"OFL.txt": ofl, "LICENSE": []byte("old")},
			want:     map[string][]byte{"LICENSE": upstreamMIT},
			status:   LicenceStatusCopied,
		},
		{
			name:     "a SOURCE.md directory is not the marker",
			upstream: map[string][]byte{"LICENSE": upstreamMIT},
			existing: map[string][]byte{"SOURCE.md/x": []byte("x")},
			want:     map[string][]byte{"LICENSE": upstreamMIT},
			status:   LicenceStatusCopied,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := t.TempDir()
			var paths []string
			for name, body := range c.upstream {
				p := filepath.Join(src, name)
				if err := os.WriteFile(p, body, 0o644); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, p)
			}
			sort.Strings(paths)

			dest := filepath.Join(t.TempDir(), "licenses", "demo-set")
			for name, body := range c.existing {
				p := filepath.Join(dest, name)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, body, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			ctx := ctxFor(t, src)
			ctx.SetString(pipeline.KeyLicenceFiles, strings.Join(paths, "\n"))
			ctx.SetString(pipeline.KeyFetchedSource, "npm:demo@1.0.0")

			err := LicenceTexts{Root: dest, Set: "demo-set", MissingReason: c.missing}.Execute(ctx)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			assertTree(t, dest, c.want)

			warns := 0
			for _, line := range strings.Split(logs.String(), "\n") {
				if strings.Contains(line, "level=WARN") {
					warns++
					if c.warn == "" || !strings.Contains(line, c.warn) {
						t.Errorf("unexpected warning: %s", line)
					}
				}
			}
			if c.warn != "" && warns != 1 {
				t.Errorf("got %d warnings, want 1 containing %q; log:\n%s", warns, c.warn, logs.String())
			}

			if c.wantErr != "" {
				return
			}
			m, _ := ctx.Metrics["licence_texts"].(map[string]any)
			if m["status"] != c.status {
				t.Errorf("metric status = %v, want %q (metric %v)", m["status"], c.status, m)
			}
			if c.status == LicenceStatusHandSupplied {
				if _, ok := m["missing"]; ok {
					t.Errorf("a hand-supplied set must not be reported as missing: %v", m)
				}
			}
			if got, _ := m["reconcile"].(bool); got != c.reconcile {
				t.Errorf("metric reconcile = %v, want %v (metric %v)", got, c.reconcile, m)
			}
		})
	}
}

// assertTree checks dir holds exactly want, byte for byte; nil want means dir
// must not exist.
func assertTree(t *testing.T, dir string, want map[string][]byte) {
	t.Helper()
	if want == nil {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s still exists (%v)", dir, err)
		}
		return
	}
	got := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		got[filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s holds %d files, want %d: %v", dir, len(got), len(want), keys(got))
	}
	for name, body := range want {
		if !bytes.Equal(got[name], body) {
			t.Errorf("%s = %q, want %q", name, got[name], body)
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
