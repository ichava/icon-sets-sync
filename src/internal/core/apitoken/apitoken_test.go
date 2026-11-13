package apitoken

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	return &Store{Path: filepath.Join(t.TempDir(), FileName), Now: func() time.Time { return now }}, &now
}

func TestOnlyTheHashIsStored(t *testing.T) {
	s, _ := newStore(t)
	tok, secret, err := s.Create("ci", []Scope{ScopeRead}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, Prefix) || len(secret) != len(Prefix)+43 {
		t.Fatalf("secret %d chars", len(secret))
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), tok.Hash) {
		t.Fatal("the store must hold the hash and never the token")
	}
	info, _ := os.Stat(s.Path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	got, err := s.Authenticate(secret)
	if err != nil || got.Name != "ci" {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestExpiryRevocationAndUnknownTokens(t *testing.T) {
	s, now := newStore(t)
	_, secret, err := s.Create("ci", []Scope{ScopeSync}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(secret + "x"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("a near miss: %v", err)
	}
	if _, err := s.Authenticate("not-ours"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("no prefix: %v", err)
	}
	*now = now.Add(time.Hour)
	if _, err := s.Authenticate(secret); !errors.Is(err, ErrExpired) {
		t.Fatalf("at expiry: %v", err)
	}
	_, other, _ := s.Create("cd", []Scope{ScopeRead}, 0)
	if _, err := s.Revoke("cd"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(other); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked: %v", err)
	}
	if _, err := s.Revoke("cd"); err == nil {
		t.Fatal("revoking twice")
	}
	list, _ := s.List()
	if len(list) != 2 {
		t.Fatalf("revoked records are kept: %d", len(list))
	}
}

func TestLastUseIsRecordedAtMostOnceAMinute(t *testing.T) {
	s, now := newStore(t)
	_, secret, _ := s.Create("ci", []Scope{ScopeRead}, 0)
	if _, err := s.Authenticate(secret); err != nil {
		t.Fatal(err)
	}
	first, _ := os.Stat(s.Path)
	*now = now.Add(10 * time.Second)
	_, _ = s.Authenticate(secret)
	list, _ := s.List()
	if !list[0].LastUsed.Equal(now.Add(-10*time.Second)) && !list[0].LastUsed.Equal(*now) {
		t.Fatalf("last used %v", list[0].LastUsed)
	}
	*now = now.Add(time.Minute)
	_, _ = s.Authenticate(secret)
	reread := &Store{Path: s.Path}
	list, _ = reread.List()
	if !list[0].LastUsed.Equal(*now) {
		t.Fatalf("last use on disk %v, want %v (first write at %v)", list[0].LastUsed, *now, first.ModTime())
	}
}

func TestScopes(t *testing.T) {
	admin := Token{Scopes: []Scope{ScopeAdmin}}
	read := Token{Scopes: []Scope{ScopeRead}}
	if !admin.Has(ScopeConfigWrite) || !read.Has(ScopeRead) || read.Has(ScopeSync) {
		t.Fatal("Has")
	}
	if _, err := ParseScopes("read, write"); err == nil {
		t.Fatal("an unknown scope parsed")
	}
	if got, _ := ParseScopes("sync,read,sync"); len(got) != 2 {
		t.Fatalf("duplicates: %v", got)
	}
	for in, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "12h": 12 * time.Hour, "": 0, "never": 0} {
		if got, err := ParseTTL(in); err != nil || got != want {
			t.Errorf("%q: %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"0d", "-1h", "soon"} {
		if _, err := ParseTTL(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestAStoreOthersCanReadIsRefused(t *testing.T) {
	s, _ := newStore(t)
	if _, _, err := s.Create("ci", []Scope{ScopeRead}, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Store{Path: s.Path}).List(); err == nil || !strings.Contains(err.Error(), "readable by other users") {
		t.Fatalf("%v", err)
	}
}
