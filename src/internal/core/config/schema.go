package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// packSchema is a byte-identical copy of config/schema/pack.schema.json at the
// repository root. It is embedded because the binary has to validate a pack
// config wherever it runs -- inside the Docker image the config directory is
// a bind mount the binary does not control -- and a Go module cannot embed a
// file outside its own directory. TestEmbeddedSchemaMatchesTheRepoCopy keeps
// the two from drifting.
//
//go:embed schema/pack.schema.json
var packSchema []byte

// PackSchema returns the embedded schema document.
func PackSchema() []byte { return bytes.Clone(packSchema) }

const packSchemaURL = "urn:ichava:icon-sets-sync:pack.schema.json"

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
)

func schema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(packSchema))
		if err != nil {
			compileErr = fmt.Errorf("embedded pack schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(packSchemaURL, doc); err != nil {
			compileErr = fmt.Errorf("embedded pack schema: %w", err)
			return
		}
		compiled, compileErr = c.Compile(packSchemaURL)
	})
	return compiled, compileErr
}

// ValidatePackDocument checks a pack config, as JSON bytes, against the schema.
//
// The error lists every violation, one per line, each with the JSON pointer of
// the offending value -- "/sinks/0/root" rather than "invalid config".
func ValidatePackDocument(doc []byte) error {
	sch, err := schema()
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return err
	}
	if err := sch.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return &SchemaError{Problems: flatten(ve)}
		}
		return err
	}
	return nil
}

// SchemaError carries every schema violation in a document.
type SchemaError struct {
	Problems []string
}

func (e *SchemaError) Error() string {
	return "does not match config/schema/pack.schema.json:\n  " + strings.Join(e.Problems, "\n  ")
}

// flatten collects the leaf causes, which are the specific failures; the
// inner nodes only say "failed because of the leaves below".
func flatten(ve *jsonschema.ValidationError) []string {
	printer := message.NewPrinter(language.English)
	var out []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "/" + strings.Join(e.InstanceLocation, "/")
			out = append(out, fmt.Sprintf("%s: %s", loc, e.ErrorKind.LocalizedString(printer)))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(out) == 0 {
		out = append(out, ve.Error())
	}
	return out
}
