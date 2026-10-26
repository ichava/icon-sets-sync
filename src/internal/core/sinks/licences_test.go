package sinks

import (
	"bytes"
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
