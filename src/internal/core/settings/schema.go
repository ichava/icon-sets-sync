package settings

import (
	"encoding/json"
	"strings"
)

// GlobalSchemaURL is the $schema a global config names, so an editor offers
// completion and checks it. It is the file GlobalSchema generates, as
// published on the default branch.
const GlobalSchemaURL = "https://raw.githubusercontent.com/ichava/icon-sets-sync/main/config/schema/global.schema.json"

// GlobalSchema is the JSON Schema of the global config, generated from Keys so
// the two cannot drift: config/schema/global.schema.json is its output, and a
// test fails when the file is stale.
func GlobalSchema() ([]byte, error) {
	root := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  GlobalSchemaURL,
		"title":                "icon-sets-sync global config",
		"description":          "~/.config/icon-sets-sync/config.json: settings for every run on this machine. Generated from the settings registry; do not edit.",
		"type":                 "object",
		"additionalProperties": false,
		"patternProperties":    map[string]any{"^_": map[string]any{}},
	}
	props := map[string]any{
		"$schema": map[string]any{"type": "string", "description": "Editor hint. Ignored by the loader."},
	}
	for _, k := range Keys {
		parts := strings.Split(k.Name, ".")
		node := props
		for _, p := range parts[:len(parts)-1] {
			child, ok := node[p].(map[string]any)
			if !ok {
				child = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}
				node[p] = child
			}
			node = child["properties"].(map[string]any)
		}
		leaf := keySchema(k)
		node[parts[len(parts)-1]] = leaf
		for _, a := range k.Aliases {
			alias := keySchema(k)
			alias["deprecated"] = true
			alias["description"] = "Alias of " + k.Name + "; `config fix` renames it."
			node[a] = alias
		}
	}
	root["properties"] = props
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func keySchema(k Key) map[string]any {
	s := map[string]any{"description": k.Help + " (environment: " + k.Env + ")"}
	if k.Default != nil {
		s["default"] = k.Default
	}
	switch k.Kind {
	case Bool:
		s["type"] = "boolean"
	case Int:
		s["type"] = "integer"
		s["minimum"] = k.Min
		if k.Max > 0 {
			s["maximum"] = k.Max
		}
	case List:
		s["anyOf"] = []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}
	default:
		s["type"] = "string"
		if len(k.Enum) > 0 {
			s["enum"] = k.Enum
		}
		if k.Pattern != nil {
			s["pattern"] = k.Pattern.String()
		}
	}
	return s
}
