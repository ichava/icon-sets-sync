package events

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEveryTypeHasASchema(t *testing.T) {
	if len(Types()) < 10 {
		t.Fatalf("%d types: the list lost some", len(Types()))
	}
	for _, typ := range Types() {
		if _, ok := Schema(typ); !ok {
			t.Errorf("%s has no schema in schemas/", typ)
		}
	}
	for _, name := range SchemaFiles() {
		if !Known(name) {
			t.Errorf("schemas/%s.json is for no published type", name)
		}
	}
	if _, err := schemas(); err != nil {
		t.Fatal(err)
	}
}

// typeConstants maps each type's value to its Go name, read from this
// package's source.
func typeConstants(t *testing.T) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "events.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Values) != len(spec.Names) {
			return true
		}
		for i, v := range spec.Values {
			lit, ok := v.(*ast.BasicLit)
			if !ok {
				continue
			}
			s, err := strconv.Unquote(lit.Value)
			if err == nil && Known(s) {
				out[s] = spec.Names[i].Name
			}
		}
		return true
	})
	return out
}

// Every type has a publisher: some production file outside this package
// names its constant. Proven red by deleting the WebhookDisabled emit in
// internal/webhooks: webhook.disabled was reported.
func TestEveryTypeHasAPublisher(t *testing.T) {
	names := typeConstants(t)
	if len(names) != len(Types()) {
		t.Fatalf("found constants for %d of %d types", len(names), len(Types()))
	}
	root := filepath.Join("..")
	var sources []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "events") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sources = append(sources, string(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) < 100 {
		t.Fatalf("read %d Go files under internal/: the walk lost them", len(sources))
	}
	for typ, name := range names {
		found := slices.ContainsFunc(sources, func(src string) bool { return strings.Contains(src, "events."+name) })
		if !found {
			t.Errorf("%s (events.%s) has a schema but no publisher", typ, name)
		}
	}
}

func TestNewIDIsAULID(t *testing.T) {
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	a, b := NewID(at), NewID(at.Add(time.Millisecond))
	if len(a) != 26 || strings.Trim(a, crockford) != "" {
		t.Fatalf("%q is not a ULID", a)
	}
	if a[:10] >= b[:10] {
		t.Fatalf("%s then %s: not sortable by time", a, b)
	}
	if a == NewID(at) {
		t.Fatal("two IDs in one millisecond collided")
	}
	if a[0] > '7' {
		t.Fatalf("%s: the first digit carries 3 bits", a)
	}
}

func TestEnvelopeIsCloudEvents(t *testing.T) {
	e, err := New("icon-sets-sync://host", WebhookTest, time.Now(), map[string]any{"name": "ci", "message": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(e); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(e)
	var doc map[string]any
	_ = json.Unmarshal(data, &doc)
	for _, k := range []string{"specversion", "id", "source", "type", "time", "datacontenttype", "data"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("envelope lacks %s", k)
		}
	}
	bad, _ := New("icon-sets-sync://host", WebhookTest, time.Now(), map[string]any{"name": "ci"})
	if Validate(bad) == nil {
		t.Fatal("data missing a required field validated")
	}
	if _, err := New("x", "nope.never", time.Now(), nil); err == nil {
		t.Fatal("an unknown type was built")
	}
}

func TestBusFiltersAndNeverBlocks(t *testing.T) {
	b := NewBus("icon-sets-sync://host", nil)
	all := b.Subscribe(1)
	only := b.Subscribe(10, WebhookTest)
	for i := 0; i < 3; i++ {
		b.Emit(ServerStopping, map[string]any{"reason": "signal"})
	}
	b.Emit(WebhookTest, map[string]any{"name": "ci", "message": "hi"})
	if all.Dropped() != 3 {
		t.Fatalf("dropped %d; a full subscriber drops, it does not block", all.Dropped())
	}
	e := <-only.C()
	if e.Type != WebhookTest || len(only.C()) != 0 {
		t.Fatalf("filtered subscriber got %s", e.Type)
	}
	all.Close()
	all.Close()
	// Publishing after a close neither panics nor reaches the closed queue,
	// and the queue drains to its end.
	b.Emit(WebhookTest, map[string]any{"name": "ci", "message": "hi"})
	n := 0
	for range all.C() {
		n++
	}
	if n != 1 {
		t.Fatalf("drained %d events, want the 1 buffered", n)
	}
}
