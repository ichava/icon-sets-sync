package sources

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// TestSafeJoinRefusesTraversal is the Zip Slip guard.
//
// Python got this free from tarfile's filter="data"; Go's archive packages do
// nothing about it, so an upstream tarball with an entry named
// ../../../etc/something would otherwise be written wherever it pointed. The
// toolkit runs against archives fetched from the internet, inside a container
// that has a mounted pack tree, so it matters.
func TestSafeJoinRefusesTraversal(t *testing.T) {
	dest := "/tmp/extract"

	for _, name := range []string{
		"../escape.svg",
		"../../etc/passwd",
		"a/../../escape.svg",
		"/absolute/escape.svg",
	} {
		got, err := safeJoin(dest, name)
		// An absolute or traversing name must either be refused, or be clamped
		// back inside the destination -- never resolve outside it.
		if err == nil && !strings.HasPrefix(got, dest) {
			t.Errorf("safeJoin(%q) = %q, which escapes %q", name, got, dest)
		}
	}

	ok, err := safeJoin(dest, "icons/home.svg")
	if err != nil {
		t.Fatalf("an ordinary entry was refused: %v", err)
	}
	if ok != filepath.Join(dest, "icons/home.svg") {
		t.Errorf("safeJoin = %q", ok)
	}
}

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractTarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tgz")
	writeTarGz(t, archive, map[string]string{
		"package/icons/home.svg": "<svg/>",
		"package/package.json":   "{}",
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractTarGz(archive, dest); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "package", "icons", "home.svg"))
	if err != nil || string(got) != "<svg/>" {
		t.Fatalf("extracted %q, err %v", got, err)
	}
}

// TestExtractTarGzRefusesATraversingEntry is the end-to-end of the guard.
func TestExtractTarGzRefusesATraversingEntry(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.tgz")
	writeTarGz(t, archive, map[string]string{"../escaped.svg": "<svg/>"})

	dest := filepath.Join(dir, "out")
	_ = ExtractTarGz(archive, dest)

	if _, err := os.Stat(filepath.Join(dir, "escaped.svg")); err == nil {
		t.Fatal("a traversing archive entry was written outside the destination")
	}
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")

	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("openmoji-15.1.0/color/svg/1F600.svg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("<svg/>")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dest := filepath.Join(dir, "out")
	if err := ExtractZip(archive, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "openmoji-15.1.0", "color", "svg", "1F600.svg")); err != nil {
		t.Fatal(err)
	}
}

