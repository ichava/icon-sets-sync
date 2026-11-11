package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// The config documents FixConfig knows. The web editor's kinds use the same
// strings.
const (
	KindGlobalConfig = "global"
	KindPackConfig   = "pack"
)

// PackSchemaRef is the $schema a pack config in config/ names.
const PackSchemaRef = "./schema/pack.schema.json"

// Fixed is FixConfig's result: the repaired text and what changed, in words.
type Fixed struct {
	Data    []byte
	Changes []string
}

// FixConfig applies the safe repairs to a config document: it formats JSON
// with two-space indentation, renames deprecated keys (packages_dir becomes
// packages_dirs) and adds a $schema an editor can complete against. Key order
// is kept. Broken JSON is an error -- a guess at what was meant is not a safe
// repair -- and YAML is returned unchanged, because re-encoding it would drop
// its comments. It never writes; the caller shows the diff and asks.
func FixConfig(kind, path string, data []byte) (Fixed, error) {
	if docfile.IsYAML(path) {
		return Fixed{Data: data}, nil
	}
	obj, err := parseObject(data)
	if err != nil {
		return Fixed{}, fmt.Errorf("%s is not valid JSON, so there is nothing safe to fix: %w", path, err)
	}
	var changes []string

	if kind == KindGlobalConfig {
		for _, k := range settings.Keys {
			for _, alias := range k.Aliases {
				if i := obj.index(alias); i >= 0 {
					if obj.index(k.Name) >= 0 {
						obj.remove(i)
						changes = append(changes, fmt.Sprintf("dropped %q: %q is set too, and wins", alias, k.Name))
					} else {
						obj[i].key = k.Name
						changes = append(changes, fmt.Sprintf("renamed the deprecated %q to %q", alias, k.Name))
					}
				}
			}
		}
	}

	schema := ""
	switch kind {
	case KindGlobalConfig:
		schema = settings.GlobalSchemaURL
	case KindPackConfig:
		schema = PackSchemaRef
	}
	if schema != "" && obj.index("$schema") < 0 {
		raw, _ := json.Marshal(schema)
		obj = append(object{{key: "$schema", value: raw}}, obj...)
		changes = append(changes, "added \"$schema\": "+schema)
	}

	out, err := obj.marshal()
	if err != nil {
		return Fixed{}, err
	}
	if !bytes.Equal(out, data) && len(changes) == 0 {
		changes = append(changes, "reformatted with two-space indentation")
	} else if !bytes.Equal(out, data) && !sameFormatting(data) {
		changes = append([]string{"reformatted with two-space indentation"}, changes...)
	}
	return Fixed{Data: out, Changes: changes}, nil
}

// sameFormatting reports whether data is already as marshal would write it.
func sameFormatting(data []byte) bool {
	obj, err := parseObject(data)
	if err != nil {
		return false
	}
	out, err := obj.marshal()
	return err == nil && bytes.Equal(out, data)
}

// object is a JSON object's members in their written order.
type object []member

type member struct {
	key   string
	value json.RawMessage
}

func (o object) index(key string) int {
	for i, m := range o {
		if m.key == key {
			return i
		}
	}
	return -1
}

func (o *object) remove(i int) { *o = append((*o)[:i], (*o)[i+1:]...) }

func parseObject(data []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("the document is not a JSON object")
	}
	var obj object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		obj = append(obj, member{key, raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("there is more after the closing brace")
	}
	return obj, nil
}

func (o object) marshal() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{")
	for i, m := range o {
		if i > 0 {
			b.WriteString(",")
		}
		k, _ := json.Marshal(m.key)
		b.Write(k)
		b.WriteString(":")
		b.Write(m.value)
	}
	b.WriteString("}")
	var out bytes.Buffer
	if err := json.Indent(&out, b.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

var quotedName = regexp.MustCompile(`"([^"]+)"`)

// Locate finds the line and column (1-based) an error is about, or 0, 0.
// JSON syntax and type errors carry a byte offset; an error naming a key
// ("unknown setting \"dry_rnu\"") is placed at that key's first occurrence,
// by the last segment of a dotted name.
func Locate(data []byte, err error) (line, col int) {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		return offsetToLineCol(data, syn.Offset)
	case errors.As(err, &typ):
		return offsetToLineCol(data, typ.Offset)
	}
	for _, m := range quotedName.FindAllStringSubmatch(err.Error(), -1) {
		name := m[1]
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if at := bytes.Index(data, []byte(`"`+name+`"`)); at >= 0 {
			return offsetToLineCol(data, int64(at)+1)
		}
	}
	return 0, 0
}

func offsetToLineCol(data []byte, offset int64) (int, int) {
	if offset < 1 {
		offset = 1
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	line, col := 1, 1
	for _, c := range data[:offset-1] {
		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
