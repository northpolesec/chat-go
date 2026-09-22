// Ported from packages/adapter-slack/src/webhook/index.test.ts @ 6adca36 (chat v4.40.0).
// Remaining cases: parseSlackWebhookBody, readSlackWebhook, and the two
// custom-verifier verifySlackRequest cases. Verification cases live in
// verify_test.go (Task 22).
package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func TestParseSlackWebhookBody(t *testing.T) {
	t.Parallel()

	t.Run("parses url verification payloads", func(t *testing.T) {
		t.Parallel()
		body := mustJSON(t, map[string]any{
			"challenge": "3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P",
			"token":     "deprecated",
			"type":      "url_verification",
		})
		payload, err := Parse("application/json", body, nil)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"kind":      "url_verification",
			"challenge": "3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P",
		})
	})

	t.Run("parses app mentions with provider-native continuation", func(t *testing.T) {
		t.Parallel()
		body := mustJSON(t, map[string]any{
			"api_app_id": "A123",
			"event": map[string]any{
				"channel": "C123",
				"text":    "<@U999> hello",
				"files": []any{
					map[string]any{
						"id":                   "F123",
						"mimetype":             "image/png",
						"name":                 "chart.png",
						"size":                 123,
						"title":                "Chart",
						"url_private":          "https://files.slack.com/files-pri/chart.png",
						"url_private_download": "https://files.slack.com/files-pri/chart-download.png",
					},
				},
				"thread_ts": "1710000000.000001",
				"ts":        "1710000000.000002",
				"type":      "app_mention",
				"user":      "U123",
			},
			"event_id":              "Ev123",
			"event_time":            1_710_000_000,
			"is_ext_shared_channel": true,
			"team_id":               "T123",
			"type":                  "event_callback",
		})
		h := make(http.Header)
		h.Set("x-slack-retry-num", "2")
		h.Set("x-slack-retry-reason", "http_timeout")

		payload, err := Parse("application/json", body, h)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"apiAppId":  "A123",
			"channelId": "C123",
			"continuation": map[string]any{
				"channelId": "C123",
				"teamId":    "T123",
				"threadTs":  "1710000000.000001",
			},
			"eventId":   "Ev123",
			"eventTime": 1_710_000_000,
			"files": []any{
				map[string]any{
					"downloadUrl": "https://files.slack.com/files-pri/chart-download.png",
					"id":          "F123",
					"mimeType":    "image/png",
					"name":        "chart.png",
					"size":        123,
					"title":       "Chart",
					"type":        "image",
					"url":         "https://files.slack.com/files-pri/chart.png",
				},
			},
			"isExtSharedChannel": true,
			"kind":               "app_mention",
			"retry":              map[string]any{"num": 2, "reason": "http_timeout"},
			"text":               "<@U999> hello",
			"threadTs":           "1710000000.000001",
			"ts":                 "1710000000.000002",
			"userId":             "U123",
		})
	})

	t.Run("uses ts as threadTs when app mentions are top-level messages", func(t *testing.T) {
		t.Parallel()
		body := mustJSON(t, map[string]any{
			"event": map[string]any{
				"channel": "C123",
				"text":    "hello",
				"ts":      "1710000000.000002",
				"type":    "app_mention",
				"user":    "U123",
			},
			"team_id": "T123",
			"type":    "event_callback",
		})
		payload, err := Parse("application/json", body, nil)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"continuation": map[string]any{"channelId": "C123", "threadTs": "1710000000.000002"},
			"kind":         "app_mention",
			"threadTs":     "1710000000.000002",
		})
	})

	t.Run("parses direct message events", func(t *testing.T) {
		t.Parallel()
		body := mustJSON(t, map[string]any{
			"event": map[string]any{
				"bot_id":       "B123",
				"channel":      "D123",
				"channel_type": "im",
				"subtype":      "bot_message",
				"text":         "hello",
				"ts":           "1710000000.000002",
				"type":         "message",
				"user":         "U123",
			},
			"team_id": "T123",
			"type":    "event_callback",
		})
		payload, err := Parse("", body, nil)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"botId":     "B123",
			"channelId": "D123",
			"kind":      "direct_message",
			"subtype":   "bot_message",
		})
	})

	t.Run("parses slash command form posts", func(t *testing.T) {
		t.Parallel()
		form := url.Values{
			"channel_id":            {"C123"},
			"channel_name":          {"general"},
			"command":               {"/deploy"},
			"enterprise_id":         {"E123"},
			"is_enterprise_install": {"true"},
			"response_url":          {"https://hooks.slack.com/commands/T123/1/abc"},
			"team_id":               {"T123"},
			"text":                  {"prod"},
			"trigger_id":            {"123.456.abc"},
			"user_id":               {"U123"},
			"user_name":             {"josh"},
		}
		body := form.Encode()
		payload, err := Parse("application/x-www-form-urlencoded", []byte(body), nil)
		must.NoError(t, err)
		got, ok := payload.(*SlashCommandPayload)
		must.True(t, ok)
		must.Eq(t, &SlashCommandPayload{
			ChannelID:           "C123",
			ChannelName:         "general",
			Command:             "/deploy",
			EnterpriseID:        "E123",
			IsEnterpriseInstall: true,
			Kind:                "slash_command",
			Raw:                 formMap(form),
			ResponseURL:         "https://hooks.slack.com/commands/T123/1/abc",
			Retry:               nil,
			TeamID:              "T123",
			Text:                "prod",
			TriggerID:           "123.456.abc",
			UserID:              "U123",
			UserName:            "josh",
		}, got)
	})

	t.Run("parses block action payloads", func(t *testing.T) {
		t.Parallel()
		raw := map[string]any{
			"actions": []any{
				map[string]any{
					"action_id": "approve",
					"block_id":  "actions",
					"selected_option": map[string]any{
						"text":  map[string]any{"text": "Yes", "type": "plain_text"},
						"value": "yes",
					},
					"text":  map[string]any{"text": "Approve", "type": "plain_text"},
					"type":  "button",
					"value": "approve-value",
				},
			},
			"channel": map[string]any{"id": "C123", "name": "general"},
			"container": map[string]any{
				"channel_id": "C123",
				"message_ts": "1710000000.000002",
				"thread_ts":  "1710000000.000001",
				"type":       "message",
			},
			"message": map[string]any{
				"blocks": []any{
					map[string]any{
						"text": map[string]any{"text": "Approve deployment?", "type": "mrkdwn"},
						"type": "section",
					},
				},
				"thread_ts": "1710000000.000001",
				"ts":        "1710000000.000002",
			},
			"response_url": "https://hooks.slack.com/actions/T123/1/abc",
			"team":         map[string]any{"enterprise_id": "E123", "id": "T123"},
			"trigger_id":   "123.456.abc",
			"type":         "block_actions",
			"user":         map[string]any{"id": "U123", "username": "josh"},
		}
		body := url.Values{"payload": {string(mustJSON(t, raw))}}.Encode()
		payload, err := Parse("application/x-www-form-urlencoded", []byte(body), nil)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"actions": []any{
				map[string]any{
					"actionId":            "approve",
					"blockId":             "actions",
					"label":               "Yes",
					"selectedOptionLabel": "Yes",
					"selectedOptionValue": "yes",
					"type":                "button",
					"user":                map[string]any{"id": "U123", "username": "josh"},
					"value":               "approve-value",
				},
			},
			"channelId": "C123",
			"continuation": map[string]any{
				"channelId":    "C123",
				"enterpriseId": "E123",
				"teamId":       "T123",
				"threadTs":     "1710000000.000001",
			},
			"kind": "block_actions",
			"messageBlocks": []any{
				map[string]any{
					"text": map[string]any{"text": "Approve deployment?", "type": "mrkdwn"},
					"type": "section",
				},
			},
			"messagePromptText": "Approve deployment?",
			"messageTs":         "1710000000.000002",
			"responseUrl":       "https://hooks.slack.com/actions/T123/1/abc",
			"teamId":            "T123",
			"threadTs":          "1710000000.000001",
			"triggerId":         "123.456.abc",
			"user":              map[string]any{"id": "U123", "username": "josh"},
			"userId":            "U123",
		})
	})

	t.Run("parses block suggestion payloads", func(t *testing.T) {
		t.Parallel()
		raw := map[string]any{
			"action_id":  "external",
			"block_id":   "input",
			"channel":    map[string]any{"id": "C123"},
			"enterprise": map[string]any{"id": "E123"},
			"team":       map[string]any{"id": "T123"},
			"type":       "block_suggestion",
			"user":       map[string]any{"id": "U123"},
			"value":      "hel",
		}
		body := url.Values{"payload": {string(mustJSON(t, raw))}}.Encode()
		payload, err := Parse("application/x-www-form-urlencoded", []byte(body), nil)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"actionId":     "external",
			"blockId":      "input",
			"channelId":    "C123",
			"enterpriseId": "E123",
			"kind":         "block_suggestion",
			"teamId":       "T123",
			"userId":       "U123",
			"value":        "hel",
		})
	})

	t.Run("parses view submissions", func(t *testing.T) {
		t.Parallel()
		raw := map[string]any{
			"team": map[string]any{"id": "T123"},
			"type": "view_submission",
			"user": map[string]any{"id": "U123"},
			"view": map[string]any{
				"callback_id":      "feedback",
				"id":               "V123",
				"private_metadata": `{"id":"123"}`,
				"response_urls": []any{
					map[string]any{
						"action_id":    "target",
						"channel_id":   "C123",
						"response_url": "https://hooks.slack.com/app/1/2/3",
					},
				},
				"state": map[string]any{
					"values": map[string]any{
						"feedback": map[string]any{
							"message": map[string]any{
								"type":  "plain_text_input",
								"value": "looks good",
							},
						},
					},
				},
			},
		}
		body := url.Values{"payload": {string(mustJSON(t, raw))}}.Encode()
		payload, err := Parse("application/x-www-form-urlencoded", []byte(body), nil)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"callbackId":      "feedback",
			"kind":            "view_submission",
			"privateMetadata": `{"id":"123"}`,
			"responseUrls": []any{
				map[string]any{
					"action_id":    "target",
					"channel_id":   "C123",
					"response_url": "https://hooks.slack.com/app/1/2/3",
				},
			},
			"teamId": "T123",
			"user":   map[string]any{"id": "U123"},
			"userId": "U123",
			"values": []any{
				map[string]any{
					"actionId": "message",
					"blockId":  "feedback",
					"type":     "plain_text_input",
					"value":    "looks good",
				},
			},
			"view": map[string]any{"callback_id": "feedback", "id": "V123"},
		})
	})

	t.Run("parses view closed payloads", func(t *testing.T) {
		t.Parallel()
		raw := map[string]any{
			"enterprise": map[string]any{"id": "E123"},
			"team":       nil,
			"type":       "view_closed",
			"user":       map[string]any{"id": "U123"},
			"view":       map[string]any{"id": "V123"},
		}
		body := url.Values{"payload": {string(mustJSON(t, raw))}}.Encode()
		payload, err := Parse("application/x-www-form-urlencoded", []byte(body), nil)
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"enterpriseId": "E123",
			"kind":         "view_closed",
			"userId":       "U123",
			"view":         map[string]any{"id": "V123"},
		})
	})

	t.Run("returns unsupported for valid but unsupported payloads", func(t *testing.T) {
		t.Parallel()
		body := mustJSON(t, map[string]any{
			"event": map[string]any{"type": "reaction_added"},
			"type":  "event_callback",
		})
		payload, err := Parse("", body, nil)
		must.NoError(t, err)
		got, ok := payload.(*UnsupportedPayload)
		must.True(t, ok)
		must.Eq(t, &UnsupportedPayload{
			Kind: "unsupported",
			Raw: map[string]any{
				"event": map[string]any{"type": "reaction_added"},
				"type":  "event_callback",
			},
			Retry: nil,
			Type:  "reaction_added",
		}, got)
	})

	t.Run("throws a parse error for invalid json", func(t *testing.T) {
		t.Parallel()
		_, err := Parse("application/json", []byte("{"), nil)
		must.ErrorIs(t, err, ErrInvalidJSON)
		must.Eq(t, "Slack webhook body is invalid JSON", err.Error())
	})
}

