package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPrivateConfigCandidatesFindsPackDevDirs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, p := range []string{
		filepath.Join(a, "aggregate", ".dev", "icon-sets-sync", "packs.json"),
		filepath.Join(b, "aggregate", ".dev", "icon-sets-sync", "packs.json"),
		filepath.Join(a, "icon-sets-flag", ".dev", "other", "packs.json"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"packs":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := privateConfigCandidatesIn([]string{a, b, a, filepath.Join(a, "nope")})
	want := []string{filepath.Join(a, "aggregate", ".dev", "icon-sets-sync"), filepath.Join(b, "aggregate", ".dev", "icon-sets-sync")}
	if len(want) == 2 && want[0] > want[1] {
		want[0], want[1] = want[1], want[0]
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
