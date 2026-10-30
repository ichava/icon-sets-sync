package app

import (
	"fmt"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// Level is how serious one validation finding is.
type Level string

// The levels a finding can have. Only LevelError makes validation fail.
const (
	LevelOK    Level = "ok"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Finding is one line of a validation report.
type Finding struct {
	Level Level  `json:"level"`
	Text  string `json:"text"`
}

// Problems counts the error-level findings.
func Problems(fs []Finding) int {
	n := 0
	for _, f := range fs {
		if f.Level == LevelError {
			n++
		}
	}
	return n
}

// ValidateConfigs validates the registry and every pack it lists, the same
// checks `config validate` runs with no arguments, in the same order.
//
// The global config is not covered: it has already been loaded, and so
// validated, by whoever built the stack.
func ValidateConfigs(dir string, stack settings.Stack) []Finding {
	registry, err := config.LoadRegistry(dir)
	if err != nil {
		return []Finding{{LevelError, err.Error()}}
	}
	where := dir
	if private := config.PrivateConfigDir(); private != "" {
		where += " and the private config dir " + private
	}
	out := []Finding{{LevelOK, fmt.Sprintf("registry: %d packs in %s", len(registry.Packs), where)}}
	for _, slug := range registry.Packs {
		out = append(out, ValidatePack(dir, slug, stack)...)
	}
	return out
}

// ValidatePack checks one registered pack: that it loads against the schema,
// that its name matches its slug, that its settings block resolves, that a
// recipe can run it and, for a manifest-pack, that its set manifest loads.
// A pack with no sinks is a warning, not an error.
func ValidatePack(dir, slug string, stack settings.Stack) []Finding {
	path, err := config.PackFile(slug, dir)
	if err != nil {
		return bad("%s", err)
	}
	pack, err := config.LoadPackFile(path)
	if err != nil {
		return bad("%s", err)
	}
	return checkPack(dir, slug, path, pack, stack)
}

// ValidatePackBytes runs ValidatePack's checks on a document that is not yet
// on disk -- the bytes an editor is about to save to path.
func ValidatePackBytes(dir, slug, path string, data []byte, stack settings.Stack) []Finding {
	pack, err := config.ParsePackBytes(path, data)
	if err != nil {
		return bad("%s", err)
	}
	return checkPack(dir, slug, path, pack, stack)
}

func bad(format string, a ...any) []Finding {
	return []Finding{{LevelError, fmt.Sprintf(format, a...)}}
}

// checkPack is every check after the document has loaded.
func checkPack(dir, slug, path string, pack *config.PackConfig, stack settings.Stack) []Finding {
	if pack.Name != slug {
		return bad("%s: name %q does not match its registry slug %q", path, pack.Name, slug)
	}
	if _, err := PackSettings(stack, pack); err != nil {
		return bad("%s: %s", path, err)
	}
	recipe := recipes.Select(pack)
	if recipe == "" {
		return bad("%s: no recipe can run source.type=%q; set `recipe`", path, pack.Source.Type)
	}
	if recipe == recipes.RecipeManifestPack {
		if _, err := manifestpack.LoadManifest(dir, pack.Name); err != nil {
			return bad("%s: %s", path, err)
		}
	}
	var out []Finding
	if len(pack.Sinks) == 0 {
		out = append(out, Finding{LevelWarn, fmt.Sprintf("%s: declares no sinks; sync will refuse it outside --dry-run", path)})
	}
	return append(out, Finding{LevelOK, fmt.Sprintf("%s (recipe %s)", path, recipe)})
}