func TestVerifyRequestRejectsOversizedBody(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("x"), maxWebhookBody+1)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", bytes.NewReader(body))
	called := false
	_, err := VerifyRequest(req, ReadOptions{Verifier: func(*http.Request, []byte) (any, error) {
		called = true
		return true, nil
	}})
	must.ErrorIs(t, err, ErrBodyTooLarge)
	must.False(t, called)
}

func TestVerifySlackRequestCustomVerifier(t *testing.T) {
	t.Parallel()

	t.Run("uses a custom verifier", func(t *testing.T) {
		t.Parallel()
		called := false
		verifier := func(*http.Request, []byte) (any, error) {
			called = true
			return true, nil
		}
		req := httptest.NewRequest(http.MethodPost, "https://example.com", strings.NewReader("payload"))
		got, err := VerifyRequest(req, ReadOptions{Verifier: verifier})
		must.NoError(t, err)
		must.Eq(t, []byte("payload"), got)
		must.True(t, called)
	})

	t.Run("allows a custom verifier to replace the body", func(t *testing.T) {
		t.Parallel()
		verifiedBody := `{"challenge":"challenge-value","type":"url_verification"}`
		req := httptest.NewRequest(http.MethodPost, "https://example.com", strings.NewReader("original"))
		payload, err := Read(req, ReadOptions{
			Verifier: func(*http.Request, []byte) (any, error) {
				return verifiedBody, nil
			},
		})
		must.NoError(t, err)
		got, ok := payload.(*URLVerificationPayload)
		must.True(t, ok)
		must.Eq(t, &URLVerificationPayload{
			Challenge: "challenge-value",
			Kind:      "url_verification",
			Raw:       map[string]any{"challenge": "challenge-value", "type": "url_verification"},
			Retry:     nil,
		}, got)
	})
}

