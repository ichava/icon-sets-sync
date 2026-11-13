// Package apitoken issues and checks the REST API's bearer tokens.
//
// A token is shown once, when it is created. Only its SHA-256 is stored, in
// api-tokens.json in the state directory (mode 0600), never in the global
// config, which is meant to be shareable. A presented token is hashed and
// compared with every stored hash in constant time; a match that has expired
// or been revoked is refused, and each accepted use is recorded.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/statefile"
)

// FileName is the token store in the state directory.
const FileName = "api-tokens.json"

// Prefix starts every token, so a scanner (and a person) can tell what it is.
const Prefix = "iss_"

// Scope is what a token may do.
type Scope string

// The scopes. Admin grants every other scope as well.
const (
	ScopeRead        Scope = "read"
	ScopeSync        Scope = "sync"
	ScopeConfigWrite Scope = "config:write"
	ScopeAdmin       Scope = "admin"
)

// Scopes lists every scope, in order.
func Scopes() []Scope { return []Scope{ScopeRead, ScopeSync, ScopeConfigWrite, ScopeAdmin} }

// ParseScopes reads a comma-separated scope list.
func ParseScopes(s string) ([]Scope, error) {
	var out []Scope
	seen := map[Scope]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sc := Scope(part)
		known := false
		for _, k := range Scopes() {
			if k == sc {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown scope %q (known: read, sync, config:write, admin)", part)
		}
		if !seen[sc] {
			seen[sc] = true
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("name at least one scope: read, sync, config:write or admin")
	}
	return out, nil
}

// Token is one stored token: everything but the secret.
type Token struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Scopes   []Scope   `json:"scopes"`
	Hash     string    `json:"sha256"`
	Created  time.Time `json:"created"`
	Expires  time.Time `json:"expires,omitzero"`
	LastUsed time.Time `json:"last_used,omitzero"`
	Revoked  time.Time `json:"revoked,omitzero"`
}

// Has reports whether the token carries scope (admin carries every scope).
func (t Token) Has(scope Scope) bool {
	for _, s := range t.Scopes {
		if s == scope || s == ScopeAdmin {
			return true
		}
	}
	return false
}

// State is how a token stands at a moment.
func (t Token) State(now time.Time) string {
	switch {
	case !t.Revoked.IsZero():
		return "revoked"
	case !t.Expires.IsZero() && !now.Before(t.Expires):
		return "expired"
	}
	return "active"
}

// Errors Authenticate returns. Each is a refusal; only the API's message
// differs.
var (
	ErrUnknown = errors.New("not a valid API token")
	ErrExpired = errors.New("this API token has expired")
	ErrRevoked = errors.New("this API token was revoked")
)

type file struct {
	Tokens []Token `json:"tokens"`
}

// Store is the token file.
type Store struct {
	Path string
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	mu      sync.Mutex
	cached  []Token
	modTime time.Time
	size    int64
	// written is when each token's last use was last persisted, so a busy
	// script does not rewrite the file on every request.
	written map[string]time.Time
}

// DefaultPath is api-tokens.json in stateDir.
func DefaultPath(stateDir string) string { return filepath.Join(stateDir, FileName) }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// load reads the file when it changed since the last read, so a token
// created or revoked by the CLI takes effect in a running server.
func (s *Store) load() ([]Token, error) {
	info, err := os.Stat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		s.cached, s.modTime, s.size = nil, time.Time{}, 0
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if s.cached != nil && info.ModTime().Equal(s.modTime) && info.Size() == s.size {
		return s.cached, nil
	}
	var f file
	if _, err := statefile.Load(s.Path, &f); err != nil {
		return nil, err
	}
	s.cached, s.modTime, s.size = f.Tokens, info.ModTime(), info.Size()
	if s.cached == nil {
		s.cached = []Token{}
	}
	return s.cached, nil
}

func (s *Store) save(tokens []Token) error {
	if err := statefile.Save(s.Path, file{Tokens: tokens}); err != nil {
		return err
	}
	s.cached = nil
	return nil
}

