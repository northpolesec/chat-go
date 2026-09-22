// Ported from packages/adapter-github/src/markdown.ts @ 6adca36 (chat v4.40.0).
// The converter lives in internal/shared since the Linear adapter uses the same one.
package github

import "github.com/northpolesec/chat-go/internal/shared"

type githubFormatConverter = shared.MarkdownFormatConverter

func newGitHubFormatConverter() *githubFormatConverter { return shared.NewMarkdownFormatConverter() }
