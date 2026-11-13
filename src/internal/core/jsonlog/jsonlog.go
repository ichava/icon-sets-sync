// Package jsonlog appends records to a JSON Lines file that rotates by size:
// the API audit log and the webhook delivery log. Files are mode 0600 in a
// 0700 directory, because both name tokens and endpoints.
package jsonlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultMaxBytes is the size at which a log rotates.
const DefaultMaxBytes = 5 << 20

// Log is one rotating JSON Lines file. Path.1 is the previous file; Keep
// older ones are kept as Path.2 and so on.
type Log struct {
	Path string
	// MaxBytes rotates the file before a write would take it past this;
	// zero is DefaultMaxBytes.
	MaxBytes int64
	// Keep is how many rotated files to keep; zero keeps one.
	Keep int

	mu sync.Mutex
}

// Append writes v as one line.
func (l *Log) Append(v any) error {
	if l == nil || l.Path == "" {
		return nil
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	max := l.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	if info, err := os.Stat(l.Path); err == nil && info.Size()+int64(len(line)) > max {
		if err := l.rotate(); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(l.Path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (l *Log) rotate() error {
	keep := l.Keep
	if keep < 1 {
		keep = 1
	}
	for i := keep; i >= 1; i-- {
		from := l.Path
		if i > 1 {
			from = fmt.Sprintf("%s.%d", l.Path, i-1)
		}
		to := fmt.Sprintf("%s.%d", l.Path, i)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Tail returns up to n of the newest records of the current file, oldest
// first, each as raw JSON. A missing file is no records.
func (l *Log) Tail(n int) ([]json.RawMessage, error) {
	if l == nil || l.Path == "" {
		return nil, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []json.RawMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if !json.Valid(line) {
			continue
		}
		out = append(out, line)
		if n > 0 && len(out) > n {
			out = out[1:]
		}
	}
	return out, sc.Err()
}
