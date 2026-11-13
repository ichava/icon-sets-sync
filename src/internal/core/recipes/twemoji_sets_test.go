package recipes

import (
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// TestTwemojiSetsBuildsTwemojiOnly: icon-sets-twemoji ships Twemoji only since
// its OpenMoji copies were removed (OpenMoji lives in icon-sets-openmoji). A
// sync that still built the two OpenMoji pipelines would put both sets back.
func TestTwemojiSetsBuildsTwemojiOnly(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-twemoji", Pack: "ichava/icon-sets-twemoji", PackRoot: "/work/icon-sets-twemoji", Recipe: RecipeTwemojiSets,
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}
	for _, dry := range []bool{true, false} {
		built, err := buildFor(emoji, "15.0.0", dry)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"twemoji-sets:twemoji@15.0.0"}
		if !dry {
			want = append(want, "twemoji-sets:commit")
		}
		var got []string
		for _, p := range built {
			got = append(got, p.Name())
			if strings.Contains(strings.ToLower(p.Name()), "openmoji") {
				t.Errorf("dry=%v: twemoji-sets built %s", dry, p.Name())
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("dry=%v: pipelines %v, want %v", dry, got, want)
		}
	}
	if _, ok := settings.Lookup("emoji.openmoji_version"); ok {
		t.Error("emoji.openmoji_version is still a setting; nothing reads it")
	}
}
