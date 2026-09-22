// Ported from packages/adapter-slack/src/webhook/types.ts @ 6adca36 (chat v4.40.0).
// Divergences: SlackWebhookPayload union → Event interface (variants named
// after upstream, Slack prefix dropped — this package is webhook);
// optional strings/numbers are zero values ("" / 0); optional bools are
// *bool; retry/continuation pointers are nil when unset; error classes are
// sentinels (ErrInvalidJSON / ErrVerifierRejected, plus Task 22's
// verification sentinels); SlackHeaders / Request collapse to http.Header
// / *http.Request; SlackWebhookVerifier → WebhookVerifier(req, body)
// (any, error) — string/[]byte replaces the body, other truthy keeps it,
// falsy rejects.
package webhook

import (
	"net/http"
	"time"
)

const (
	// ErrInvalidJSON is SlackWebhookParseError ("Slack webhook body is invalid JSON").
	ErrInvalidJSON sentinel = "Slack webhook body is invalid JSON"
	// ErrVerifierRejected is "Slack webhook verifier rejected the request".
	ErrVerifierRejected sentinel = "Slack webhook verifier rejected the request"
)

// Event is the SlackWebhookPayload discriminated union.
type Event interface {
	webhookEvent()
}

// WebhookVerifier is SlackWebhookVerifier. A falsy result rejects; a string
// or []byte replaces the signed body; any other truthy value keeps it.
type WebhookVerifier func(req *http.Request, body []byte) (any, error)

// ReadOptions is SlackReadOptions (parse + verify), flattened.
type ReadOptions struct {
	ContentType   string
	SigningSecret string
	Now           time.Time
	Verifier      WebhookVerifier
}

// Retry is SlackRetry.
type Retry struct {
	Num    float64 `json:"num"`
	Reason string  `json:"reason,omitempty"`
}

// Continuation is SlackContinuation.
type Continuation struct {
	ChannelID    string `json:"channelId"`
	EnterpriseID string `json:"enterpriseId,omitempty"`
	TeamID       string `json:"teamId,omitempty"`
	ThreadTs     string `json:"threadTs"`
}

// User is SlackUser.
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	TeamID   string `json:"teamId,omitempty"`
	Username string `json:"username,omitempty"`
}

// File is SlackFile.
type File struct {
	DownloadURL string         `json:"downloadUrl,omitempty"`
	Filetype    string         `json:"filetype,omitempty"`
	ID          string         `json:"id"`
	MimeType    string         `json:"mimeType,omitempty"`
	Name        string         `json:"name,omitempty"`
	Raw         map[string]any `json:"raw"`
	Size        float64        `json:"size,omitempty"`
	Title       string         `json:"title,omitempty"`
	Type        string         `json:"type"`
	URL         string         `json:"url,omitempty"`
}

// URLVerificationPayload is SlackUrlVerificationPayload.
type URLVerificationPayload struct {
	Challenge string         `json:"challenge"`
	Kind      string         `json:"kind"`
	Raw       map[string]any `json:"raw"`
	Retry     *Retry         `json:"retry,omitempty"`
}

func (*URLVerificationPayload) webhookEvent() {}

// EventBasePayload is SlackEventBasePayload.
type EventBasePayload struct {
	APIAppID           string         `json:"apiAppId,omitempty"`
	ChannelID          string         `json:"channelId"`
	Continuation       Continuation   `json:"continuation"`
	EnterpriseID       string         `json:"enterpriseId,omitempty"`
	EventID            string         `json:"eventId,omitempty"`
	EventTime          float64        `json:"eventTime,omitempty"`
	Files              []File         `json:"files"`
	IsExtSharedChannel *bool          `json:"isExtSharedChannel,omitempty"`
	Raw                map[string]any `json:"raw"`
	Retry              *Retry         `json:"retry,omitempty"`
	TeamID             string         `json:"teamId,omitempty"`
	Text               string         `json:"text"`
	ThreadTs           string         `json:"threadTs"`
	Ts                 string         `json:"ts"`
	UserID             string         `json:"userId,omitempty"`
}

// AppMentionPayload is SlackAppMentionPayload.
type AppMentionPayload struct {
	EventBasePayload
	EventType string `json:"eventType"`
	Kind      string `json:"kind"`
}

func (*AppMentionPayload) webhookEvent() {}

// DirectMessagePayload is SlackDirectMessagePayload.
type DirectMessagePayload struct {
	EventBasePayload
	BotID     string `json:"botId,omitempty"`
	EventType string `json:"eventType"`
	Kind      string `json:"kind"`
	Subtype   string `json:"subtype,omitempty"`
}

func (*DirectMessagePayload) webhookEvent() {}

// SlashCommandPayload is SlackSlashCommandPayload.
type SlashCommandPayload struct {
	ChannelID           string            `json:"channelId"`
	ChannelName         string            `json:"channelName,omitempty"`
	Command             string            `json:"command"`
	EnterpriseID        string            `json:"enterpriseId,omitempty"`
	IsEnterpriseInstall bool              `json:"isEnterpriseInstall"`
	Kind                string            `json:"kind"`
	Raw                 map[string]string `json:"raw"`
	ResponseURL         string            `json:"responseUrl,omitempty"`
	Retry               *Retry            `json:"retry,omitempty"`
	TeamID              string            `json:"teamId,omitempty"`
	Text                string            `json:"text"`
	TriggerID           string            `json:"triggerId,omitempty"`
	UserID              string            `json:"userId"`
	UserName            string            `json:"userName,omitempty"`
}