// TestSingleExtractedRoot covers the wrapper directory a GitHub archive adds,
// whose name embeds the tag and so cannot be predicted from the config.
func TestSingleExtractedRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openmoji-15.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A zip made on macOS carries this; it must not count as a second root.
	if err := os.MkdirAll(filepath.Join(dir, "__MACOSX"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := singleExtractedRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "openmoji-15.1.0" {
		t.Errorf("root = %s", got)
	}

	if err := os.MkdirAll(filepath.Join(dir, "second"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := singleExtractedRoot(dir); err == nil {
		t.Error("two roots must be an error rather than a guess at which is wanted")
	}
}

func TestUnicodeCldrRequiresAVersion(t *testing.T) {
	// The default once ran ahead of what Unicode had published, every fetch
	// 404'd, and the pack shipped a tagged release with zero SVGs. There is
	// no default now.
	if err := (UnicodeCldr{}).Execute(nil); err == nil {
		t.Fatal("expected an error naming the missing unicode_version")
	}
}

// S6: extraction is bounded. Zip-slip is blocked by safeJoin; a decompression
// bomb is a different attack, and it is the size of the output that matters,
// not the size of the archive.

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func dirBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func TestExtractionLimits(t *testing.T) {
	bomb := strings.Repeat("\x00", 4<<20) // compresses to a few KiB
	many := map[string]string{}
	for i := 0; i < 20; i++ {
		many[filepath.Join("package", "f"+string(rune('a'+i))+".svg")] = "<svg/>"
	}

	cases := []struct {
		name   string
		files  map[string]string
		limits ExtractLimits
		want   string // error substring; "" means success
	}{
		{"member over its cap", map[string]string{"package/big.bin": bomb}, ExtractLimits{MaxMemberBytes: 1 << 20, MaxTotalBytes: 1 << 30, MaxEntries: 100}, "member"},
		{"total over its cap", map[string]string{"package/a.bin": bomb[:600<<10], "package/b.bin": bomb[:600<<10]}, ExtractLimits{MaxMemberBytes: 1 << 20, MaxTotalBytes: 1 << 20, MaxEntries: 100}, "total"},
		{"too many entries", many, ExtractLimits{MaxMemberBytes: 1 << 20, MaxTotalBytes: 1 << 30, MaxEntries: 10}, "entries"},
		{"within every cap", map[string]string{"package/ok.svg": "<svg/>"}, ExtractLimits{MaxMemberBytes: 1 << 20, MaxTotalBytes: 1 << 20, MaxEntries: 10}, ""},
		{"member exactly at its cap", map[string]string{"package/ok.bin": bomb[:1024]}, ExtractLimits{MaxMemberBytes: 1024, MaxTotalBytes: 1 << 20, MaxEntries: 10}, ""},
	}

	for _, format := range []string{"tar.gz", "zip"} {
		for _, tc := range cases {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				archive := filepath.Join(dir, "a."+format)
				dest := filepath.Join(dir, "out")

				var err error
				if format == "zip" {
					writeZip(t, archive, tc.files)
					err = extractZip(archive, dest, tc.limits)
				} else {
					writeTarGz(t, archive, tc.files)
					err = extractTarGz(archive, dest, tc.limits)
				}

				if tc.want == "" {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err = %v, want one naming %q", err, tc.want)
				}
				// The write stops at the cap; it does not write the bomb out first.
				if got := dirBytes(t, dest); got > tc.limits.MaxTotalBytes {
					t.Errorf("wrote %d bytes, past the %d byte total cap", got, tc.limits.MaxTotalBytes)
				}
			})
		}
	}
}

func TestDefaultExtractLimitsAreSetAndGenerous(t *testing.T) {
	l := DefaultExtractLimits
	if l.MaxMemberBytes < 64<<20 || l.MaxTotalBytes < 1<<30 || l.MaxEntries < 50_000 {
		t.Fatalf("defaults %+v would refuse a real upstream archive (the largest holds tens of thousands of files)", l)
	}
	if l.MaxMemberBytes > l.MaxTotalBytes {
		t.Fatalf("member cap %d above total cap %d", l.MaxMemberBytes, l.MaxTotalBytes)
	}
}

// S7: npm pack must never run a package's lifecycle scripts. A registry spec
// does not run them, but a git or local spec runs `prepare`, and the spec comes
// from config or the local web UI.

func TestNpmPackArgsIgnoreScripts(t *testing.T) {
	args := NpmPackArgs("@tabler/icons@3.46.0")
	want := map[string]bool{"pack": false, "@tabler/icons@3.46.0": false, "--silent": false, "--ignore-scripts": false}
	for _, a := range args {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for a, seen := range want {
		if !seen {
			t.Errorf("npm args %v lack %q", args, a)
		}
	}
}

// fakeNpm puts an `npm` on PATH that records its arguments and fails, so the
// test sees exactly what the real call site passed.
func fakeNpm(t *testing.T) (argsFile string) {
	t.Helper()
	bin := t.TempDir()
	argsFile = filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$NPM_ARGS_FILE\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NPM_ARGS_FILE", argsFile)
	return argsFile
}

func TestNpmTarballPassesIgnoreScripts(t *testing.T) {
	argsFile := fakeNpm(t)

	_, _ = pipeline.Named("demo").
		Source(NpmTarball{Package: "@tabler/icons", Version: "3.46.0"}).
		Sink(nopSink{}).
		Run()

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the fake npm was never called: %v", err)
	}
	if !strings.Contains(string(got), "--ignore-scripts") {
		t.Fatalf("npm pack ran without --ignore-scripts; args:\n%s", got)
	}
}
