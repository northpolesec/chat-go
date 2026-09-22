// Ported from packages/adapter-slack/src/file.ts @ 6adca36 (chat v4.40.0).
// Divergences: isSlackAuthUrl lives in slack/api (exported IsSlackAuthURL)
// so the api subpackage does not import this package. This file keeps a
// same-package alias. optional apiUrl is an empty string (unset) rather
// than undefined; origin is WHATWG-style (lowercase scheme/host, omit
// default ports) because net/url does neither (Node URL does both).
package slack

import "github.com/northpolesec/chat-go/slack/api"

func isSlackAuthURL(rawURL, apiURL string) bool {
	return api.IsSlackAuthURL(rawURL, apiURL)
}
