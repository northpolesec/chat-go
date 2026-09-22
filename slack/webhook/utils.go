// Ported from packages/adapter-slack/src/webhook/utils.ts @ 6adca36 (chat v4.40.0).
// Divergences: SlackHeaders → http.Header (getHeader reuses headerValue);
// trimStart → TrimLeftFunc(unicode.IsSpace); JSON.parse → json.Unmarshal
// into json.RawMessage then any (the sanctioned dynamic-unmarshal site);
// Number.isFinite → ParseFloat + IsNaN/IsInf.
package webhook

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

func getRetry(header http.Header) *Retry {
	retryNum := headerValue(header, "x-slack-retry-num")
	if retryNum == "" {
		return nil
	}
	num, err := strconv.ParseFloat(retryNum, 64)
	if err != nil || math.IsNaN(num) || math.IsInf(num, 0) {
		return nil
	}
	return &Retry{
		Num:    num,
		Reason: headerValue(header, "x-slack-retry-reason"),
	}
}

func isFormBody(body []byte, contentType string) bool {
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return true
	}
	if strings.Contains(contentType, "application/json") {
		return false
	}
	trimmed := strings.TrimLeftFunc(string(body), unicode.IsSpace)
	return !strings.HasPrefix(trimmed, "{") && strings.Contains(string(body), "=")
}

func parseJSONBody(body []byte) (any, error) {
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, ErrInvalidJSON
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, ErrInvalidJSON
	}
	return v, nil
}

func isRecord(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

func recordValue(value any) map[string]any {
	if !isRecord(value) {
		return nil
	}
	return value.(map[string]any)
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func optionalString(value any) string {
	return stringValue(value)
}
