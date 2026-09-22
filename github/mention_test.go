package github

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestDetectMention(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want bool
	}{
		{"@chatbot please review", true},
		{"Hey @Chatbot, thoughts?", true},    // case-insensitive
		{"see @chatbot.", true},              // punctuation after
		{"email me@chatbot", false},          // \w before @
		{"@chatbot-bot is different", false}, // hyphen suffix → other name
		{"@chatbotx no", false},              // \w suffix
		{"no mention here", false},
		{"@12345 by id", true}, // botUserID fallback
		{"(@chatbot)", true},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			must.Eq(t, tc.want, detectMention(tc.text, "chatbot", "12345"))
		})
	}
	must.False(t, detectMention("@chatbot", "", ""))
}
