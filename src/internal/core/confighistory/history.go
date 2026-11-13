// Package confighistory keeps a timestamped copy of a config file before every
// save, so a bad edit -- from the web UI's editor or `config edit` -- can be
// seen as a diff and undone with a restore.
package confighistory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DefaultKeep is how many copies of one file are kept when the history_keep
// setting is not set.
const DefaultKeep = 50

// Store is one history directory.
type Store struct {
	// Dir holds one subdirectory per config file.
	Dir string
	// Keep is how many copies of each file are kept; the oldest go first.
	// Zero keeps none: history is off.
	Keep int
	// Now is time.Now, swapped by tests.
	Now func() time.Time
}

// Entry is one saved copy.
type Entry struct {
	// ID names the copy within its file's history: a UTC timestamp.
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Size int64     `json:"size"`
}

// DefaultDir is $XDG_STATE_HOME/icon-sets-sync/history, else
// ~/.local/state/icon-sets-sync/history, on every OS -- the house layout for
// mutable state.
func DefaultDir(lookup func(string) (string, bool)) string {
	if v, ok := lookup("XDG_STATE_HOME"); ok && v != "" {
		return filepath.Join(v, "icon-sets-sync", "history")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "icon-sets-sync", "history")
}

const idLayout = "20060102T150405.000000000Z"

var validID = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z$`)

// key is the subdirectory for one file: its base name, for a person browsing
// the directory, and a hash of its absolute path, so two files with one name
// (a pack config in two checkouts) never share a history.
func key(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha256.Sum256([]byte(abs))
	base := regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(filepath.Base(abs), "_")
	return base + "-" + hex.EncodeToString(sum[:])[:12]
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Save records data as a copy of path, then drops copies beyond Keep. With
// Keep zero it records nothing and returns an empty Entry.
func (s Store) Save(path string, data []byte) (Entry, error) {
	if s.Keep <= 0 || s.Dir == "" {
		return Entry{}, nil
	}
	dir := filepath.Join(s.Dir, key(path))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Entry{}, err
	}
	t := s.now().UTC()
	e := Entry{ID: t.Format(idLayout), Time: t, Size: int64(len(data))}
	if err := os.WriteFile(filepath.Join(dir, e.ID+filepath.Ext(path)), data, 0o600); err != nil {
		return Entry{}, err
	}
	// Record where the copies came from, for a person reading the directory.
	_ = os.WriteFile(filepath.Join(dir, "SOURCE"), []byte(path+"\n"), 0o600)
	return e, s.prune(dir)
}

func (s Store) prune(dir string) error {
	entries, err := s.entries(dir)
	if err != nil {
		return err
	}
	for i := s.Keep; i < len(entries); i++ {
		if err := os.Remove(entries[i].file); err != nil {
			return err
		}
	}
	return nil
}

type stored struct {
	Entry
	file string
}

func (s Store) entries(dir string) ([]stored, error) {
	list, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []stored
	for _, d := range list {
		id := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		if d.IsDir() || !validID.MatchString(id) {
			continue
		}
		t, err := time.Parse(idLayout, id)
		if err != nil {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		out = append(out, stored{Entry{ID: id, Time: t, Size: info.Size()}, filepath.Join(dir, d.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// List returns path's copies, newest first.
func (s Store) List(path string) ([]Entry, error) {
	if s.Dir == "" {
		return nil, nil
	}
	st, err := s.entries(filepath.Join(s.Dir, key(path)))
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(st))
	for i, e := range st {
		out[i] = e.Entry
	}
	return out, nil
}

// Read returns one copy of path.
func (s Store) Read(path, id string) ([]byte, error) {
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("history: %q is not a history id", id)
	}
	st, err := s.entries(filepath.Join(s.Dir, key(path)))
	if err != nil {
		return nil, err
	}
	for _, e := range st {
		if e.ID == id {
			return os.ReadFile(e.file)
		}
	}
	return nil, fmt.Errorf("history: no copy %s of %s: %w", id, path, os.ErrNotExist)
}
