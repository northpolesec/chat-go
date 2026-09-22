// JSON comparison convention for Block Kit tests (Task 18/19).
//
// Object pins (upstream toEqual / toMatchObject on objects): marshal both
// sides, unmarshal into any, must.Eq. That is byte-order-independent and
// what Task 19 should copy.
//
// String pins (upstream toBe / toEqual on a string): compare the string
// with must.Eq (exact bytes). Do not remarshal.
package blocks

import (
	"encoding/json"
	"testing"

	"github.com/shoenig/test/must"
)

func jsonEq(t *testing.T, expected, actual any) {
	t.Helper()
	must.Eq(t, jsonVal(t, expected), jsonVal(t, actual))
}

func jsonVal(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	must.NoError(t, err)
	var out any
	must.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func jsonMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := jsonVal(t, v).(map[string]any)
	must.True(t, ok)
	return m
}
