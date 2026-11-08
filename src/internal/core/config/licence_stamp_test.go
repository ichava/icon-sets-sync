package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// licencedPack writes a pack whose config.json records its upstream licence
// by hash, as bootstrap, heroicons, lucide and simple-icons do, and whose
// vendored licence file holds body.
func licencedPack(t *testing.T, recorded, body string) (*PackConfig, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "resources", "assets", "svg")
	if err := os.MkdirAll(filepath.Join(dir, "licenses", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "licenses", "demo", "LICENSE"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(recorded))
	cfg := `{
  "package": {"version": "0.1.0", "upstream_version": "1.68.0"},
  "upstream": {"current_version": "1.68.0"},
  "license": {
    "spdx": "ISC",
    "file": "resources/assets/svg/licenses/demo/LICENSE",
    "sha256": "` + hex.EncodeToString(sum[:]) + `",
    "checked_at": "2026-11-04",
    "checked_version": "1.68.0"
  }
}
`
	target := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return &PackConfig{PackRoot: root, VersionFile: DefaultVersionFile, VersionKeys: DefaultVersionKeys()}, target
}

// R7: the sync stamped the new upstream version but left
// license.checked_version behind, so each of the four packs that record their
// licence by hash failed its own AttributionTest on every sync PR
// (icon-sets-lucide#6). When the vendored licence still hashes to the
// recorded value, the licence has been checked at the new version too.
func TestStampAdvancesTheLicenceCheckWhenTheHashStillMatches(t *testing.T) {
	stampNow = func() time.Time { return time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { stampNow = time.Now })
	pack, target := licencedPack(t, "ISC licence text\n", "ISC licence text\n")

	if _, err := WriteVendoredVersion(pack, "1.70.0"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(target)
	if got, _ := getStringAtPath(raw, "license.checked_version"); got != "1.70.0" {
		t.Errorf("license.checked_version = %q, want 1.70.0", got)
	}
	if got, _ := getStringAtPath(raw, "license.checked_at"); got != "2026-11-08" {
		t.Errorf("license.checked_at = %q, want 2026-11-08", got)
	}
}

// A licence that changed upstream is not re-checked by a machine: both fields
// stay, so the pack's own test fails and a person reads the new terms.
func TestStampLeavesAChangedLicenceUnchecked(t *testing.T) {
	pack, target := licencedPack(t, "ISC licence text\n", "A different licence\n")

	if _, err := WriteVendoredVersion(pack, "1.70.0"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(target)
	if got, _ := getStringAtPath(raw, "license.checked_version"); got != "1.68.0" {
		t.Errorf("license.checked_version = %q, want it left at 1.68.0", got)
	}
	if got, _ := getStringAtPath(raw, "license.checked_at"); got != "2026-11-04" {
		t.Errorf("license.checked_at = %q, want it left at 2026-11-04", got)
	}
	if got, _ := getStringAtPath(raw, "upstream.current_version"); got != "1.70.0" {
		t.Errorf("upstream.current_version = %q, want 1.70.0", got)
	}
}
