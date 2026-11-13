package sources

import (
	"context"
	"crypto/sha1" //nolint:gosec // npm's legacy shasum, checked only when a version publishes no sha512 integrity
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ichava/icon-sets-sync/src/internal/core/httpx"
	"github.com/ichava/icon-sets-sync/src/internal/core/toolpath"
)

// NpmRegistry is where a package's tarball is fetched from without npm.
const NpmRegistry = "https://registry.npmjs.org"

// npmPath is the run-wide npm_path setting, applied once at startup the way
// allowed_hosts is.
var (
	npmMu   sync.RWMutex
	npmPath string
)

// SetNpmPath sets the npm_path setting: the npm executable to use before
// ICHAVA_NPM, PATH and the usual install directories are searched. Empty
// clears it.
func SetNpmPath(p string) {
	npmMu.Lock()
	defer npmMu.Unlock()
	npmPath = p
}

func configuredNpmPath() string {
	npmMu.RLock()
	defer npmMu.RUnlock()
	return npmPath
}

// NpmSearch is where FindNpm looked, in order, for the message when it found
// nothing and for doctor.
type NpmSearch struct {
	Found    string
	Searched []string
}

// npmEnv and npmDirs are seams for tests.
var (
	npmEnv  = os.Getenv
	npmDirs = func() []string { return toolpath.Dirs() }
)

// FindNpm resolves the npm executable: the npm_path setting, then ICHAVA_NPM,
// then PATH, then the directories node version managers and installers use.
//
// A server started from Finder, launchd or a desktop shortcut has none of
// Homebrew, nvm, Volta or fnm on its PATH, which is why PATH alone is not
// enough: npm is usually installed and simply not visible.
func FindNpm() (NpmSearch, error) {
	var s NpmSearch
	try := func(label, p string) bool {
		if p == "" {
			return false
		}
		s.Searched = append(s.Searched, label+" "+p)
		if toolpath.IsExecutable(p) {
			s.Found = p
			return true
		}
		return false
	}
	if try("npm_path setting:", configuredNpmPath()) {
		return s, nil
	}
	if try("ICHAVA_NPM:", npmEnv("ICHAVA_NPM")) {
		return s, nil
	}
	if p, err := exec.LookPath("npm"); err == nil {
		s.Found = p
		s.Searched = append(s.Searched, "PATH: "+p)
		return s, nil
	}
	s.Searched = append(s.Searched, "PATH ("+npmEnv("PATH")+")")
	for _, dir := range npmDirs() {
		for _, name := range toolpath.Names("npm") {
			if try("", filepath.Join(dir, name)) {
				return s, nil
			}
		}
	}
	return s, fmt.Errorf("npm not found; looked in: %s. Install Node.js, or set npm_path "+
		"(ICHAVA_NPM, or --set npm_path=<path to npm>)", strings.Join(trimLabels(s.Searched), "; "))
}

func trimLabels(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

// registryVersion is the part of a registry version document used here.
type registryVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Dist    struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
		Shasum    string `json:"shasum"`
	} `json:"dist"`
}

// registryURL is the version document for pkg at version (or a dist-tag such
// as "latest"), in the form the upstream checker already requests.
func registryURL(pkg, version string) string {
	return NpmRegistry + "/" + pkg + "/" + url.PathEscape(version)
}

// fetchFromRegistry downloads pkg@version's tarball into dir without npm and
// checks it against the integrity the registry publishes. It returns the
// tarball's path and the concrete version the registry resolved.
func fetchFromRegistry(ctx context.Context, client *httpx.Client, pkg, version, dir string) (string, string, error) {
	var doc registryVersion
	if err := client.GetJSON(ctx, registryURL(pkg, version), &doc); err != nil {
		return "", "", err
	}
	if doc.Dist.Tarball == "" || doc.Version == "" {
		return "", "", fmt.Errorf("registry: %s@%s has no dist.tarball", pkg, version)
	}
	if doc.Dist.Integrity == "" && doc.Dist.Shasum == "" {
		return "", "", fmt.Errorf("registry: %s@%s publishes no integrity or shasum; refusing an unchecked tarball", pkg, doc.Version)
	}
	u, err := url.Parse(doc.Dist.Tarball)
	if err != nil {
		return "", "", fmt.Errorf("registry: tarball URL %q: %w", doc.Dist.Tarball, err)
	}
	dest := filepath.Join(dir, path.Base(u.Path))
	if filepath.Dir(dest) != filepath.Clean(dir) || !strings.HasSuffix(dest, ".tgz") {
		return "", "", fmt.Errorf("registry: unexpected tarball name in %q", doc.Dist.Tarball)
	}
	if _, err := client.Download(ctx, doc.Dist.Tarball, dest, true); err != nil {
		return "", "", err
	}
	if err := checkIntegrity(dest, doc.Dist.Integrity, doc.Dist.Shasum); err != nil {
		_ = os.Remove(dest)
		return "", "", fmt.Errorf("registry: %s@%s: %w", pkg, doc.Version, err)
	}
	return dest, doc.Version, nil
}

// checkIntegrity verifies a file against an SRI string (sha512 preferred) or,
// failing that, npm's legacy hex sha1 shasum.
func checkIntegrity(file, integrity, shasum string) error {
	for _, entry := range strings.Fields(integrity) {
		algo, want, ok := strings.Cut(entry, "-")
		if !ok || algo != "sha512" {
			continue
		}
		got, err := digest(file, sha512.New())
		if err != nil {
			return err
		}
		if base64.StdEncoding.EncodeToString(got) != want {
			return fmt.Errorf("integrity mismatch: the tarball is not the one the registry published (sha512)")
		}
		return nil
	}
	if shasum == "" {
		return fmt.Errorf("no sha512 integrity to check (have %q)", integrity)
	}
	got, err := digest(file, sha1.New()) //nolint:gosec // see the import
	if err != nil {
		return err
	}
	if hex.EncodeToString(got) != strings.ToLower(shasum) {
		return fmt.Errorf("integrity mismatch: the tarball is not the one the registry published (sha1)")
	}
	return nil
}

func digest(file string, h interface {
	io.Writer
	Sum([]byte) []byte
}) ([]byte, error) {
	f, err := os.Open(file) //nolint:gosec // a path this package just wrote
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
