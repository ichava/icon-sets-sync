// Package events is the in-process event bus every integration reads: the
// API's event stream, the webhook dispatcher, and the per-job stream the web
// UI already had. Each event is a CloudEvents 1.0 envelope in the JSON format
// (https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/formats/json-format.md),
// so a receiver can use the tooling that format already has, and each type's
// data has a JSON Schema embedded in the binary.
//
// The package imports nothing else from this module: publishers depend on it,
// never the other way round.
package events

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// SpecVersion is the CloudEvents version of every envelope.
const SpecVersion = "1.0"

// The v1 event types. A type is published by exactly the code named beside
// it; TestEveryTypeHasAPublisher fails on a type nothing publishes, and
// TestEveryTypeHasASchema on one with no schema.
const (
	// JobStarted: a sync, sync-all, recipe or audit run began (web UI or API).
	JobStarted = "job.started"
	// JobProgress: a pipeline stage started, finished or failed. Stream only:
	// it is too frequent for a webhook, and the dispatcher refuses it.
	JobProgress = "job.progress"
	// JobFinished: a run ended, with its status.
	JobFinished = "job.finished"
	// PackSynced: one pack of a run refreshed from upstream.
	PackSynced = "pack.synced"
	// PackBehindUpstream: an upstream check found a pack behind.
	PackBehindUpstream = "pack.behind_upstream"
	// UpstreamChecked: an upstream check of every pack finished.
	UpstreamChecked = "upstream.checked"
	// ConfigChanged: a config file or setting was saved. It names the file
	// and the key, never the value.
	ConfigChanged = "config.changed"
	// ServerStarted and ServerStopping: the serve lifecycle.
	ServerStarted  = "server.started"
	ServerStopping = "server.stopping"
	// WebhookDisabled: an endpoint was turned off after too many
	// consecutive failed deliveries.
	WebhookDisabled = "webhook.disabled"
	// WebhookTest: a test delivery asked for by `webhooks test` or the API.
	WebhookTest = "webhook.test"
)

// types lists every published type, in display order.
var types = []string{
	JobStarted, JobProgress, JobFinished, PackSynced, PackBehindUpstream, UpstreamChecked,
	ConfigChanged, ServerStarted, ServerStopping, WebhookDisabled, WebhookTest,
}

// StreamOnly are the types a webhook cannot subscribe to.
var streamOnly = map[string]bool{JobProgress: true}

// Types returns every event type, in display order.
func Types() []string { return append([]string(nil), types...) }

// Known reports whether t is an event type.
func Known(t string) bool {
	for _, k := range types {
		if k == t {
			return true
		}
	}
	return false
}

// StreamOnly reports whether t is sent on the event stream only, never to a
// webhook.
func StreamOnly(t string) bool { return streamOnly[t] }

// Event is one CloudEvents 1.0 envelope.
type Event struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	Data            json.RawMessage `json:"data"`
}

// Source is this machine's event source, icon-sets-sync://<hostname>.
func Source() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	return "icon-sets-sync://" + host
}

// New builds an envelope. data is marshalled as JSON.
func New(source, typ string, now time.Time, data any) (Event, error) {
	if !Known(typ) {
		return Event{}, fmt.Errorf("events: unknown type %q", typ)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, fmt.Errorf("events: %s data: %w", typ, err)
	}
	return Event{
		SpecVersion: SpecVersion, ID: NewID(now), Source: source, Type: typ,
		Time: now.UTC(), DataContentType: "application/json", Data: raw,
	}, nil
}

// crockford is the ULID alphabet.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns a ULID (https://github.com/ulid/spec): 48 bits of
// milliseconds and 80 random bits, 26 characters, sortable by time.
func NewID(now time.Time) string {
	var b [16]byte
	ms := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(b[6:]); err != nil {
		panic("crypto/rand: " + err.Error()) // rand.Read does not fail on supported platforms
	}
	// 128 bits as 26 base-32 digits, least significant last; the first
	// digit carries the top 3 bits.
	hi, lo := binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:])
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out)
}

//go:embed schemas/*.json
var schemaFiles embed.FS

// Schema returns the JSON Schema of a type's data.
func Schema(typ string) ([]byte, bool) {
	if !Known(typ) {
		return nil, false
	}
	data, err := schemaFiles.ReadFile("schemas/" + typ + ".json")
	if err != nil {
		return nil, false
	}
	return data, true
}

// SchemaFiles lists the embedded schema files' types, sorted.
func SchemaFiles() []string {
	entries, _ := schemaFiles.ReadDir("schemas")
	var out []string
	for _, e := range entries {
		name := e.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			out = append(out, name[:len(name)-5])
		}
	}
	sort.Strings(out)
	return out
}

var (
	compileOnce sync.Once
	compiled    map[string]*jsonschema.Schema
	compileErr  error
)

func schemas() (map[string]*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		compiled = map[string]*jsonschema.Schema{}
		for _, t := range types {
			data, ok := Schema(t)
			if !ok {
				compileErr = fmt.Errorf("events: no schema for %s", t)
				return
			}
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
			if err != nil {
				compileErr = fmt.Errorf("events: schema %s: %w", t, err)
				return
			}
			url := "urn:ichava:icon-sets-sync:events:" + t
			if err := c.AddResource(url, doc); err != nil {
				compileErr = fmt.Errorf("events: schema %s: %w", t, err)
				return
			}
			s, err := c.Compile(url)
			if err != nil {
				compileErr = fmt.Errorf("events: schema %s: %w", t, err)
				return
			}
			compiled[t] = s
		}
	})
	return compiled, compileErr
}

// Validate checks an event's envelope and its data against the type's schema.
func Validate(e Event) error {
	if e.SpecVersion != SpecVersion || e.ID == "" || e.Source == "" || e.Time.IsZero() {
		return errors.New("events: incomplete envelope")
	}
	all, err := schemas()
	if err != nil {
		return err
	}
	s, ok := all[e.Type]
	if !ok {
		return fmt.Errorf("events: unknown type %q", e.Type)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(e.Data))
	if err != nil {
		return fmt.Errorf("events: %s data: %w", e.Type, err)
	}
	if err := s.Validate(inst); err != nil {
		return fmt.Errorf("events: %s data: %w", e.Type, err)
	}
	return nil
}
