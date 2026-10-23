package web

import (
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// The ui-a11y CI job serves this fixture; a fixture that stopped validating
// would fail that job for a reason that has nothing to do with accessibility.
func TestTheA11yFixtureValidates(t *testing.T) {
	findings := app.ValidateConfigs(filepath.Join("testdata", "a11y", "config"), settings.Stack{})
	if n := app.Problems(findings); n != 0 {
		t.Fatalf("%d problems: %+v", n, findings)
	}
	if len(findings) < 3 {
		t.Fatalf("only %d findings; the registry lists two packs", len(findings))
	}
}
