package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/shoenig/test/must"
)

func sig(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	t.Parallel()
	body := `{"action":"created"}`
	must.True(t, verifySignature("s3cret", []byte(body), sig("s3cret", body)))
	must.False(t, verifySignature("s3cret", []byte(body), sig("other", body)))
	must.False(t, verifySignature("s3cret", []byte(body), ""))
	must.False(t, verifySignature("s3cret", []byte(body), "sha1=abc"))
	must.False(t, verifySignature("", []byte(body), sig("", body))) // no secret configured → never valid
}
