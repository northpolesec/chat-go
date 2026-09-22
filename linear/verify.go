package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
	"time"
)

// timestampWindow is Linear's documented replay window for webhookTimestamp.
const timestampWindow = 60 * time.Second

// verifySignature checks Linear-Signature (hex HMAC-SHA256 of the raw body
// with the webhook signing secret). An empty secret never verifies.
func verifySignature(secret string, body []byte, header string) bool {
	if secret == "" || header == "" {
		return false
	}
	got, err := hex.DecodeString(header)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hmac.Equal(got, m.Sum(nil))
}

// checkTimestamp accepts a delivery whose timestamp (Linear-Timestamp
// header first, body webhookTimestamp second; both unix ms) is within
// timestampWindow of now. Both absent → false: the AgentSessionEvent
// payload declares webhookTimestamp non-null.
func checkTimestamp(now time.Time, header string, bodyMs float64) bool {
	ms, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		if bodyMs == 0 {
			return false
		}
		ms = int64(bodyMs)
	}
	return math.Abs(float64(now.Sub(time.UnixMilli(ms)))) <= float64(timestampWindow)
}
