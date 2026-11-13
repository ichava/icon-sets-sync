package recipes

import (
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
)

// mustManifestPath is ManifestPath for a pack name the test chose.
func mustManifestPath(t *testing.T, dir, pack string) string {
	t.Helper()
	path, err := manifestpack.ManifestPath(dir, pack)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
