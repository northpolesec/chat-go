// Ported from packages/adapter-slack/src/crypto.ts @ 6adca36 (chat v4.40.0).
// Implementation lives in packages/adapter-shared/src/crypto.ts (slack
// re-exports it). Divergences: helpers stay unexported (Task 30 store uses them);
// encryptToken/decryptToken return errors instead of throwing;
// isEncryptedTokenData accepts encryptedTokenData, *encryptedTokenData, or
// map[string]any (the TS type guard); Node Buffer base64 is more lenient
// than encoding/base64 (invalid alphabet is an error here).
package slack

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const (
	aesKeySize    = 32
	hexKeyCharset = `^[0-9a-fA-F]{64}$`
)

var hexKeyPattern = regexp.MustCompile(hexKeyCharset)

// encryptedTokenData is the AES-256-GCM wire form stored on an installation.
// data is ciphertext without the tag; iv is the 12-byte nonce; tag is 16 bytes.
// All three fields are standard base64.
type encryptedTokenData struct {
	Data string `json:"data"`
	IV   string `json:"iv"`
	Tag  string `json:"tag"`
}

func encryptToken(plaintext string, key []byte) (encryptedTokenData, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return encryptedTokenData{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return encryptedTokenData{}, err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	tagStart := len(sealed) - gcm.Overhead()
	return encryptedTokenData{
		Data: base64.StdEncoding.EncodeToString(sealed[:tagStart]),
		IV:   base64.StdEncoding.EncodeToString(nonce),
		Tag:  base64.StdEncoding.EncodeToString(sealed[tagStart:]),
	}, nil
}

func decryptToken(encrypted encryptedTokenData, key []byte) (string, error) {
	iv, err := base64.StdEncoding.DecodeString(encrypted.IV)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.Data)
	if err != nil {
		return "", err
	}
	tag, err := base64.StdEncoding.DecodeString(encrypted.Tag)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(iv) != gcm.NonceSize() {
		return "", fmt.Errorf("invalid nonce length %d", len(iv))
	}
	if len(tag) != gcm.Overhead() {
		return "", fmt.Errorf("invalid tag length %d", len(tag))
	}
	sealed := make([]byte, 0, len(ciphertext)+len(tag))
	sealed = append(sealed, ciphertext...)
	sealed = append(sealed, tag...)
	plain, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes (received %d)", aesKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func isEncryptedTokenData(value any) bool {
	switch v := value.(type) {
	case encryptedTokenData:
		return true
	case *encryptedTokenData:
		return v != nil
	case map[string]any:
		_, okIV := v["iv"].(string)
		_, okData := v["data"].(string)
		_, okTag := v["tag"].(string)
		return okIV && okData && okTag
	default:
		return false
	}
}

func decodeKey(rawKey string) ([]byte, error) {
	trimmed := strings.TrimSpace(rawKey)
	var key []byte
	var err error
	if hexKeyPattern.MatchString(trimmed) {
		key, err = hex.DecodeString(trimmed)
	} else {
		key, err = decodeKeyBase64(trimmed)
	}
	if err != nil {
		return nil, err
	}
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("encryption key must decode to exactly 32 bytes (received %d). use a 64-char hex string or 44-char base64 string", len(key))
	}
	return key, nil
}

func decodeKeyBase64(s string) ([]byte, error) {
	if key, err := base64.StdEncoding.DecodeString(s); err == nil {
		return key, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
