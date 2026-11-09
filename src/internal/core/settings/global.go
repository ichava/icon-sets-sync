package settings

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
)

// AppDir is the directory name under the XDG config home.
const AppDir = "icon-sets-sync"

// GlobalConfigEnv points at a global config file explicitly, overriding the
// XDG lookup. --global-config does the same from the command line.
const GlobalConfigEnv = "ICHAVA_GLOBAL_CONFIG"

// GlobalLocation is where the global config was found, and where it belongs.
type GlobalLocation struct {
	// Path is the file read, or "" when there is none.
	Path string
	// Default is where a global config belongs on this machine:
	// $XDG_CONFIG_HOME/icon-sets-sync/config.json, else
	// ~/.config/icon-sets-sync/config.json -- on every OS, Windows included.
	Default string
	// Layer names what chose Path: ICHAVA_GLOBAL_CONFIG, XDG_CONFIG_HOME,
	// "~/.config" or "legacy location".
	Layer string
	// Legacy is set when Path is at an old location (the OS config directory,
	// %AppData% or ~/Library/Application Support) because the default has no
	// file. It is read, never moved; `config migrate` copies it.
	Legacy bool
}

// LegacyConfigDirs are the old homes of the global config: the OS config
// directory, when it differs from ~/.config. Nil when there is none.
func LegacyConfigDirs() []string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return nil
	}
	return []string{dir}
}

// FindGlobalConfig locates the optional global config. legacyDirs are the old
// config homes searched, after the default, for a file not yet migrated.
//
// The default deliberately overrides the org rule of %AppData% on Windows
// (the owner, 2026-11-09): one documented path on every OS.
func FindGlobalConfig(lookup func(string) (string, bool), legacyDirs []string) (GlobalLocation, error) {
	var loc GlobalLocation
	base, _ := lookup("XDG_CONFIG_HOME")
	layer := "XDG_CONFIG_HOME"
	if base == "" {
		layer = "~/.config"
		home, ok := lookup("HOME")
		if !ok || home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return loc, nil //nolint:nilerr // no home, so no global config to find
			}
			home = h
		}
		base = filepath.Join(home, ".config")
	}
	loc.Default = filepath.Join(base, AppDir, "config.json")

	if explicit, ok := lookup(GlobalConfigEnv); ok && explicit != "" {
		loc.Path, loc.Layer = explicit, GlobalConfigEnv
		return loc, nil
	}

	path, err := docfile.Find(filepath.Join(base, AppDir), "config")
	switch {
	case err == nil:
		loc.Path, loc.Layer = path, layer
		return loc, nil
	case !errors.Is(err, os.ErrNotExist):
		return loc, err
	}
	for _, dir := range legacyDirs {
		if filepath.Clean(dir) == filepath.Clean(base) {
			continue
		}
		path, err := docfile.Find(filepath.Join(dir, AppDir), "config")
		if err == nil {
			loc.Path, loc.Layer, loc.Legacy = path, "legacy location", true
			return loc, nil
		}
	}
	return loc, nil
}

// GlobalConfigPath finds the optional global config file.
//
// It is FindGlobalConfig's Path, legacy locations included. Returns "" with no
// error when there is none: the file is optional, and a fresh machine or a CI
// runner has none.
func GlobalConfigPath(lookup func(string) (string, bool)) (string, error) {
	loc, err := FindGlobalConfig(lookup, LegacyConfigDirs())
	return loc.Path, err
}

// LoadGlobal reads a global config file into the global layer.
func LoadGlobal(path string) (Values, error) {
	if path == "" {
		return Values{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseGlobal(path, data)
}

// ParseGlobal is LoadGlobal on bytes already in hand, so an editor validates
// a global config with the loader that will read it.
func ParseGlobal(path string, data []byte) (Values, error) {
	tree, err := docfile.ParseTree(path, data)
	if err != nil {
		return nil, err
	}
	values, err := FromTree(tree, LayerGlobal)
	if err != nil {
		return nil, wrapPath(path, err)
	}
	ResolvePaths(values, filepath.Dir(path))
	return values, nil
}

type pathError struct {
	path string
	err  error
}

func (e *pathError) Error() string { return e.path + ": " + e.err.Error() }
func (e *pathError) Unwrap() error { return e.err }

func wrapPath(path string, err error) error { return &pathError{path: path, err: err} }
