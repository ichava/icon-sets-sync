package confighistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveListReadAndPrune(t *testing.T) {
	h := Store{Dir: t.TempDir(), Keep: 3}
	file := filepath.Join(t.TempDir(), "config.json")
	clock := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	h.Now = func() time.Time { clock = clock.Add(time.Second); return clock }

	for i := 0; i < 5; i++ {
		if _, err := h.Save(file, []byte{byte('a' + i)}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := h.List(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("kept %d, want 3 (Keep)", len(list))
	}
	if !list[0].Time.After(list[2].Time) {
		t.Errorf("not newest first: %+v", list)
	}
	data, err := h.Read(file, list[0].ID)
	if err != nil || string(data) != "e" {
		t.Fatalf("newest = %q %v", data, err)
	}
	if _, err := h.Read(file, "../../etc/passwd"); err == nil {
		t.Error("an id that escapes the history directory was read")
	}
	other := filepath.Join(t.TempDir(), "config.json")
	if l, _ := h.List(other); len(l) != 0 {
		t.Errorf("two files with one name share a history: %v", l)
	}
}

func TestKeepZeroDisablesHistory(t *testing.T) {
	h := Store{Dir: t.TempDir(), Keep: 0}
	file := filepath.Join(t.TempDir(), "c.json")
	if e, err := h.Save(file, []byte("x")); err != nil || e.ID != "" {
		t.Fatalf("%+v %v", e, err)
	}
	if entries, _ := os.ReadDir(h.Dir); len(entries) != 0 {
		t.Errorf("wrote %d entries", len(entries))
	}
}

func TestDefaultDirIsUnderTheStateHome(t *testing.T) {
	got := DefaultDir(func(k string) (string, bool) {
		if k == "XDG_STATE_HOME" {
			return "/state", true
		}
		return "", false
	})
	if got != filepath.Join("/state", "icon-sets-sync", "history") {
		t.Errorf("DefaultDir = %s", got)
	}
}

func TestUnifiedDiff(t *testing.T) {
	d := Unified("a", "b", "x\ny\nz\n", "x\nY\nz\nw\n")
	for _, want := range []string{"--- a", "+++ b", "-y", "+Y", "+w", " x"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff lacks %q:\n%s", want, d)
		}
	}
	if Unified("a", "b", "same\n", "same\n") != "" {
		t.Error("identical texts diff")
	}
}
