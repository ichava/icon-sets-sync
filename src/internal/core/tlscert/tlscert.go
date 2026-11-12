// Package tlscert gives serve a TLS certificate when it binds beyond
// loopback: the operator's own (api.tls_cert and api.tls_key), or one it
// generates once, self-signed, and keeps in the state directory, so its
// fingerprint stays the same from one start to the next and a client can pin
// it.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The generated pair, in the state directory.
const (
	CertFile = "tls-cert.pem"
	KeyFile  = "tls-key.pem"
)

// validity is how long a generated certificate lasts. A new one, with a new
// fingerprint, is generated after that or when the host changes.
const validity = 397 * 24 * time.Hour

// Result is the certificate serve presents.
type Result struct {
	Config *tls.Config
	// Fingerprint is the SHA-256 of the leaf certificate's DER, as hex pairs
	// separated by colons: what a browser shows and what curl --pinnedpubkey
	// is not (that one hashes the public key; see PublicKeyPin).
	Fingerprint string
	// PublicKeyPin is base64 SHA-256 of the leaf's public key, for curl's
	// --pinnedpubkey "sha256//<pin>".
	PublicKeyPin string
	// Generated is set when the certificate is the tool's own self-signed one,
	// and New when it was made on this start.
	Generated, New bool
	// CertPath is the file it came from.
	CertPath string
}

// Load reads the operator's certificate and key.
func Load(certPath, keyPath string) (Result, error) {
	if certPath == "" || keyPath == "" {
		return Result{}, errors.New("api.tls_cert and api.tls_key are set together")
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return Result{}, fmt.Errorf("api.tls_cert/api.tls_key: %w", err)
	}
	return result(pair, certPath)
}

// SelfSigned returns the generated certificate in dir, making one when there
// is none, it has expired (or will within a day), or it does not name host.
func SelfSigned(dir, host string, now time.Time) (Result, error) {
	certPath, keyPath := filepath.Join(dir, CertFile), filepath.Join(dir, KeyFile)
	if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		leaf, perr := x509.ParseCertificate(pair.Certificate[0])
		if perr == nil && now.Add(24*time.Hour).Before(leaf.NotAfter) && covers(leaf, host) {
			r, err := result(pair, certPath)
			r.Generated = true
			return r, err
		}
	}
	if err := generate(certPath, keyPath, host, now); err != nil {
		return Result{}, err
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return Result{}, err
	}
	r, err := result(pair, certPath)
	r.Generated, r.New = true, true
	return r, err
}

func covers(leaf *x509.Certificate, host string) bool {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	return leaf.VerifyHostname(strings.Trim(host, "[]")) == nil
}

func generate(certPath, keyPath, host string, now time.Time) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "icon-sets-sync"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	h := strings.Trim(host, "[]")
	switch ip := net.ParseIP(h); {
	case ip != nil && !ip.IsUnspecified():
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	case ip == nil && h != "" && h != "localhost":
		tmpl.DNSNames = append(tmpl.DNSNames, h)
	}
	if hn, err := os.Hostname(); err == nil && hn != "" && hn != h {
		tmpl.DNSNames = append(tmpl.DNSNames, hn)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}
	if err := writePEM(keyPath, "PRIVATE KEY", keyDER); err != nil {
		return err
	}
	return writePEM(certPath, "CERTIFICATE", der)
}

func writePEM(path, kind string, der []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: kind, Bytes: der}); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func result(pair tls.Certificate, certPath string) (Result, error) {
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(leaf.Raw)
	pin := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return Result{
		Config:       &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12},
		Fingerprint:  colonHex(sum[:]),
		PublicKeyPin: base64.StdEncoding.EncodeToString(pin[:]),
		CertPath:     certPath,
	}, nil
}

func colonHex(b []byte) string {
	h := strings.ToUpper(hex.EncodeToString(b))
	parts := make([]string, 0, len(b))
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}
