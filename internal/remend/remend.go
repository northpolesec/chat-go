// Ported from packages/remend/src/index.ts @ streamdown v1.2.1.
// Divergences: Mend is the default-export remend(); MendWithOptions is remend(text, opts).
// Options exposes only InlineKatex (the opt-in flag). Default-true completions are
// disabled via remendOptions (unexported) so handler tests can pass option: false.
// Custom RemendHandler is unexported customHandler. LinkMode / incomplete-image
// early-return live in the pipeline. Built-in handle* functions stay unexported.
// Empty string is the Go stand-in for TS !text; non-string inputs cannot be passed.
package remend

import (
	"slices"
	"strings"
)

// Options configures remend. Completions default on except InlineKatex.
type Options struct {
	InlineKatex bool
}

const (
	prioritySingleTilde            = 0
	priorityComparisonOperators    = 5
	priorityHTMLTags               = 10
	prioritySetextHeadings         = 15
	priorityLinks                  = 20
	priorityBoldItalic             = 30
	priorityBold                   = 35
	priorityItalicDoubleUnderscore = 40
	priorityItalicSingleAsterisk   = 41
	priorityItalicSingleUnderscore = 42
	priorityInlineCode             = 50
	priorityStrikethrough          = 60
	priorityKatex                  = 70
	priorityInlineKatex            = 75
	priorityDefault                = 100
)

const (
	incompleteLinkMarker       = "streamdown:incomplete-link"
	incompleteImagePlaceholder = "streamdown:incomplete-image"
	linkModeProtocol           = "protocol"
	linkModeTextOnly           = "text-only"
)

type customHandler struct {
	handle   func(string) string
	name     string
	priority *int
}

type remendOptions struct {
	bold                *bool
	boldItalic          *bool
	comparisonOperators *bool
	htmlTags            *bool
	images              *bool
	inlineCode          *bool
	inlineKatex         bool
	italic              *bool
	katex               *bool
	linkMode            string
	links               *bool
	setextHeadings      *bool
	singleTilde         *bool
	strikethrough       *bool
	handlers            []customHandler
}

type builtInSpec struct {
	handle       func(string) string
	name         string
	optionKey    string
	priority     int
	earlyReturn  func(string) bool
	withLinkMode func(linkMode string) func(string) string
}

type enabledHandler struct {
	handle      func(string) string
	priority    int
	earlyReturn func(string) bool
}

func isEnabled(option *bool) bool {
	return option == nil || *option
}

func linksEarlyReturn(result string) bool {
	return strings.HasSuffix(result, "]("+incompleteLinkMarker+")") ||
		strings.HasSuffix(result, "]("+incompleteImagePlaceholder+")")
}

// builtInHandlers is the remend pipeline in upstream priority order.
func builtInHandlers() []builtInSpec {
	return []builtInSpec{
		{
			name:      "singleTilde",
			handle:    handleSingleTildeEscape,
			priority:  prioritySingleTilde,
			optionKey: "singleTilde",
		},
		{
			name:      "comparisonOperators",
			handle:    handleComparisonOperators,
			priority:  priorityComparisonOperators,
			optionKey: "comparisonOperators",
		},
		{
			name:      "htmlTags",
			handle:    handleIncompleteHTMLTag,
			priority:  priorityHTMLTags,
			optionKey: "htmlTags",
		},
		{
			name:      "setextHeadings",
			handle:    handleIncompleteSetextHeading,
			priority:  prioritySetextHeadings,
			optionKey: "setextHeadings",
		},
		{
			name:         "links",
			handle:       func(text string) string { return handleIncompleteLinksAndImages(text, linkModeProtocol) },
			priority:     priorityLinks,
			optionKey:    "links",
			earlyReturn:  linksEarlyReturn,
			withLinkMode: withLinkMode,
		},
		{
			name:      "boldItalic",
			handle:    handleIncompleteBoldItalic,
			priority:  priorityBoldItalic,
			optionKey: "boldItalic",
		},
		{
			name:      "bold",
			handle:    handleIncompleteBold,
			priority:  priorityBold,
			optionKey: "bold",
		},
		{
			name:      "italicDoubleUnderscore",
			handle:    handleIncompleteDoubleUnderscoreItalic,
			priority:  priorityItalicDoubleUnderscore,
			optionKey: "italic",
		},
		{
			name:      "italicSingleAsterisk",
			handle:    handleIncompleteSingleAsteriskItalic,
			priority:  priorityItalicSingleAsterisk,
			optionKey: "italic",
		},
		{
			name:      "italicSingleUnderscore",
			handle:    handleIncompleteSingleUnderscoreItalic,
			priority:  priorityItalicSingleUnderscore,
			optionKey: "italic",
		},
		{
			name:      "inlineCode",
			handle:    handleIncompleteInlineCode,
			priority:  priorityInlineCode,
			optionKey: "inlineCode",
		},
		{
			name:      "strikethrough",
			handle:    handleIncompleteStrikethrough,
			priority:  priorityStrikethrough,
			optionKey: "strikethrough",
		},
		{
			name:      "katex",
			handle:    handleIncompleteBlockKatex,
			priority:  priorityKatex,
			optionKey: "katex",
		},
		{
			name:      "inlineKatex",
			handle:    handleIncompleteInlineKatex,
			priority:  priorityInlineKatex,
			optionKey: "inlineKatex",
		},
	}
}

