package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
)

// A private config directory holds recipes that must not ship in this
// repository: the pack configs, set manifests and registry of packs that are
// not public. It is read at run time and merged with the public config
// directory, so a private pack is synced by the same binary and the same
// recipes as a public one without its configuration ever being committed here.
//
// It is laid out exactly like the public directory -- an optional packs.json
// (or .yaml) naming its packs, one <slug>.json per pack, and any file a recipe
// reads beside its pack config, such as a <slug>.sets.json set manifest.
//
// The merge rules are deliberately small:
//
//   - The registry is the public one followed by every private slug it does
//     not already list, in the private registry's order.
//   - A file present in both directories is read from the private one, so a
//     private deployment can override a public pack without forking it.
//   - A private directory that is set but missing is an error, never an empty
//     overlay: a mount that failed must not read as "no private packs".
//
// It is set once, by the CLI after it resolves the private_config_dir setting
// (--private-config-dir, ICHAVA_PRIVATE_CONFIG_DIR), and read by every loader
// in this package. A process-wide value rather than another parameter, because
// every caller that names a config directory -- the CLI, the TUI, the web UI's
// editor and the recipes -- must see the same overlay, and threading a second
// directory through each of them is how one of them would end up without it.
var (
	privateMu  sync.RWMutex
	privateDir string
)

// SetPrivateConfigDir sets the private config directory and returns a func
// restoring the previous one, for tests. An empty dir means none.
func SetPrivateConfigDir(dir string) (restore func()) {
	privateMu.Lock()
	prev := privateDir
	privateDir = dir
	privateMu.Unlock()
	return func() {
		privateMu.Lock()
		privateDir = prev
		privateMu.Unlock()
	}
}

// PrivateConfigDir is the private config directory in use, or "".
func PrivateConfigDir() string {
	privateMu.RLock()
	defer privateMu.RUnlock()
	return privateDir
}

// CheckPrivateConfigDir reports a private directory that is set but cannot be
// read as a directory. Nil when none is set.
func CheckPrivateConfigDir() error {
	dir := PrivateConfigDir()
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("private config dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("private config dir %s: not a directory", dir)
	}
	return nil
}

// searchDirs is where a config file is looked for, in order: the private
// directory first, then the public one.
func searchDirs(configDir string) []string {
	private := PrivateConfigDir()
	if private == "" || sameDir(private, configDir) {
		return []string{configDir}
	}
	return []string{private, configDir}
}

func sameDir(a, b string) bool {
	ca, errA := filepath.Abs(a)
	cb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && ca == cb
}

// findDoc is docfile.Find across the search directories.
func findDoc(configDir, base string) (string, error) {
	if err := CheckPrivateConfigDir(); err != nil {
		return "", err
	}
	for _, dir := range searchDirs(configDir) {
		path, err := docfile.Find(dir, base)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", os.ErrNotExist
}

// ResolveFile returns where a file a recipe reads beside its pack config is:
// the private directory's copy when there is one, else the public path --
// returned even when it does not exist, so the caller's error names it.
func ResolveFile(configDir, name string) string {
	dirs := searchDirs(configDir)
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(dirs[len(dirs)-1], name)
}

// mergePrivateRegistry appends the private registry's packs to registry.
func mergePrivateRegistry(registry *PacksRegistry) error {
	if err := CheckPrivateConfigDir(); err != nil {
		return err
	}
	dir := PrivateConfigDir()
	if dir == "" {
		return nil
	}
	file, err := docfile.Find(dir, "packs")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	private, err := ParseRegistryBytes(file, data)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(registry.Packs))
	for _, slug := range registry.Packs {
		seen[slug] = true
	}
	for _, slug := range private.Packs {
		if !seen[slug] {
			registry.Packs = append(registry.Packs, slug)
		}
	}
	return nil
}
