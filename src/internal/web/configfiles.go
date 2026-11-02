package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// Config file kinds the editor knows how to validate.
const (
	KindRegistry = "registry"
	KindPack     = "pack"
	KindManifest = "manifest"
	KindGlobal   = "global"
)

// GlobalName is the editor's name for the global config file.
const GlobalName = "global"

// ConfigFile is one document the editor offers. Name is the only thing a
// request may name: the editor serves a file only if it is in this list, so
// no request can reach a path the list did not put there.
type ConfigFile struct {
	Name, Path, Kind, Slug string
}

// ConfigDoc is a file's current contents.
type ConfigDoc struct {
	ConfigFile
	Data   []byte
	Exists bool
	// Rev is the sha256 of the bytes on disk ("" when the file does not
	// exist). A save carries the Rev it was edited from, and is refused if
	// the file has changed since.
	Rev string
	// ReadOnly, with Why, when the directory cannot be written.
	ReadOnly bool
	Why      string
}

// ErrConflict is returned when a file changed on disk after it was opened.
var ErrConflict = errors.New("the file changed on disk after it was opened")

// ValidationError carries the findings that refused a save.
type ValidationError struct{ Findings []app.Finding }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%d problem(s); nothing was written", app.Problems(e.Findings))
}

func rev(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ConfigFiles implements Engine.
func (e *AppEngine) ConfigFiles() ([]ConfigFile, error) {
	dir := e.configDir()
	registryPath, err := docfile.Find(dir, "packs")
	if err != nil {
		return nil, fmt.Errorf("no pack registry in %s: %w", dir, err)
	}
	out := []ConfigFile{{Name: filepath.Base(registryPath), Path: registryPath, Kind: KindRegistry}}
	registry, regErr := config.LoadRegistry(dir)
	if regErr == nil {
		for _, slug := range registry.Packs {
			if path, err := config.PackFile(slug, dir); err == nil {
				out = append(out, ConfigFile{Name: filepath.Base(path), Path: path, Kind: KindPack, Slug: slug})
			}
			if slug == app.ManifestPack {
				m := manifestpack.ManifestPath(dir, slug)
				if _, err := os.Stat(m); err == nil {
					out = append(out, ConfigFile{Name: filepath.Base(m), Path: m, Kind: KindManifest, Slug: slug})
				}
			}
		}
	}
	if g := e.globalPath(); g != "" {
		out = append(out, ConfigFile{Name: GlobalName, Path: g, Kind: KindGlobal})
	}
	return out, nil
}

// globalPath is the global config in use, or where a new one would go.
func (e *AppEngine) globalPath() string {
	if e.GlobalPath != "" {
		return e.GlobalPath
	}
	if e.GlobalHome == "" {
		return ""
	}
	return filepath.Join(e.GlobalHome, settings.AppDir, "config.json")
}

func (e *AppEngine) configFile(name string) (ConfigFile, error) {
	files, err := e.ConfigFiles()
	if err != nil {
		return ConfigFile{}, err
	}
	for _, f := range files {
		if f.Name == name {
			return f, nil
		}
	}
	return ConfigFile{}, os.ErrNotExist
}

// ReadConfig implements Engine.
func (e *AppEngine) ReadConfig(name string) (ConfigDoc, error) {
	f, err := e.configFile(name)
	if err != nil {
		return ConfigDoc{}, err
	}
	doc := ConfigDoc{ConfigFile: f}
	data, err := os.ReadFile(f.Path)
	switch {
	case err == nil:
		doc.Data, doc.Exists, doc.Rev = data, true, rev(data)
	case errors.Is(err, os.ErrNotExist):
	default:
		return doc, err
	}
	if why := writeProbe(filepath.Dir(f.Path)); why != "" {
		doc.ReadOnly, doc.Why = true, why
	}
	return doc, nil
}

// writeProbe reports why a directory cannot be written, or "". It creates and
// removes a file rather than reading permission bits, because a read-only
// bind mount (the Docker image mounts config/ :ro) has writable-looking bits.
func writeProbe(dir string) string {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		// A new global config: its directory is created on save.
		parent := filepath.Dir(dir)
		if _, err := os.Stat(parent); err != nil {
			return ""
		}
		dir = parent
	}
	f, err := os.CreateTemp(dir, ".icon-sets-sync-probe-*")
	if err != nil {
		return fmt.Sprintf("%s cannot be written: %v", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return ""
}

// ValidateConfig implements Engine: the same loaders and checks the CLI uses,
// run on bytes that are not yet on disk.
func (e *AppEngine) ValidateConfig(name string, data []byte) []app.Finding {
	f, err := e.configFile(name)
	if err != nil {
		return []app.Finding{{Level: app.LevelError, Text: err.Error()}}
	}
	return validateDoc(f, data, e.configDir(), e.stack())
}

func validateDoc(f ConfigFile, data []byte, dir string, stack settings.Stack) []app.Finding {
	bad := func(err error) []app.Finding { return []app.Finding{{Level: app.LevelError, Text: err.Error()}} }
	ok := func(text string) []app.Finding { return []app.Finding{{Level: app.LevelOK, Text: text}} }
	switch f.Kind {
	case KindPack:
		return app.ValidatePackBytes(dir, f.Slug, f.Path, data, stack)
	case KindRegistry:
		reg, err := config.ParseRegistryBytes(f.Path, data)
		if err != nil {
			return bad(err)
		}
		var out []app.Finding
		for _, slug := range reg.Packs {
			if _, err := config.PackFile(slug, dir); err != nil {
				out = append(out, app.Finding{Level: app.LevelError, Text: err.Error()})
			}
		}
		if app.Problems(out) > 0 {
			return out
		}
		return ok(fmt.Sprintf("registry: %d packs, each with a config", len(reg.Packs)))
	case KindManifest:
		m, err := manifestpack.ParseManifestBytes(f.Path, data)
		if err != nil {
			return bad(err)
		}
		return ok(fmt.Sprintf("set manifest: %d sets", len(m.Sets)))
	case KindGlobal:
		v, err := settings.ParseGlobal(f.Path, data)
		if err != nil {
			return bad(err)
		}
		return ok(fmt.Sprintf("global config: %d settings", len(v)))
	}
	return bad(fmt.Errorf("%s: no validator for kind %q", f.Name, f.Kind))
}

// WriteConfig implements Engine. It refuses a stale base revision and an
// invalid document, and otherwise replaces the file atomically. Saving the
// global config also reloads its layer, so the next run sees it.
func (e *AppEngine) WriteConfig(name string, data []byte, base string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	doc, err := e.ReadConfig(name)
	if err != nil {
		return err
	}
	if doc.ReadOnly {
		return errors.New(doc.Why)
	}
	if base != doc.Rev {
		return ErrConflict
	}
	findings := validateDoc(doc.ConfigFile, data, e.configDir(), e.stack())
	if app.Problems(findings) > 0 {
		return &ValidationError{Findings: findings}
	}
	switch doc.Kind {
	case KindPack:
		err = config.WritePackFileAtomic(doc.Path, data)
	case KindGlobal:
		if err = os.MkdirAll(filepath.Dir(doc.Path), 0o755); err == nil {
			err = docfile.WriteAtomic(doc.Path, data)
		}
	default:
		err = docfile.WriteAtomic(doc.Path, data)
	}
	if err != nil {
		return err
	}
	if doc.Kind == KindGlobal {
		values, err := settings.ParseGlobal(doc.Path, data)
		if err != nil {
			return err
		}
		e.mu.Lock()
		e.Stack.Global = values
		e.GlobalPath = doc.Path
		e.mu.Unlock()
	}
	return nil
}
