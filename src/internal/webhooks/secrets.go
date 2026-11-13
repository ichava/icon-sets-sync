package webhooks

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/core/statefile"
)

// SecretsFile is where the tool keeps the secrets it generated, in the state
// directory, mode 0600: never in the global config, which is shareable.
const SecretsFile = "webhook-secrets.json"

// RotationGrace is how long the previous secret keeps signing after a
// rotation, so a receiver can switch with no failed delivery.
const RotationGrace = 24 * time.Hour

// secret is one endpoint's record.
type secret struct {
	Current       string    `json:"current"`
	Created       time.Time `json:"created"`
	Previous      string    `json:"previous,omitempty"`
	PreviousUntil time.Time `json:"previous_until,omitzero"`
}

// Secrets is the secrets file.
type Secrets struct {
	Path string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Lookup reads the environment, for secret_env; nil means os.LookupEnv.
	Lookup func(string) (string, bool)

	mu sync.Mutex
}

// DefaultSecretsPath is webhook-secrets.json in stateDir.
func DefaultSecretsPath(stateDir string) string { return filepath.Join(stateDir, SecretsFile) }

func (s *Secrets) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Secrets) load() (map[string]secret, error) {
	all := map[string]secret{}
	if _, err := statefile.Load(s.Path, &all); err != nil {
		return nil, err
	}
	return all, nil
}

// NewSecret returns a Standard Webhooks secret: whsec_ and the base64 of 32
// random bytes.
func NewSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error()) // rand.Read does not fail on supported platforms
	}
	return SecretPrefix + base64.StdEncoding.EncodeToString(b)
}

// Create makes and stores a secret for name, replacing none: an endpoint
// that has one keeps it (rotate it instead).
func (s *Secrets) Create(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return "", err
	}
	if _, ok := all[name]; ok {
		return "", fmt.Errorf("%s already has a secret; `webhooks rotate %s` replaces it", name, name)
	}
	v := NewSecret()
	all[name] = secret{Current: v, Created: s.now().UTC()}
	return v, statefile.Save(s.Path, all)
}

// Rotate replaces name's secret, keeping the old one valid for
// RotationGrace, and returns the new one.
func (s *Secrets) Rotate(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return "", err
	}
	old, ok := all[name]
	if !ok {
		return "", fmt.Errorf("%s has no generated secret to rotate", name)
	}
	now := s.now()
	v := NewSecret()
	all[name] = secret{Current: v, Created: now.UTC(), Previous: old.Current, PreviousUntil: now.Add(RotationGrace).UTC()}
	return v, statefile.Save(s.Path, all)
}

// Remove deletes name's secret, when the endpoint is removed.
func (s *Secrets) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := all[name]; !ok {
		return nil
	}
	delete(all, name)
	return statefile.Save(s.Path, all)
}

// Signing returns the secrets that sign a delivery to w now: secret_env's
// value when it names one that is set, else the generated secret and, within
// a rotation's grace, the previous one.
func (s *Secrets) Signing(w settings.Webhook) ([]string, error) {
	if w.SecretEnv != "" {
		lookup := s.Lookup
		if lookup == nil {
			lookup = os.LookupEnv
		}
		if v, ok := lookup(w.SecretEnv); ok && v != "" {
			return []string{v}, nil
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return nil, err
	}
	rec, ok := all[w.Name]
	if !ok || rec.Current == "" {
		if w.SecretEnv != "" {
			return nil, fmt.Errorf("%s: %s is not set, and the tool generated no secret for it", w.Name, w.SecretEnv)
		}
		return nil, fmt.Errorf("%s has no secret; `webhooks add` makes one", w.Name)
	}
	out := []string{rec.Current}
	if rec.Previous != "" && s.now().Before(rec.PreviousUntil) {
		out = append(out, rec.Previous)
	}
	return out, nil
}

// Has reports whether name has a generated secret.
func (s *Secrets) Has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return false
	}
	_, ok := all[name]
	return ok
}
