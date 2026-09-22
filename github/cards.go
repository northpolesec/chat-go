// Ported from packages/adapter-github/src/cards.ts @ 6adca36 (chat v4.40.0).
package github

import (
	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
)

// CardToGitHubMarkdown renders a card as GFM; the renderer is shared with Linear.
func CardToGitHubMarkdown(card chat.Card) string { return shared.CardToMarkdown(card) }

// CardToPlainText is the no-markdown fallback (actions omitted).
func CardToPlainText(card chat.Card) string { return shared.CardToPlainText(card) }
