package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The settings table in docs/configuration.md is hand-written. This keeps it
// from losing a key: every setting and every environment variable it reads
// must appear there.
func TestEverySettingIsDocumented(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "docs", "configuration.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	doc := string(data)
	checked := 0
	for _, k := range Keys {
		for _, want := range append([]string{"`" + k.Name + "`", k.Env}, k.LegacyEnv...) {
			checked++
			if !strings.Contains(doc, want) {
				t.Errorf("%s does not mention %s", path, want)
			}
		}
	}
	if checked < len(Keys)*2 {
		t.Fatalf("checked %d names for %d keys", checked, len(Keys))
	}
}
