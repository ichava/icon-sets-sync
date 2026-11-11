package webvendor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func integrity(b []byte) string {
	s := sha512.Sum512(b)
	return "sha512-" + base64.StdEncoding.EncodeToString(s[:])
}

// fakeRegistry serves packuments, version manifests, tarballs and the tagged
// Tabler licence from memory.
type fakeRegistry map[string][]byte

func (f fakeRegistry) Get(u string) ([]byte, error) {
	b, ok := f[u]
	if !ok {
		return nil, fmt.Errorf("GET %s: 404", u)
	}
	return b, nil
}

func (f fakeRegistry) publish(t *testing.T, pkg, version string, files map[string]string, deps map[string]string) {
	t.Helper()
	tarURL := Registry + "/" + escape(pkg) + "/-/" + version + ".tgz"
	tb := tgz(t, files)
	f[tarURL] = tb
	m, _ := json.Marshal(map[string]any{
		"version": version, "dependencies": deps,
		"dist": map[string]string{"tarball": tarURL, "integrity": integrity(tb)},
	})
	f[Registry+"/"+escape(pkg)+"/"+version] = m
	p, _ := json.Marshal(map[string]any{"dist-tags": map[string]string{"latest": version}})
	f[Registry+"/"+escape(pkg)] = p
}

const mit = "MIT License\n\nPermission is hereby granted, free of charge...\n"

func fakeTabler(t *testing.T, coreVersion string) fakeRegistry {
	t.Helper()
	f := fakeRegistry{}
	banner := "/*! Tabler v" + coreVersion + " (https://tabler.io) */"
	f.publish(t, PkgCore, coreVersion, map[string]string{
		"package/dist/css/tabler.min.css":     banner + "body{}",
		"package/dist/js/tabler.min.js":       banner + `class X{static get VERSION(){return"5.3.8"}}`,
		"package/dist/js/tabler-theme.min.js": banner,
		"package/../../escape":                "must be ignored",
	}, map[string]string{PkgPopper: "^2.11.8"})
	f.publish(t, PkgIcons, "3.49.0", map[string]string{
		"package/LICENSE": mit,
		"package/dist/tabler-sprite.svg": `<svg><defs><symbol id="tabler-check" viewBox="0 0 24 24"><path d="M5 12l5 5"/></symbol>` +
			`<symbol id="tabler-x" viewBox="0 0 24 24"><path d="M18 6l-12 12"/></symbol><symbol id="tabler-unused"><path/></symbol></defs></svg>`,
	}, nil)
	f.publish(t, PkgWebfont, "3.49.0", map[string]string{
		"package/LICENSE":                       mit,
		"package/dist/fonts/tabler-icons.woff2": "wOF2-fake-font",
		"package/dist/tabler-icons.min.css": `/*! Tabler Icons 3.49.0 */@font-face{font-family:"tabler-icons";src:url("./fonts/tabler-icons.woff2?v3.49.0") format("woff2")}` +
			`.ti{font-family:"tabler-icons" !important;line-height:1}.ti-check:before{content:"\ea5e"}.ti-x:before{content:"\eb55"}.ti-unused:before{content:"\ffff"}`,
	}, nil)
	f.publish(t, PkgBootstrap, "5.3.8", map[string]string{"package/LICENSE": mit}, nil)
	f.publish(t, PkgPopper, "2.11.8", map[string]string{"package/LICENSE.md": mit}, nil)
	f[CoreLicenceURL(coreVersion)] = []byte(mit)
	return f
}

func vendorDir(t *testing.T) (*Vendor, string) {
	t.Helper()
	root := t.TempDir()
	assets := filepath.Join(root, "src", "internal", "web", "assets", "vendor")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "icons.txt"), []byte("# used\ncheck\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Vendor{Root: root, AssetDir: assets, Log: func(string, ...any) {}}, assets
}

