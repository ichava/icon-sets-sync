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

// GlobalConfigPath finds the optional global config file.
//
// $XDG_CONFIG_HOME/icon-sets-sync/config.{json,yaml,yml}, falling back to
// ~/.config when XDG_CONFIG_HOME is unset -- on macOS too, per the house rule
// that config lives where a person expects to find it. Returns "" with no
// error when there is none: the file is optional, and a fresh machine or a CI
// container has none.
func GlobalConfigPath(lookup func(string) (string, bool)) (string, error) {
	if explicit, ok := lookup(GlobalConfigEnv); ok && explicit != "" {
		return explicit, nil
	}

	base, _ := lookup("XDG_CONFIG_HOME")
	if base == "" {
		home, ok := lookup("HOME")
		if !ok || home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", nil //nolint:nilerr // no home, so no global config to find
			}
			home = h
		}
		base = filepath.Join(home, ".config")
	}

	path, err := docfile.Find(filepath.Join(base, AppDir), "config")
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return path, err
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
	return values, nil
}

type pathError struct {
	path string
	err  error
}

func (e *pathError) Error() string { return e.path + ": " + e.err.Error() }
func (e *pathError) Unwrap() error { return e.err }

func wrapPath(path string, err error) error { return &pathError{path: path, err: err} }
