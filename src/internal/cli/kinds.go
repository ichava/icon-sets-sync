package cli

import (
	"github.com/spf13/pflag"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/kinds"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
)

// kindFlags are the row-7 flags `check` and `sync` share.
type kindFlags struct{ mode, dir string }

func addKindFlags(f *pflag.FlagSet, kf *kindFlags) {
	f.StringVar(&kf.mode, "suggest-kinds", "",
		"suggest packs' items that look like illustrations; =all also covers items whose kind is only the pack's package.kind. Writes <dir>/<pack>.json and never changes a kind")
	// Bare --suggest-kinds means the default mode, as core's doctor option does.
	f.Lookup("suggest-kinds").NoOptDefVal = string(kinds.ModeDefault)
	f.StringVar(&kf.dir, "kind-suggestions-dir", app.DefaultKindSuggestionsDir,
		"where --suggest-kinds writes its files")
}

// enabled reports whether the flag was given.
func (kf *kindFlags) enabled() bool { return kf.mode != "" }

// validate rejects a bad mode before any work is done.
func (kf *kindFlags) validate() error {
	if !kf.enabled() {
		return nil
	}
	if _, err := kinds.ParseMode(kf.mode); err != nil {
		return usageErr("--suggest-kinds: %s", err)
	}
	return nil
}

// report runs the scan and says what it found, on stderr so a --json stdout
// stays one document. A problem here is a warning: the check or sync it rides
// on keeps its own exit code.
func (kf *kindFlags) report(packs []*config.PackConfig) {
	if !kf.enabled() {
		return
	}
	mode, err := kinds.ParseMode(kf.mode)
	if err != nil {
		return // validate() has already refused it
	}

	for _, r := range app.SuggestKinds(packs, mode, kf.dir) {
		if r.Err != nil {
			ui.Warning("%s: kind suggestions skipped: %v", r.Pack, r.Err)
			continue
		}
		ui.Info("%s: %d kind suggestion(s) among %d item(s) -> %s", r.Pack, r.Records, r.Items, r.File)
		for _, is := range r.Issues {
			ui.Warning("%s: %s at %s: %s", r.Pack, is.Code, is.Where, is.Detail)
		}
		if n := len(r.Skipped); n > 0 {
			ui.Warning("%s: %d item(s) could not be measured, first: %s", r.Pack, n, r.Skipped[0])
		}
	}
}