func TestRunLatestVendorsEverythingTogether(t *testing.T) {
	v, assets := vendorDir(t)
	v.Fetch = fakeTabler(t, "1.7.1")
	if err := v.Run(true); err != nil {
		t.Fatal(err)
	}
	vers, err := os.ReadFile(filepath.Join(assets, VersionFile))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseVersions(vers)
	if err != nil {
		t.Fatal(err)
	}
	if parsed[PkgCore] != "1.7.1" || parsed[PkgBootstrap] != "5.3.8" || parsed[PkgPopper] != "2.11.8" {
		t.Fatalf("pinned %v", parsed)
	}
	sprite, _ := os.ReadFile(filepath.Join(assets, "tabler-icons.svg"))
	if !strings.Contains(string(sprite), "tabler-check") || strings.Contains(string(sprite), "tabler-unused") {
		t.Errorf("subset is wrong:\n%s", sprite)
	}
	css, _ := os.ReadFile(filepath.Join(assets, "tabler-icons", "tabler-icons.css"))
	for _, want := range []string{`.ti-check:before{content:"\ea5e"}`, `.ti-x:before`, `src:url("tabler-icons.woff2") format("woff2")`, `.ti{font-family:"tabler-icons"`} {
		if !strings.Contains(string(css), want) {
			t.Errorf("webfont CSS lacks %s:\n%s", want, css)
		}
	}
	if strings.Contains(string(css), "ti-unused") || strings.Contains(string(css), "./fonts/") {
		t.Errorf("webfont CSS is not cut to icons.txt or keeps the upstream font path:\n%s", css)
	}
	if font, _ := os.ReadFile(filepath.Join(assets, "tabler-icons", "tabler-icons.woff2")); string(font) != "wOF2-fake-font" {
		t.Errorf("woff2 = %q", font)
	}
	manifest, _ := os.ReadFile(filepath.Join(assets, "MANIFEST.sha256"))
	for _, want := range []string{"tabler/tabler.min.css", "LICENSES/popper.txt", VersionFile, "icons.txt", "tabler-icons.svg",
		"tabler-icons/tabler-icons.css", "tabler-icons/tabler-icons.woff2", "LICENSES/tabler-icons-webfont.txt"} {
		if !strings.Contains(string(manifest), "  "+want+"\n") {
			t.Errorf("manifest lacks %s", want)
		}
	}
	notices, _ := os.ReadFile(filepath.Join(v.Root, "THIRD_PARTY_NOTICES.md"))
	if !strings.Contains(string(notices), "## @tabler/core 1.7.1") || strings.Count(string(notices), "Permission is hereby granted") != 5 {
		t.Errorf("notices:\n%s", notices)
	}
	if _, err := os.Stat(filepath.Join(v.Root, "escape")); err == nil {
		t.Error("a traversing tar entry was written")
	}

	// Re-vendoring the pin reproduces the same files.
	before, _ := os.ReadFile(filepath.Join(assets, "MANIFEST.sha256"))
	if err := v.Run(false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(assets, "MANIFEST.sha256"))
	if !bytes.Equal(before, after) {
		t.Error("make tabler-vendor did not reproduce make tabler-update's output")
	}
}

func TestRunRefusesATamperedTarball(t *testing.T) {
	v, _ := vendorDir(t)
	f := fakeTabler(t, "1.7.1")
	tarURL := Registry + "/" + escape(PkgCore) + "/-/1.7.1.tgz"
	f[tarURL] = append([]byte{}, f[tarURL]...)
	f[tarURL][len(f[tarURL])-1] ^= 0xff
	v.Fetch = f
	if err := v.Run(true); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("a tampered tarball was accepted: %v", err)
	}
}

