// Ported from packages/adapter-slack/src/webhook/index.test.ts @ 6adca36 (chat v4.40.0).
// Verification cases only (verifySlackSignature + verifySlackRequest
// "returns the verified body"). parseSlackWebhookBody, readSlackWebhook,
// and custom-verifier cases are Task 23.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

const secret = "8f742231b10e8888abcd99yyyzzz85a5"
const timestamp int64 = 1_531_420_618

func now() time.Time {
	return time.Unix(timestamp, 0).UTC()
}

func sign(body string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte{':'})
	mac.Write([]byte(body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func headers(body string, ts int64) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	h.Set("X-Slack-Signature", sign(body, ts))
	return h
}

func TestVerifySlackSignature(t *testing.T) {
	t.Parallel()

	t.Run("accepts a valid Slack signature", func(t *testing.T) {
		t.Parallel()
		body := "token=xyzz0WbapA4vBCDEFasx0q6G&team_id=T1DC2JH3J&team_domain=testteamnow&channel_id=G8PSS9T3V&channel_name=foobar&user_id=U2CERLKJA&user_name=roadrunner&command=%2Fwebhook-collect&text=&response_url=https%3A%2F%2Fhooks.slack.com%2Fcommands%2FT1DC2JH3J%2F397700885554%2F96rGlfmibIGlgcZRskXaIFfN&trigger_id=398738663015.47445629121.803a0bc887a14d10d2c447fce8b6703c"
		must.NoError(t, Verify(secret, headers(body, timestamp), []byte(body), now()))
	})

	t.Run("rejects stale timestamps", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		err := Verify(secret, headers(body, timestamp-301), []byte(body), now())
		must.ErrorIs(t, err, ErrTimestampSkew)
		must.Eq(t, "Slack timestamp is too old", err.Error())
	})

	t.Run("rejects invalid signatures", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		h := headers(body, timestamp)
		h.Set("X-Slack-Signature", "v0=bad")
		err := Verify(secret, h, []byte(body), now())
		must.ErrorIs(t, err, ErrSignatureMismatch)
		must.Eq(t, "Slack signature is invalid", err.Error())
	})

	t.Run("rejects well-formed signatures with the wrong digest", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		h := headers(body, timestamp)
		h.Set("X-Slack-Signature", "v0="+strings.Repeat("0", 64))
		err := Verify(secret, h, []byte(body), now())
		must.ErrorIs(t, err, ErrSignatureMismatch)
		must.Eq(t, "Slack signature is invalid", err.Error())
	})

	t.Run("accepts plain object headers case-insensitively", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		h := http.Header{
			"Content-Type":              {"application/json"},
			"X-Slack-Request-Timestamp": {strconv.FormatInt(timestamp, 10)},
			"X-Slack-Signature":         {sign(body, timestamp)},
		}
		must.NoError(t, Verify(secret, h, []byte(body), now()))
	})
}

func TestVerifySlackRequest(t *testing.T) {
	t.Parallel()

	t.Run("returns the verified body", func(t *testing.T) {
		t.Parallel()
		body := `{"type":"event_callback"}`
		must.NoError(t, Verify(secret, headers(body, timestamp), []byte(body), now()))
	})
}

func TestVerifyGoAdded(t *testing.T) {
	t.Parallel()

	t.Run("matches Node createHmac sha256 hex digest", func(t *testing.T) {
		t.Parallel()
		// node:crypto createHmac("sha256", secret).update(`v0:${timestamp}:payload`).digest("hex")
		must.Eq(t, "v0=f7f1533d1cad29bbef61cb831d3f8091fce4bd4438a0fabd4d55008d21fc54ce", sign("payload", timestamp))
	})

	t.Run("rejects missing signature headers", func(t *testing.T) {
		t.Parallel()
		err := Verify(secret, http.Header{}, []byte("payload"), now())
		must.ErrorIs(t, err, ErrMissingSignature)
		must.Eq(t, "Slack signature headers are required", err.Error())
	})

	t.Run("rejects a missing signing secret", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		err := Verify("", headers(body, timestamp), []byte(body), now())
		must.ErrorIs(t, err, ErrSigningSecretRequired)
		must.Eq(t, "Slack signing secret is required", err.Error())
	})

	t.Run("rejects a non-numeric timestamp", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		h := headers(body, timestamp)
		h.Set("X-Slack-Request-Timestamp", "not-a-number")
		err := Verify(secret, h, []byte(body), now())
		must.ErrorIs(t, err, ErrTimestampSkew)
		must.Eq(t, "Slack timestamp is invalid", err.Error())
	})

	t.Run("accepts a timestamp at the 300s skew boundary", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		must.NoError(t, Verify(secret, headers(body, timestamp-300), []byte(body), now()))
	})

	t.Run("rejects a future timestamp beyond 300s", func(t *testing.T) {
		t.Parallel()
		body := "payload"
		err := Verify(secret, headers(body, timestamp+301), []byte(body), now())
		must.ErrorIs(t, err, ErrTimestampSkew)
	})
}
