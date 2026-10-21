package docfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToJSON(t *testing.T) {
	tests := []struct {
		name, path, in string
		want           string
		wantErr        string
	}{
		{"json passes through", "a.json", `{"a":1}`, `{"a":1}`, ""},
		{"invalid json", "a.json", `{"a":`, "", "a.json"},
		{"yaml becomes json", "a.yaml", "a: 1\nb: [x, y]\n", `{"a":1,"b":["x","y"]}`, ""},
		{"yml too", "a.yml", "name: tabler\n", `{"name":"tabler"}`, ""},
		{"nested yaml", "a.yaml", "git:\n  base: main\n", `{"git":{"base":"main"}}`, ""},
		{"non-string key refused", "a.yaml", "1: one\n", "", "not a string"},
		{"empty yaml", "a.yaml", "", "", "empty document"},
		{"broken yaml", "a.yaml", "a: [1,\n", "", "a.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToJSON(tt.path, []byte(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFind(t *testing.T) {
	dir := t.TempDir()

	if _, err := Find(dir, "tabler"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: err = %v, want ErrNotExist", err)
	}

	yamlPath := filepath.Join(dir, "tabler.yaml")
	if err := os.WriteFile(yamlPath, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Find(dir, "tabler"); err != nil || got != yamlPath {
		t.Fatalf("yaml: %q, %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "tabler.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Find(dir, "tabler"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("both: err = %v, want ambiguous", err)
	}
}

func TestReadTree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := os.WriteFile(path, []byte("concurrency: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree, err := ReadTree(path)
	if err != nil {
		t.Fatal(err)
	}
	// YAML's int arrives as JSON's float64, whichever syntax the file used.
	if tree["concurrency"] != float64(2) {
		t.Fatalf("got %#v", tree["concurrency"])
	}

	listPath := filepath.Join(dir, "list.json")
	if err := os.WriteFile(listPath, []byte(`[1]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTree(listPath); err == nil {
		t.Fatal("a list at the top level: want an error")
	}
}
