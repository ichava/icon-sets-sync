package sources

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/httpx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
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
	return buf.Bytes()
}

func sri(b []byte) string {
	sum := sha512.Sum512(b)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// fakeRegistry serves one version document and its tarball, and records the
// URLs asked for.
func fakeRegistry(t *testing.T, tarball []byte, integrity string) (*httpx.Client, *[]string) {
	t.Helper()
	var asked []string
	tarURL := NpmRegistry + "/@tabler/icons/-/icons-3.51.0.tgz"
	doc := fmt.Sprintf(`{"name":"@tabler/icons","version":"3.51.0","dist":{"tarball":%q,"integrity":%q}}`, tarURL, integrity)
	c := httpx.New()
	c.Sleep = func(time.Duration) {}
	c.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		asked = append(asked, r.URL.String())
		body := []byte(doc)
		if r.URL.String() == tarURL {
			body = tarball
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})
	return c, &asked
}

// The tabler sync failed "npm not on PATH" on a server started outside a
// shell. The tarball is now fetched from the registry, with no npm at all, and
// checked against the integrity the registry publishes.
func TestNpmTarballFetchesFromTheRegistryWithoutNpm(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no npm anywhere on PATH
	ball := tgz(t, map[string]string{"package/icons/outline/a.svg": "<svg/>"})
	client, asked := fakeRegistry(t, ball, sri(ball))

	res, err := pipeline.Named("demo").
		Source(NpmTarball{Package: "@tabler/icons", Version: "latest", HTTP: client}).
		Sink(nopSink{}).
		RunContext(context.Background())
	if err != nil || !res.Success {
		t.Fatalf("run: %v %v", err, res.Err)
	}
	if (*asked)[0] != NpmRegistry+"/@tabler/icons/latest" {
		t.Errorf("first request %s", (*asked)[0])
	}
}

func TestRegistryFetchRefusesATamperedTarball(t *testing.T) {
	ball := tgz(t, map[string]string{"package/a.svg": "<svg/>"})
	other := tgz(t, map[string]string{"package/a.svg": "<svg>evil</svg>"})
	client, _ := fakeRegistry(t, other, sri(ball))
	_, _, err := fetchFromRegistry(context.Background(), client, "@tabler/icons", "3.51.0", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "integrity mismatch") {
		t.Fatalf("err = %v, want an integrity mismatch", err)
	}
}

func TestRegistryFetchRefusesAnUncheckableTarball(t *testing.T) {
	ball := tgz(t, map[string]string{"package/a.svg": "<svg/>"})
	client, _ := fakeRegistry(t, ball, "")
	_, _, err := fetchFromRegistry(context.Background(), client, "@tabler/icons", "3.51.0", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "refusing an unchecked tarball") {
		t.Fatalf("err = %v", err)
	}
}

func fakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// FindNpm's order: the npm_path setting, ICHAVA_NPM, PATH, then the usual
// install directories. Its failure says everything it searched.
func TestFindNpmOrderAndMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits")
	}
	usual := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ICHAVA_NPM", "")
	old := npmDirs
	npmDirs = func() []string { return []string{usual} }
	t.Cleanup(func() { npmDirs = old; SetNpmPath("") })

	if _, err := FindNpm(); err == nil {
		t.Fatal("found an npm in an empty world")
	} else {
		for _, want := range []string{"npm not found", usual, "npm_path", "ICHAVA_NPM"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("message %q lacks %q", err, want)
			}
		}
	}

	inUsual := fakeExe(t, usual, "npm")
	if s, err := FindNpm(); err != nil || s.Found != inUsual {
		t.Fatalf("usual dir: %+v %v", s, err)
	}
	env := fakeExe(t, t.TempDir(), "npm")
	t.Setenv("ICHAVA_NPM", env)
	if s, _ := FindNpm(); s.Found != env {
		t.Errorf("ICHAVA_NPM not preferred: %s", s.Found)
	}
	setting := fakeExe(t, t.TempDir(), "npm")
	SetNpmPath(setting)
	if s, _ := FindNpm(); s.Found != setting {
		t.Errorf("npm_path not preferred: %s", s.Found)
	}
}

func TestNewestFirstOrdersNodeVersionsNumerically(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"v9.1.0", "v22.3.0", "v18.20.1"} {
		if err := os.MkdirAll(filepath.Join(root, v, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := newestFirst(filepath.Join(root, "*", "bin"))
	if len(got) != 3 || !strings.Contains(got[0], "v22.3.0") || !strings.Contains(got[2], "v9.1.0") {
		t.Fatalf("order %v", got)
	}
}
