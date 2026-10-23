package jobs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultKeep is how many finished jobs the history keeps. It is a ceiling: a
// test pins it, so raising it is a decision rather than drift.
const DefaultKeep = 50

// Store persists finished jobs as JSON Lines, one file per job: the first line
// is the job's Info, every later line one Event. It only ever touches files
// in its own directory whose names it could have written.
type Store struct {
	Dir  string
	Keep int
}

var fileName = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{6}\.jsonl$`)

// ValidID reports whether id has the shape newID produces, so a request
// cannot name a path outside the store.
func ValidID(id string) bool { return fileName.MatchString(id + ".jsonl") }

func (s *Store) path(id string) string { return filepath.Join(s.Dir, id+".jsonl") }

// Save writes a finished job and prunes the history to Keep.
func (s *Store) Save(info Info, events []Event) error {
	if !ValidID(info.ID) {
		return fmt.Errorf("invalid job id %q", info.ID)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".job-*.tmp")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	err = enc.Encode(info)
	for _, e := range events {
		if err != nil {
			break
		}
		err = enc.Encode(e)
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path(info.ID))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return s.prune()
}

func (s *Store) keep() int {
	if s.Keep < 1 {
		return DefaultKeep
	}
	return s.Keep
}

// names returns the job files, oldest first.
func (s *Store) names() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && fileName.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) prune() error {
	names, err := s.names()
	if err != nil {
		return err
	}
	for len(names) > s.keep() {
		if err := os.Remove(filepath.Join(s.Dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

// List reads every stored job's Info, newest first.
func (s *Store) List() ([]Info, error) {
	names, err := s.names()
	if err != nil {
		return nil, err
	}
	var out []Info
	for i := len(names) - 1; i >= 0; i-- {
		info, _, err := s.load(strings.TrimSuffix(names[i], ".jsonl"), true)
		if err == nil {
			out = append(out, info)
		}
	}
	return out, nil
}

// Load reads one stored job and its events.
func (s *Store) Load(id string) (Info, []Event, error) { return s.load(id, false) }

// Open returns the raw JSON Lines file, for download.
func (s *Store) Open(id string) (io.ReadCloser, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	f, err := os.Open(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *Store) load(id string, headerOnly bool) (Info, []Event, error) {
	var info Info
	f, err := s.Open(id)
	if err != nil {
		return info, nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	if !sc.Scan() {
		return info, nil, fmt.Errorf("%s: empty", id)
	}
	if err := json.Unmarshal(sc.Bytes(), &info); err != nil {
		return info, nil, fmt.Errorf("%s: %w", id, err)
	}
	if headerOnly {
		return info, nil, nil
	}
	var events []Event
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return info, events, fmt.Errorf("%s: %w", id, err)
		}
		events = append(events, e)
	}
	return info, events, sc.Err()
}
