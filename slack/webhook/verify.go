// Ported from packages/adapter-slack/src/webhook/verify.ts @ 6adca36 (chat v4.40.0).
// Divergences: verifySlackSignature → Verify(signingSecret, header, body, now);
// now is time.Time (test seam; adapter passes time.Now()) not a now() ms
// callback; maxSkewSeconds is the default 300 (no options struct); failures
// are sentinel errors (errors.Is) not SlackWebhookVerificationError; Web
// Crypto availability check omitted (crypto/hmac is always present);
// webhookVerifier / verifySlackRequest / readSlackWebhook are not here
// (custom verifier is an adapter hook; parse+read are Task 23).
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxSkewSeconds is options.maxSkewSeconds ?? 300.
const DefaultMaxSkewSeconds = 300

// sentinel is a comparable error so errors.Is works and ST1005 does not
// rewrite upstream SlackWebhookVerificationError messages.
type sentinel string

func (s sentinel) Error() string { return string(s) }

const (
	// ErrSignatureMismatch is "Slack signature is invalid" (malformed or wrong digest).
	ErrSignatureMismatch sentinel = "Slack signature is invalid"
	// ErrTimestampSkew is "Slack timestamp is too old" (abs(now-ts) > 300).
	// Non-finite timestamps unwrap to this sentinel with message
	// "Slack timestamp is invalid".
	ErrTimestampSkew sentinel = "Slack timestamp is too old"
	// ErrMissingSignature is "Slack signature headers are required".
	ErrMissingSignature sentinel = "Slack signature headers are required"
	// ErrSigningSecretRequired is "Slack signing secret is required".
	// Fourth sentinel: empty secret is a caller config error, not a header miss.
	ErrSigningSecretRequired sentinel = "Slack signing secret is required"
)

type verificationError struct {
	msg string
	err error
}

func (e *verificationError) Error() string { return e.msg }
func (e *verificationError) Unwrap() error { return e.err }

// Verify checks a Slack webhook HMAC-SHA256 signature.
// The signed base is v0:<timestamp>:<body> using the raw timestamp header
// string. Comparison is hmac.Equal. Skew is rejected when
// math.Abs(now.Unix()-ts) > DefaultMaxSkewSeconds (300 inclusive is accepted).
func Verify(signingSecret string, header http.Header, body []byte, now time.Time) error {
	if signingSecret == "" {
		return ErrSigningSecretRequired
	}

	timestamp := headerValue(header, "x-slack-request-timestamp")
	signature := headerValue(header, "x-slack-signature")
	if timestamp == "" || signature == "" {
		return ErrMissingSignature
	}

	ts, err := strconv.ParseFloat(timestamp, 64)
	if err != nil || math.IsNaN(ts) || math.IsInf(ts, 0) {
		return &verificationError{msg: "Slack timestamp is invalid", err: ErrTimestampSkew}
	}

	if math.Abs(float64(now.Unix())-ts) > DefaultMaxSkewSeconds {
		return ErrTimestampSkew
	}

	sig, err := parseSlackSignature(signature)
	if err != nil {
		return err
	}

	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{':'})
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return ErrSignatureMismatch
	}
	return nil
}

func parseSlackSignature(signature string) ([]byte, error) {
	if !strings.HasPrefix(signature, "v0=") {
		return nil, ErrSignatureMismatch
	}
	hexStr := signature[3:]
	if hexStr == "" || len(hexStr)%2 != 0 {
		return nil, ErrSignatureMismatch
	}
	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, ErrSignatureMismatch
	}
	return decoded, nil
}

func headerValue(h http.Header, name string) string {
	if v := h.Get(name); v != "" {
		return v
	}
	lower := strings.ToLower(name)
	for key, values := range h {
		if strings.EqualFold(key, lower) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}
