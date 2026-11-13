package tlscert

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSelfSignedIsKeptAndRenewed(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	first, err := SelfSigned(dir, "192.168.1.20", now)
	if err != nil {
		t.Fatal(err)
	}
	if !first.New || !first.Generated || first.Config == nil || len(first.Fingerprint) != 95 {
		t.Fatalf("%+v", first)
	}
	leaf, err := x509.ParseCertificate(first.Config.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("192.168.1.20"); err != nil {
		t.Fatalf("the certificate does not name the bound host: %v", err)
	}
	for _, f := range []string{CertFile, KeyFile} {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v", f, err)
		}
	}
	again, err := SelfSigned(dir, "192.168.1.20", now.Add(24*time.Hour))
	if err != nil || again.New || again.Fingerprint != first.Fingerprint {
		t.Fatalf("a restart must keep the certificate and its fingerprint: %+v, %v", again, err)
	}
	moved, err := SelfSigned(dir, "10.0.0.5", now)
	if err != nil || !moved.New {
		t.Fatalf("another host needs a new certificate: %v", err)
	}
	expired, err := SelfSigned(dir, "10.0.0.5", now.Add(validity))
	if err != nil || !expired.New {
		t.Fatalf("an expiring certificate is replaced: %v", err)
	}
	own, err := Load(filepath.Join(dir, CertFile), filepath.Join(dir, KeyFile))
	if err != nil || own.Fingerprint != expired.Fingerprint {
		t.Fatalf("Load: %v", err)
	}
	if _, err := Load(filepath.Join(dir, CertFile), ""); err == nil {
		t.Fatal("a certificate without a key loaded")
	}
}
