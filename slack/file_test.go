package slack

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestIsSlackAuthURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://files.slack.com/files-pri/T1/F1/img.png",
		"https://files.slack-gov.com/files-pri/T1/F1/img.png",
		"https://slack-files.com/files-pri/T1/F1/img.png",
		"https://slack-files-gov.com/files-pri/T1/F1/img.png",
		"https://slack.com/api/files.info",
		"https://slack-gov.com/api/files.info",
		"HTTPS://FILES.SLACK.COM/files-pri/T1/F1/img.png",
		"https://files.slack.com:443/files-pri/T1/F1/img.png",
	} {
		t.Run("accepts "+raw, func(t *testing.T) {
			t.Parallel()
			must.True(t, isSlackAuthURL(raw, ""))
		})
	}

	t.Run("rejects a non-Slack host", func(t *testing.T) {
		t.Parallel()
		must.False(t, isSlackAuthURL("https://example.com/file.png", ""))
	})

	t.Run("rejects an invalid URL", func(t *testing.T) {
		t.Parallel()
		must.False(t, isSlackAuthURL("not a url", ""))
		must.False(t, isSlackAuthURL("", ""))
	})

	t.Run("accepts the optional apiUrl origin", func(t *testing.T) {
		t.Parallel()
		must.True(t, isSlackAuthURL("https://slack.example.com/file.png", "https://slack.example.com/api"))
	})

	t.Run("rejects a host that does not match apiUrl", func(t *testing.T) {
		t.Parallel()
		must.False(t, isSlackAuthURL("https://other.example.com/file.png", "https://slack.example.com/api"))
	})

	t.Run("listed Slack origin still matches when apiUrl is invalid", func(t *testing.T) {
		t.Parallel()
		must.True(t, isSlackAuthURL("https://files.slack.com/file.png", "not a url"))
	})
}