func optionByKey(opts remendOptions, key string) *bool {
	switch key {
	case "bold":
		return opts.bold
	case "boldItalic":
		return opts.boldItalic
	case "comparisonOperators":
		return opts.comparisonOperators
	case "htmlTags":
		return opts.htmlTags
	case "images":
		return opts.images
	case "inlineCode":
		return opts.inlineCode
	case "italic":
		return opts.italic
	case "katex":
		return opts.katex
	case "links":
		return opts.links
	case "setextHeadings":
		return opts.setextHeadings
	case "singleTilde":
		return opts.singleTilde
	case "strikethrough":
		return opts.strikethrough
	default:
		return nil
	}
}

func getEnabledBuiltInHandlers(opts remendOptions) []enabledHandler {
	linkMode := opts.linkMode
	if linkMode == "" {
		linkMode = linkModeProtocol
	}
	var out []enabledHandler
	for _, spec := range builtInHandlers() {
		if spec.name == "links" {
			if !isEnabled(opts.links) && !isEnabled(opts.images) {
				continue
			}
			handle := spec.handle
			if spec.withLinkMode != nil {
				handle = spec.withLinkMode(linkMode)
			}
			er := spec.earlyReturn
			if linkMode != linkModeProtocol {
				er = nil
			}
			out = append(out, enabledHandler{handle: handle, priority: spec.priority, earlyReturn: er})
			continue
		}
		if spec.name == "inlineKatex" {
			if !opts.inlineKatex {
				continue
			}
			out = append(out, enabledHandler{handle: spec.handle, priority: spec.priority, earlyReturn: spec.earlyReturn})
			continue
		}
		if !isEnabled(optionByKey(opts, spec.optionKey)) {
			continue
		}
		out = append(out, enabledHandler{handle: spec.handle, priority: spec.priority, earlyReturn: spec.earlyReturn})
	}
	return out
}

func remend(text string, opts remendOptions) string {
	if text == "" {
		return text
	}
	result := text
	if strings.HasSuffix(text, " ") && !strings.HasSuffix(text, "  ") {
		result = text[:len(text)-1]
	}

	enabled := getEnabledBuiltInHandlers(opts)
	all := make([]enabledHandler, 0, len(enabled)+len(opts.handlers))
	all = append(all, enabled...)
	for _, h := range opts.handlers {
		p := priorityDefault
		if h.priority != nil {
			p = *h.priority
		}
		all = append(all, enabledHandler{handle: h.handle, priority: p})
	}
	slices.SortStableFunc(all, func(a, b enabledHandler) int {
		return a.priority - b.priority
	})
	for _, h := range all {
		result = h.handle(result)
		if h.earlyReturn != nil && h.earlyReturn(result) {
			return result
		}
	}
	return result
}

// Mend closes incomplete markdown using remend defaults (inline KaTeX off).
func Mend(text string) string {
	return remend(text, remendOptions{})
}

// MendWithOptions closes incomplete markdown with the given options.
func MendWithOptions(text string, opts Options) string {
	return remend(text, remendOptions{inlineKatex: opts.InlineKatex})
}
