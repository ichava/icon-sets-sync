// Package docfile reads the JSON and YAML documents the tool is configured by.
//
// Both formats decode to the same shape -- JSON's value model -- so everything
// downstream (schema validation, strict struct decoding, the settings layers)
// handles one representation and cannot behave differently depending on which
// syntax a maintainer preferred.
package docfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Extensions are tried in this order. JSON first, because every shipped config
// is JSON and a lookup should find it without a second stat.
var Extensions = []string{".json", ".yaml", ".yml"}

// IsYAML reports whether a path names a YAML document.
func IsYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// Find returns dir/base with whichever supported extension exists.
//
// Two at once is an error, not a preference order: a tabler.json beside a
// tabler.yaml means someone converted the file and forgot to delete the
// original, and silently picking one runs a config nobody is editing.
// Returns os.ErrNotExist (wrapped) when none exists.
func Find(dir, base string) (string, error) {
	var found []string
	for _, ext := range Extensions {
		candidate := filepath.Join(dir, base+ext)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			found = append(found, candidate)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("%w: %s.{json,yaml,yml} in %s", os.ErrNotExist, base, dir)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("ambiguous config: %s all exist; keep one", strings.Join(found, ", "))
	}
}

// ToJSON converts a document to canonical JSON bytes. JSON input is returned
// as-is after a validity check; YAML is decoded and re-encoded.
func ToJSON(path string, data []byte) ([]byte, error) {
	if !IsYAML(path) {
		if !json.Valid(data) {
			// Decode once more for the error position json.Valid does not give.
			var v any
			if err := json.Unmarshal(data, &v); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			return nil, fmt.Errorf("%s: not valid JSON", path)
		}
		return data, nil
	}

	var v any
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: empty document", path)
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	normalised, err := normalise(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out, err := json.Marshal(normalised)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// normalise makes a YAML value JSON-encodable. yaml.v3 decodes a mapping with
// non-string keys to map[any]any, which encoding/json refuses; a config key
// that is not a string is a mistake, so it is reported rather than stringified.
func normalise(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			n, err := normalise(child)
			if err != nil {
				return nil, err
			}
			x[k] = n
		}
		return x, nil
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("mapping key %v is not a string", k)
			}
			n, err := normalise(child)
			if err != nil {
				return nil, err
			}
			out[ks] = n
		}
		return out, nil
	case []any:
		for i, child := range x {
			n, err := normalise(child)
			if err != nil {
				return nil, err
			}
			x[i] = n
		}
		return x, nil
	default:
		return v, nil
	}
}

// ReadTree reads a document into a generic map. Numbers decode as float64,
// exactly as encoding/json produces them, whichever syntax the file used.
func ReadTree(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	canonical, err := ToJSON(path, data)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := json.Unmarshal(canonical, &tree); err != nil {
		return nil, fmt.Errorf("%s: want an object at the top level: %w", path, err)
	}
	if tree == nil {
		return nil, fmt.Errorf("%s: want an object at the top level", path)
	}
	return tree, nil
}