func TestRunRefusesAMissingIcon(t *testing.T) {
	v, assets := vendorDir(t)
	if err := os.WriteFile(filepath.Join(assets, "icons.txt"), []byte("check\ngone-upstream\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v.Fetch = fakeTabler(t, "1.7.1")
	if err := v.Run(true); err == nil || !strings.Contains(err.Error(), "gone-upstream") {
		t.Fatalf("err %v", err)
	}
}

func TestRunPinnedRefusesABootstrapMismatch(t *testing.T) {
	v, assets := vendorDir(t)
	pin := Versions{PkgCore: "1.7.1", PkgIcons: "3.49.0", PkgBootstrap: "5.3.7", PkgPopper: "2.11.8"}
	if err := os.WriteFile(filepath.Join(assets, VersionFile), pin.Format(), 0o644); err != nil {
		t.Fatal(err)
	}
	v.Fetch = fakeTabler(t, "1.7.1")
	if err := v.Run(false); err == nil || !strings.Contains(err.Error(), "Bootstrap 5.3.8") {
		t.Fatalf("err %v", err)
	}
}

func TestParseVersions(t *testing.T) {
	if _, err := ParseVersions([]byte("@tabler/core 1.7.1\n")); err == nil {
		t.Error("a file missing three packages parsed")
	}
	if _, err := ParseVersions([]byte("@tabler/core\n")); err == nil {
		t.Error("a line without a version parsed")
	}
	v := Versions{PkgCore: "1", PkgIcons: "2", PkgBootstrap: "3", PkgPopper: "4"}
	got, err := ParseVersions(v.Format())
	if err != nil || len(got) != 4 || got[PkgPopper] != "4" {
		t.Fatalf("round trip %v %v", got, err)
	}
}

func TestSameMajor(t *testing.T) {
	for _, c := range []struct {
		r, v string
		ok   bool
	}{{"^2.11.8", "2.11.8", true}, {"^2.11.8", "3.0.0", false}, {"~2.1", "2.9.9", true}, {"", "2.0.0", false}} {
		if sameMajor(c.r, c.v) != c.ok {
			t.Errorf("sameMajor(%q, %q)", c.r, c.v)
		}
	}
}

func TestCheckReportsAPinBehindTheRegistry(t *testing.T) {
	v, assets := vendorDir(t)
	v.Fetch = fakeTabler(t, "1.7.1")
	pin := Versions{PkgCore: "1.7.0", PkgIcons: "3.49.0", PkgBootstrap: "5.3.8", PkgPopper: "2.11.8"}
	if err := os.WriteFile(filepath.Join(assets, VersionFile), pin.Format(), 0o644); err != nil {
		t.Fatal(err)
	}
	behind, err := v.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(behind) != 1 || !strings.Contains(behind[0], "pinned at 1.7.0; 1.7.1 is out") {
		t.Fatalf("behind %v", behind)
	}
	pin[PkgCore] = "1.7.1"
	if err := os.WriteFile(filepath.Join(assets, VersionFile), pin.Format(), 0o644); err != nil {
		t.Fatal(err)
	}
	if behind, err := v.Check(); err != nil || len(behind) != 0 {
		t.Fatalf("an up-to-date pin reported %v %v", behind, err)
	}
}

// G-03: the vendor fetches through httpx, so the upstream allowlist, the
// https-only redirect guard and the size caps apply here too. A host off
// the allowlist is refused before any request is sent.
func TestHTTPFetcherRefusesAHostOffTheAllowlist(t *testing.T) {
	_, err := HTTPFetcher{}.Get("https://not-on-the-allowlist.invalid/pkg.tgz")
	if err == nil || !strings.Contains(err.Error(), "not on the upstream allowlist") {
		t.Fatalf("err = %v, want an allowlist refusal", err)
	}
}

func TestHTTPFetcherRefusesPlainHTTP(t *testing.T) {
	if _, err := (HTTPFetcher{}).Get("http://registry.npmjs.org/x"); err == nil {
		t.Fatal("plain http to a non-loopback host was not refused")
	}
}

// R6: @tabler/core's tarball ships no LICENSE, so the vendor reads it from
// raw.githubusercontent.com -- a host the upstream allowlist (G-03) refuses.
// `make tabler-vendor` has failed since the vendor moved onto httpx. The
// fetcher allows the hosts of the URLs this package itself builds, and no
// other.
func TestHTTPFetcherAllowsTheLicenceHost(t *testing.T) {
	c := HTTPFetcher{}.client()
	if err := c.CheckURL(CoreLicenceURL("1.7.1")); err != nil {
		t.Errorf("the core licence URL is refused: %v", err)
	}
	if err := c.CheckURL("https://raw.githubusercontent.com.evil.invalid/x"); err == nil {
		t.Error("a look-alike host was allowed")
	}
}

func TestSubsetWebfontCSS(t *testing.T) {
	css := []byte(`@font-face{src:url("./fonts/tabler-icons.woff2?v1") format("woff2"),url("./fonts/tabler-icons.ttf") format("truetype")}.ti{font-family:"tabler-icons"}.ti-check:before{content:"\\ea5e"}`)
	if _, err := SubsetWebfontCSS(css, []string{"check", "gone"}); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("a missing icon was not refused: %v", err)
	}
	out, err := SubsetWebfontCSS(css, []string{"check"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "truetype") {
		t.Errorf("the subset keeps a non-woff2 source:\n%s", out)
	}
}
