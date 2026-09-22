package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// verifySignature checks GitHub's X-Hub-Signature-256 ("sha256=<hex>")
// against body with the webhook secret. An empty secret never verifies.
func verifySignature(secret string, body []byte, header string) bool {
	if secret == "" || header == "" {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	expected := "sha256=" + hex.EncodeToString(m.Sum(nil))
	return hmac.Equal([]byte(header), []byte(expected))
}
