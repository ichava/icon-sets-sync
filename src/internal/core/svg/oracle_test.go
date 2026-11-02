package svg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oracleRecord is one icon as the Python/lxml filter saw it. Its verdicts are
// frozen in testdata/lxml-oracle.json, over the same even sample of the
// vendored corpus TestCorpusAgreesWithLxml runs live, so the differential keeps
// working once the Python implementation is removed and there is no lxml to
// run. Records are keyed by the file's content hash, so
// an icon an upstream refresh has since changed is simply not compared rather
// than compared against a verdict about different bytes.
type oracleRecord struct {
	Path            string   `json:"path"`
	SHA256          string   `json:"sha256"`
	Error           string   `json:"error"`
	Removed         []string `json:"removed"`
	InventorySHA256 string   `json:"inventory_sha256"`
	InventoryLen    int      `json:"inventory_len"`
}

type oracleDoc struct {
	SampleLimit int            `json:"sample_limit"`
	Records     []oracleRecord `json:"records"`
}

func loadOracle(t *testing.T) oracleDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "lxml-oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc oracleDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// The oracle must be there and the size it was recorded at. A truncated or
// emptied file would otherwise make the differential below compare nothing.
func TestOracleIsIntact(t *testing.T) {
	doc := loadOracle(t)
	if len(doc.Records) < 1000 {
		t.Fatalf("oracle holds %d records, want at least 1000", len(doc.Records))
	}
	// The oracle is committed to a public repository, so it may only sample
	// the public packs. Records from a private pack were dropped on
	// 2026-10-26, leaving 1,238 of the 1,274 recorded.
	public := map[string]int{"icon-sets-emoji": 0, "icon-sets-flag": 0, "icon-sets-tabler": 0}
	for i, r := range doc.Records {
		if len(r.SHA256) != 64 || (r.Error == "" && len(r.InventorySHA256) != 64) {
			t.Fatalf("record %d (%s) is malformed", i, r.Path)
		}
		pack, _, _ := strings.Cut(r.Path, "/")
		if _, ok := public[pack]; !ok {
			t.Errorf("record %d (%s) samples %s, which is not a public pack", i, r.Path, pack)
			continue
		}
		public[pack]++
	}
	for pack, n := range public {
		if n == 0 {
			t.Errorf("the oracle holds no record from %s", pack)
		}
	}
}

func inventoryHash(inv []string) string {
	sum := sha256.Sum256([]byte(strings.Join(inv, "\n")))
	return hex.EncodeToString(sum[:])
}

// TestCorpusAgreesWithTheFrozenOracle is the gate that survives the Python: for every oracle icon still present in
// a sibling checkout with the same bytes, the Go filter must remove exactly
// what lxml removed and leave exactly the structure lxml left. It does not
// compare bytes -- quoting, self-closing style and whitespace differ between
// any two serialisers and no consumer can observe them.
func TestCorpusAgreesWithTheFrozenOracle(t *testing.T) {
	doc := loadOracle(t)
	root := packsRoot(t)

	var present, compared, bothRejected, mismatches int
	for _, want := range doc.Records {
		path := filepath.Join(root, want.Path)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		present++
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != want.SHA256 {
			continue // changed upstream since the oracle was recorded
		}

		serialised, removed, gerr := SanitiseBytes(raw, false)
		if want.Error != "" {
			if gerr == nil {
				mismatches++
				t.Errorf("%s: lxml refused (%s) but Go accepted it", want.Path, want.Error)
			} else {
				bothRejected++
			}
			continue
		}
		if gerr != nil {
			mismatches++
			t.Errorf("%s: Go refused (%v) but lxml accepted it", want.Path, gerr)
			continue
		}
		compared++

		if got := dedupeSorted(removed); !equal(got, want.Removed) {
			mismatches++
			t.Errorf("%s: removed differs\n  go:   %v\n  lxml: %v", want.Path, got, want.Removed)
		} else {
			reparsed, err := Parse(serialised, MustLoad().StripComments)
			if err != nil {
				t.Errorf("%s: Go output does not re-parse: %v", want.Path, err)
				continue
			}
			inv := Inventory(reparsed)
			if len(inv) != want.InventoryLen || inventoryHash(inv) != want.InventorySHA256 {
				mismatches++
				t.Errorf("%s: surviving structure differs (%d entries, lxml left %d)",
					want.Path, len(inv), want.InventoryLen)
			}
		}
		if mismatches > 10 {
			t.Fatal("too many mismatches; stopping")
		}
	}

	if present == 0 {
		t.Skipf("no sibling pack checkouts under %s; TestOracleIsIntact still guards the oracle", root)
	}
	// Assert the size of what was inspected: siblings present but no file
	// matching the oracle means the comparison ran on nothing.
	if compared+bothRejected == 0 {
		t.Fatalf("%d oracle icons found on disk but none with the recorded bytes", present)
	}
	t.Logf("agrees with lxml on %d of %d oracle icons present (%d changed since recording, %d rejected by both)",
		compared, present, present-compared-bothRejected, bothRejected)
}
