package jsonlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendRotatesAndTails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "x.jsonl")
	l := &Log{Path: path, MaxBytes: 64, Keep: 2}
	for i := 0; i < 10; i++ {
		if err := l.Append(map[string]int{"n": i, "padding": 1234567}); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() > 64 {
		t.Fatalf("current file: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Fatal("no second rotated file")
	}
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Fatal("kept more than Keep rotated files")
	}
	recs, err := l.Tail(1)
	if err != nil || len(recs) != 1 || string(recs[0]) != `{"n":9,"padding":1234567}` {
		t.Fatalf("%s, %v", recs, err)
	}
	var none *Log
	if err := none.Append(1); err != nil {
		t.Fatal("a nil log keeps nothing, without error")
	}
}
