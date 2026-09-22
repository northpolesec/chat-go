package slack

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/shoenig/test/must"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	must.NoError(t, err)
	return key
}

func TestEncryptTokenDecryptToken(t *testing.T) {
	t.Parallel()
	key := testKey(t)

	t.Run("round-trips a token correctly", func(t *testing.T) {
		t.Parallel()
		token := "xoxb-test-bot-token-12345"
		encrypted, err := encryptToken(token, key)
		must.NoError(t, err)
		decrypted, err := decryptToken(encrypted, key)
		must.NoError(t, err)
		must.Eq(t, token, decrypted)
	})

	t.Run("produces different ciphertexts for same input (random IV)", func(t *testing.T) {
		t.Parallel()
		token := "xoxb-same-token"
		a, err := encryptToken(token, key)
		must.NoError(t, err)
		b, err := encryptToken(token, key)
		must.NoError(t, err)
		must.NotEq(t, a.Data, b.Data)
		must.NotEq(t, a.IV, b.IV)
	})

	t.Run("decryption with wrong key throws", func(t *testing.T) {
		t.Parallel()
		token := "xoxb-secret"
		encrypted, err := encryptToken(token, key)
		must.NoError(t, err)
		wrongKey := testKey(t)
		_, err = decryptToken(encrypted, wrongKey)
		must.Error(t, err)
	})

	t.Run("decryption with tampered ciphertext throws", func(t *testing.T) {
		t.Parallel()
		token := "xoxb-secret"
		encrypted, err := encryptToken(token, key)
		must.NoError(t, err)
		encrypted.Data = base64.StdEncoding.EncodeToString([]byte("tampered"))
		_, err = decryptToken(encrypted, key)
		must.Error(t, err)
	})
}

func TestDecodeKey(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	keyBase64 := base64.StdEncoding.EncodeToString(key)
	keyHex := hex.EncodeToString(key)

	t.Run("decodes a valid 32-byte base64 key", func(t *testing.T) {
		t.Parallel()
		got, err := decodeKey(keyBase64)
		must.NoError(t, err)
		must.Eq(t, 32, len(got))
		must.Eq(t, key, got)
	})

	t.Run("decodes a valid 64-char hex key", func(t *testing.T) {
		t.Parallel()
		got, err := decodeKey(keyHex)
		must.NoError(t, err)
		must.Eq(t, 32, len(got))
		must.Eq(t, key, got)
	})

	t.Run("trims whitespace", func(t *testing.T) {
		t.Parallel()
		got, err := decodeKey("  " + keyBase64 + "  ")
		must.NoError(t, err)
		must.Eq(t, 32, len(got))
	})

	t.Run("throws for non-32-byte key", func(t *testing.T) {
		t.Parallel()
		short := make([]byte, 16)
		_, err := rand.Read(short)
		must.NoError(t, err)
		shortKey := base64.StdEncoding.EncodeToString(short)
		_, err = decodeKey(shortKey)
		must.ErrorContains(t, err, "encryption key must decode to exactly 32 bytes")
	})

	t.Run("throws for empty string", func(t *testing.T) {
		t.Parallel()
		_, err := decodeKey("")
		must.Error(t, err)
	})
}

func TestIsEncryptedTokenData(t *testing.T) {
	t.Parallel()
	key := testKey(t)

	t.Run("returns true for valid encrypted data", func(t *testing.T) {
		t.Parallel()
		encrypted, err := encryptToken("test", key)
		must.NoError(t, err)
		must.True(t, isEncryptedTokenData(encrypted))
	})

	t.Run("returns false for plain string", func(t *testing.T) {
		t.Parallel()
		must.False(t, isEncryptedTokenData("xoxb-token"))
	})

	t.Run("returns false for null/undefined", func(t *testing.T) {
		t.Parallel()
		must.False(t, isEncryptedTokenData(nil))
		var unset *encryptedTokenData
		must.False(t, isEncryptedTokenData(unset))
	})

	t.Run("returns false for object missing fields", func(t *testing.T) {
		t.Parallel()
		must.False(t, isEncryptedTokenData(map[string]any{"iv": "a", "data": "b"}))
		must.False(t, isEncryptedTokenData(map[string]any{"iv": "a", "tag": "c"}))
	})

	t.Run("returns false for object with non-string fields", func(t *testing.T) {
		t.Parallel()
		must.False(t, isEncryptedTokenData(map[string]any{"iv": 1, "data": 2, "tag": 3}))
	})
}

// Go-added: a ciphertext produced by Node createCipheriv("aes-256-gcm")
// (12-byte IV, 16-byte tag, no AAD) must decrypt here. Upstream tests are
// round-trips only; this pins the interop layout.
func TestDecryptTokenNodeFixedVector(t *testing.T) {
	t.Parallel()
	key, err := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	must.NoError(t, err)
	plain, err := decryptToken(encryptedTokenData{
		IV:   "AQIDBAUGBwgJCgsM",
		Data: "Ah19wsVxzDmPnJa3CYxrfQGz15hclIg=",
		Tag:  "38e2wceb/Vwtf572qmWjqw==",
	}, key)
	must.NoError(t, err)
	must.Eq(t, "xoxb-fixed-vector-token", plain)
}

// Go-added: pins the AES-256-GCM wire layout the installation store serializes.
func TestEncryptedTokenDataWireFormat(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	encrypted, err := encryptToken("xoxb-layout", key)
	must.NoError(t, err)

	iv, err := base64.StdEncoding.DecodeString(encrypted.IV)
	must.NoError(t, err)
	must.Eq(t, 12, len(iv))

	tag, err := base64.StdEncoding.DecodeString(encrypted.Tag)
	must.NoError(t, err)
	must.Eq(t, 16, len(tag))

	_, err = base64.StdEncoding.DecodeString(encrypted.Data)
	must.NoError(t, err)

	raw, err := json.Marshal(encrypted)
	must.NoError(t, err)
	var obj map[string]string
	must.NoError(t, json.Unmarshal(raw, &obj))
	must.Eq(t, 3, len(obj))
	must.Eq(t, encrypted.Data, obj["data"])
	must.Eq(t, encrypted.IV, obj["iv"])
	must.Eq(t, encrypted.Tag, obj["tag"])
}
