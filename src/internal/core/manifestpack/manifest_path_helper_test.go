package manifestpack

import "testing"

// mustManifestPath is ManifestPath for a pack name the test chose.
func mustManifestPath(t *testing.T, dir, pack string) string {
	t.Helper()
	path, err := ManifestPath(dir, pack)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
