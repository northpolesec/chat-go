package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func sig(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	t.Parallel()
	body := []byte(`{"a":1}`)
	must.True(t, verifySignature("s", body, sig("s", string(body))))
	must.False(t, verifySignature("s", body, sig("other", string(body))))
	must.False(t, verifySignature("s", body, ""))
	must.False(t, verifySignature("", body, sig("", string(body))))
	must.False(t, verifySignature("s", body, "not-hex"))
}

func TestCheckTimestamp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	ms := func(d time.Duration) string { return strconv.FormatInt(now.Add(d).UnixMilli(), 10) }
	must.True(t, checkTimestamp(now, ms(-30*time.Second), 0))
	must.True(t, checkTimestamp(now, "", float64(now.Add(-30*time.Second).UnixMilli())))
	must.False(t, checkTimestamp(now, ms(-10*time.Minute), 0))
	must.False(t, checkTimestamp(now, ms(2*time.Minute), 0))
	must.False(t, checkTimestamp(now, "", 0))
	// header wins over the body
	must.False(t, checkTimestamp(now, ms(-10*time.Minute), float64(now.UnixMilli())))
}