func TestReadSlackWebhook(t *testing.T) {
	t.Parallel()

	t.Run("verifies and parses requests", func(t *testing.T) {
		t.Parallel()
		body := `{"challenge":"challenge-value","type":"url_verification"}`
		req := httptest.NewRequest(http.MethodPost, "https://example.com/slack", strings.NewReader(body))
		req.Header = headers(body, timestamp)
		payload, err := Read(req, ReadOptions{SigningSecret: secret, Now: now()})
		must.NoError(t, err)
		matchObject(t, payload, map[string]any{
			"challenge": "challenge-value",
			"kind":      "url_verification",
		})
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	must.NoError(t, err)
	return b
}

func formMap(v url.Values) map[string]string {
	out := make(map[string]string, len(v))
	for k, vs := range v {
		if len(vs) > 0 {
			out[k] = vs[len(vs)-1]
		}
	}
	return out
}

func matchObject(t *testing.T, actual, expected any) {
	t.Helper()
	matchValue(t, jsonVal(t, actual), jsonVal(t, expected), "")
}

func matchValue(t *testing.T, actual, expected any, path string) {
	t.Helper()
	switch e := expected.(type) {
	case map[string]any:
		am, ok := actual.(map[string]any)
		must.True(t, ok, must.Sprintf("%s: want object", path))
		for k, ev := range e {
			p := k
			if path != "" {
				p = path + "." + k
			}
			matchValue(t, am[k], ev, p)
		}
	case []any:
		as, ok := actual.([]any)
		must.True(t, ok, must.Sprintf("%s: want array", path))
		must.Eq(t, len(e), len(as), must.Sprintf("%s: length", path))
		for i := range e {
			matchValue(t, as[i], e[i], path+"[]")
		}
	default:
		must.Eq(t, expected, actual, must.Sprintf("%s", path))
	}
}

func jsonVal(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	must.NoError(t, err)
	var out any
	must.NoError(t, json.Unmarshal(raw, &out))
	return out
}