func (*SlashCommandPayload) webhookEvent() {}

// Action is SlackAction.
type Action struct {
	ActionID            string         `json:"actionId"`
	BlockID             string         `json:"blockId,omitempty"`
	Label               string         `json:"label,omitempty"`
	Raw                 map[string]any `json:"raw"`
	SelectedOptionLabel string         `json:"selectedOptionLabel,omitempty"`
	SelectedOptionValue string         `json:"selectedOptionValue,omitempty"`
	Type                string         `json:"type"`
	User                *User          `json:"user,omitempty"`
	Value               string         `json:"value,omitempty"`
}

// BlockActionsPayload is SlackBlockActionsPayload.
type BlockActionsPayload struct {
	Actions             []Action       `json:"actions"`
	ChannelID           string         `json:"channelId,omitempty"`
	Continuation        *Continuation  `json:"continuation,omitempty"`
	EnterpriseID        string         `json:"enterpriseId,omitempty"`
	IsEnterpriseInstall *bool          `json:"isEnterpriseInstall,omitempty"`
	Kind                string         `json:"kind"`
	MessageBlocks       []any          `json:"messageBlocks,omitempty"`
	MessagePromptBlock  any            `json:"messagePromptBlock,omitempty"`
	MessagePromptText   string         `json:"messagePromptText,omitempty"`
	MessageTs           string         `json:"messageTs,omitempty"`
	Raw                 map[string]any `json:"raw"`
	ResponseURL         string         `json:"responseUrl,omitempty"`
	Retry               *Retry         `json:"retry,omitempty"`
	TeamID              string         `json:"teamId,omitempty"`
	ThreadTs            string         `json:"threadTs,omitempty"`
	TriggerID           string         `json:"triggerId,omitempty"`
	User                User           `json:"user"`
	UserID              string         `json:"userId"`
	UserName            string         `json:"userName,omitempty"`
}

func (*BlockActionsPayload) webhookEvent() {}

// BlockSuggestionPayload is SlackBlockSuggestionPayload.
type BlockSuggestionPayload struct {
	ActionID     string         `json:"actionId"`
	BlockID      string         `json:"blockId"`
	ChannelID    string         `json:"channelId,omitempty"`
	EnterpriseID string         `json:"enterpriseId,omitempty"`
	Kind         string         `json:"kind"`
	Raw          map[string]any `json:"raw"`
	Retry        *Retry         `json:"retry,omitempty"`
	TeamID       string         `json:"teamId,omitempty"`
	UserID       string         `json:"userId"`
	Value        string         `json:"value"`
}

func (*BlockSuggestionPayload) webhookEvent() {}

// ViewSubmissionPayload is SlackViewSubmissionPayload.
type ViewSubmissionPayload struct {
	CallbackID      string           `json:"callbackId,omitempty"`
	EnterpriseID    string           `json:"enterpriseId,omitempty"`
	Kind            string           `json:"kind"`
	PrivateMetadata string           `json:"privateMetadata,omitempty"`
	Raw             map[string]any   `json:"raw"`
	ResponseURLs    []any            `json:"responseUrls,omitempty"`
	Retry           *Retry           `json:"retry,omitempty"`
	TeamID          string           `json:"teamId,omitempty"`
	User            User             `json:"user"`
	UserID          string           `json:"userId"`
	Values          []ViewStateValue `json:"values"`
	View            map[string]any   `json:"view"`
}

func (*ViewSubmissionPayload) webhookEvent() {}

// ViewClosedPayload is SlackViewClosedPayload.
type ViewClosedPayload struct {
	EnterpriseID string         `json:"enterpriseId,omitempty"`
	Kind         string         `json:"kind"`
	Raw          map[string]any `json:"raw"`
	Retry        *Retry         `json:"retry,omitempty"`
	TeamID       string         `json:"teamId,omitempty"`
	User         User           `json:"user"`
	UserID       string         `json:"userId"`
	View         map[string]any `json:"view"`
}

func (*ViewClosedPayload) webhookEvent() {}

// ViewStateValue is SlackViewStateValue.
type ViewStateValue struct {
	ActionID            string         `json:"actionId"`
	BlockID             string         `json:"blockId"`
	Raw                 map[string]any `json:"raw"`
	SelectedOptionLabel string         `json:"selectedOptionLabel,omitempty"`
	SelectedOptionValue string         `json:"selectedOptionValue,omitempty"`
	Type                string         `json:"type,omitempty"`
	Value               string         `json:"value,omitempty"`
}

// UnsupportedPayload is SlackUnsupportedPayload.
type UnsupportedPayload struct {
	Kind  string `json:"kind"`
	Raw   any    `json:"raw"`
	Retry *Retry `json:"retry,omitempty"`
	Type  string `json:"type"`
}

func (*UnsupportedPayload) webhookEvent() {}
