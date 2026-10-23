package web

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/tabler"
)

func vendorFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := assetFS.ReadFile("assets/vendor/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Every vendored file hashes to what MANIFEST.sha256 recorded when the
// vendoring tool wrote it, so a hand edit or a partial update fails here.
func TestVendoredFilesMatchTheManifest(t *testing.T) {
	sc := bufio.NewScanner(bytes.NewReader(vendorFile(t, "MANIFEST.sha256")))
	n := 0
	for sc.Scan() {
		sum, name, ok := strings.Cut(sc.Text(), "  ")
		if !ok {
			t.Fatalf("bad manifest line %q", sc.Text())
		}
		got := sha256.Sum256(vendorFile(t, name))
		if hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s does not match MANIFEST.sha256; re-run make tabler-vendor", name)
		}
		n++
	}
	if n < 10 {
		t.Fatalf("manifest lists %d files, want at least 10", n)
	}
}

// TABLER_VERSION is the one place the version is pinned; the vendored files
// must carry the same version in their banners.
func TestTablerVersionMatchesTheBanners(t *testing.T) {
	vers, err := tabler.ParseVersions(vendorFile(t, tabler.VersionFile))
	if err != nil {
		t.Fatal(err)
	}
	banner := "Tabler v" + vers[tabler.PkgCore] + " "
	for _, f := range []string{"tabler/tabler.min.css", "tabler/tabler.min.js", "tabler/tabler-theme.min.js"} {
		if !bytes.Contains(vendorFile(t, f), []byte(banner)) {
			t.Errorf("%s does not carry %q", f, strings.TrimSpace(banner))
		}
	}
	bs, err := tabler.BundledBootstrap(vendorFile(t, "tabler/tabler.min.js"))
	if err != nil || bs != vers[tabler.PkgBootstrap] {
		t.Errorf("bundled Bootstrap %q, pinned %q (%v)", bs, vers[tabler.PkgBootstrap], err)
	}
}

// Each vendored package has a licence text, and the repository's
// THIRD_PARTY_NOTICES.md is exactly what the tool generates from them, so the
// archives ship the same notices the binary serves.
func TestNoticesMatchTheEmbeddedLicences(t *testing.T) {
	s, _ := newTestServer(t)
	ls, err := s.embeddedLicences()
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 4 {
		t.Fatalf("%d licences", len(ls))
	}
	for _, l := range ls {
		if !bytes.Contains(l.Text, []byte("Permission is hereby granted")) {
			t.Errorf("%s: LICENSES/%s is missing or not MIT", l.Package, l.File)
		}
	}
	root, err := os.ReadFile(filepath.Join("..", "..", "..", "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(root, tabler.Notices(ls)) {
		t.Error("THIRD_PARTY_NOTICES.md is out of date; re-run make tabler-vendor")
	}
}

// iconCall matches {{icon "x"}} and the icon argument of navItem.
var iconCall = regexp.MustCompile(`(?:icon|navItem \. "[^"]+" "[^"]+") "([a-z0-9-]+)"`)

// Every icon a template asks for is in the sprite, and every icon the vendor
// list cuts is in the sprite: a missing one renders as nothing.
func TestEveryIconUsedIsInTheSprite(t *testing.T) {
	sprite := string(vendorFile(t, "tabler-icons.svg"))
	listed := tabler.ReadIconList(vendorFile(t, "icons.txt"))
	for _, n := range listed {
		if !strings.Contains(sprite, `id="tabler-`+n+`"`) {
			t.Errorf("icons.txt lists %s but the sprite lacks it", n)
		}
	}
	used := 0
	entries, _ := templateFS.ReadDir("templates")
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			sub, _ := templateFS.ReadDir("templates/" + e.Name())
			for _, f := range sub {
				files = append(files, "templates/"+e.Name()+"/"+f.Name())
			}
			continue
		}
		files = append(files, "templates/"+e.Name())
	}
	for _, f := range files {
		body, err := templateFS.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range iconCall.FindAllStringSubmatch(string(body), -1) {
			used++
			if !strings.Contains(sprite, `id="tabler-`+m[1]+`"`) {
				t.Errorf("%s uses icon %s, which is not in the sprite; add it to icons.txt", f, m[1])
			}
		}
	}
	if used < 5 {
		t.Fatalf("found %d icon uses, want at least 5", used)
	}
}
