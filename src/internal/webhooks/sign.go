// Package webhooks delivers events to the receivers webhooks.endpoints names,
// signed in the Standard Webhooks format (https://www.standardwebhooks.com/),
// through a dialer that checks every address it connects to.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The Standard Webhooks headers.
const (
	HeaderID        = "webhook-id"
	HeaderTimestamp = "webhook-timestamp"
	HeaderSignature = "webhook-signature"
)

// SecretPrefix starts every secret, as Standard Webhooks writes them.
const SecretPrefix = "whsec_"

// DefaultTolerance is how far a receiver lets a timestamp drift.
const DefaultTolerance = 5 * time.Minute

// secretKey is the HMAC key a secret stands for: the bytes after whsec_,
// base64-decoded. A secret without the prefix (from secret_env) is used as
// written, which is what the reference libraries do too.
func secretKey(secret string) []byte {
	if rest, ok := strings.CutPrefix(secret, SecretPrefix); ok {
		if key, err := base64.StdEncoding.DecodeString(rest); err == nil {
			return key
		}
	}
	return []byte(secret)
}

// Sign returns one signature, "v1,<base64 HMAC-SHA256(id.timestamp.body)>".
func Sign(secret, id string, ts time.Time, body []byte) string {
	return signWithKey(secretKey(secret), id, ts, body)
}

// signWithKey is Sign with the HMAC key already decoded.
func signWithKey(key []byte, id string, ts time.Time, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + strconv.FormatInt(ts.Unix(), 10) + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// SignatureHeader is the webhook-signature value for every secret currently
// valid: during a rotation, both the new and the old secret sign, space
// separated, so a receiver holding either verifies.
func SignatureHeader(secrets []string, id string, ts time.Time, body []byte) string {
	sigs := make([]string, 0, len(secrets))
	for _, s := range secrets {
		sigs = append(sigs, Sign(s, id, ts, body))
	}
	return strings.Join(sigs, " ")
}

// Errors Verify returns.
var (
	ErrNoSignature = errors.New("webhook: missing id, timestamp or signature header")
	ErrTimestamp   = errors.New("webhook: timestamp outside the tolerance")
	ErrSignature   = errors.New("webhook: no signature matches")
)

// Verify is the receiver's check: the timestamp is within tolerance of now,
// and one of the signatures in the header is the HMAC of id.timestamp.body
// under secret. It is the Go version of the check docs/tools/webhooks.md
// gives, and this package's tests use it.
func Verify(secret string, h http.Header, body []byte, now time.Time, tolerance time.Duration) error {
	id, tsText, sigs := h.Get(HeaderID), h.Get(HeaderTimestamp), h.Get(HeaderSignature)
	if id == "" || tsText == "" || sigs == "" {
		return ErrNoSignature
	}
	sec, err := strconv.ParseInt(tsText, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrTimestamp, tsText)
	}
	ts := time.Unix(sec, 0)
	if tolerance <= 0 {
		tolerance = DefaultTolerance
	}
	if now.Sub(ts) > tolerance || ts.Sub(now) > tolerance {
		return ErrTimestamp
	}
	want := Sign(secret, id, ts, body)
	for _, got := range strings.Fields(sigs) {
		if hmac.Equal([]byte(got), []byte(want)) {
			return nil
		}
	}
	return ErrSignature
}