// nameRE is a token's name: a slug.
var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Create makes a token and returns it with its secret, which is never stored
// and cannot be shown again. ttl zero means it never expires.
func (s *Store) Create(name string, scopes []Scope, ttl time.Duration) (Token, string, error) {
	if !nameRE.MatchString(name) {
		return Token{}, "", fmt.Errorf("name %q: want a slug such as ci", name)
	}
	if len(scopes) == 0 {
		return Token{}, "", errors.New("name at least one scope")
	}
	if ttl < 0 {
		return Token{}, "", errors.New("the expiry must be in the future")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	if err != nil {
		return Token{}, "", err
	}
	now := s.now()
	for _, t := range tokens {
		if t.Name == name && t.State(now) == "active" {
			return Token{}, "", fmt.Errorf("an active token is already named %q; revoke it or pick another name", name)
		}
	}
	secret := Prefix + random(32)
	t := Token{ID: random(6), Name: name, Scopes: scopes, Hash: hashOf(secret), Created: now.UTC()}
	if ttl > 0 {
		t.Expires = now.Add(ttl).UTC()
	}
	out := append(append([]Token(nil), tokens...), t)
	if err := s.save(out); err != nil {
		return Token{}, "", err
	}
	return t, secret, nil
}

// List returns every token, newest first.
func (s *Store) List() ([]Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	if err != nil {
		return nil, err
	}
	out := append([]Token(nil), tokens...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out, nil
}

// Revoke marks the active token with this name or ID revoked. The record is
// kept, so the list still shows what existed and when it was turned off.
func (s *Store) Revoke(nameOrID string) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	if err != nil {
		return Token{}, err
	}
	out := append([]Token(nil), tokens...)
	now := s.now()
	for i, t := range out {
		if (t.Name == nameOrID || t.ID == nameOrID) && t.Revoked.IsZero() {
			out[i].Revoked = now.UTC()
			if err := s.save(out); err != nil {
				return Token{}, err
			}
			return out[i], nil
		}
	}
	return Token{}, fmt.Errorf("no unrevoked token is named %q", nameOrID)
}

// Authenticate checks a presented token. Every stored hash is compared, in
// constant time, whatever matches first, so the time taken does not say how
// many tokens exist or which one nearly matched.
func (s *Store) Authenticate(presented string) (Token, error) {
	if !strings.HasPrefix(presented, Prefix) || len(presented) > 256 {
		return Token{}, ErrUnknown
	}
	sum := sha256.Sum256([]byte(presented))
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	if err != nil {
		return Token{}, err
	}
	found := -1
	for i, t := range tokens {
		stored, err := hex.DecodeString(t.Hash)
		if err != nil || len(stored) != sha256.Size {
			continue
		}
		if subtle.ConstantTimeCompare(sum[:], stored) == 1 {
			found = i
		}
	}
	if found < 0 {
		return Token{}, ErrUnknown
	}
	t := tokens[found]
	now := s.now()
	switch t.State(now) {
	case "revoked":
		return t, ErrRevoked
	case "expired":
		return t, ErrExpired
	}
	s.touch(tokens, found, now)
	return tokens[found], nil
}

// touch records a use, writing it out at most once a minute per token.
func (s *Store) touch(tokens []Token, i int, now time.Time) {
	tokens[i].LastUsed = now.UTC()
	if s.written == nil {
		s.written = map[string]time.Time{}
	}
	id := tokens[i].ID
	if last, ok := s.written[id]; ok && now.Sub(last) < time.Minute {
		return
	}
	s.written[id] = now
	_ = s.save(append([]Token(nil), tokens...))
}

// ParseTTL reads an expiry such as 30d, 12h or 90m; "" and "never" are zero.
func ParseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "never" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("--expires %q: want a whole number of days such as 30d", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("--expires %q: want a duration such as 30d, 12h or 90m", s)
	}
	return d, nil
}

func hashOf(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error()) // rand.Read does not fail on supported platforms
	}
	if n <= 8 {
		return hex.EncodeToString(b)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
