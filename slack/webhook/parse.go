// Ported from packages/adapter-slack/src/webhook/parse.ts @ 6adca36 (chat v4.40.0).
// Divergences: parseSlackWebhookBody → Parse(contentType, body, header);
// header is flattened SlackParseOptions.headers (retry + content-type
// fallback); Event is the payload union; URLSearchParams → url.ParseQuery
// (fromEntries last-wins); json.RawMessage + type switch is the sanctioned
// dynamic-unmarshal site. readSlackWebhook / verifySlackRequest live here
// as Read / VerifyRequest so Task 22's Verify signature stays intact.
package webhook

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxWebhookBody matches the GitHub and Linear adapters (25 MiB).
const maxWebhookBody = 25 << 20

// ErrBodyTooLarge is returned when a webhook body exceeds maxWebhookBody.
// The body is not passed to a WebhookVerifier.
const ErrBodyTooLarge sentinel = "Slack webhook body exceeds 25 MiB"

// Parse is parseSlackWebhookBody.
func Parse(contentType string, body []byte, header http.Header) (Event, error) {
	if contentType == "" {
		contentType = headerValue(header, "content-type")
	}
	retry := getRetry(header)
	if isFormBody(body, contentType) {
		return parseFormBody(body, retry)
	}
	raw, err := parseJSONBody(body)
	if err != nil {
		return nil, err
	}
	return classifyJSONPayload(raw, retry), nil
}

// Read is readSlackWebhook: verify (or custom hook) then parse.
func Read(req *http.Request, opts ReadOptions) (Event, error) {
	body, err := VerifyRequest(req, opts)
	if err != nil {
		return nil, err
	}
	return Parse(opts.ContentType, body, req.Header)
}

// VerifyRequest is verifySlackRequest. A WebhookVerifier replaces HMAC and
// may substitute the body; otherwise Verify is used.
func VerifyRequest(req *http.Request, opts ReadOptions) ([]byte, error) {
	body, err := readLimitedBody(req.Body)
	if err != nil {
		return nil, err
	}
	if opts.Verifier != nil {
		result, err := opts.Verifier(req, body)
		if err != nil {
			return nil, err
		}
		if isFalsy(result) {
			return nil, ErrVerifierRejected
		}
		switch r := result.(type) {
		case string:
			return []byte(r), nil
		case []byte:
			return r, nil
		default:
			return body, nil
		}
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := Verify(opts.SigningSecret, req.Header, body, now); err != nil {
		return nil, err
	}
	return body, nil
}

func readLimitedBody(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r, maxWebhookBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxWebhookBody {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}

func parseFormBody(body []byte, retry *Retry) (Event, error) {
	params, _ := url.ParseQuery(string(body))
	if params.Has("payload") {
		raw, err := parseJSONBody([]byte(params.Get("payload")))
		if err != nil {
			return nil, err
		}
		return classifyInteractionPayload(raw, retry), nil
	}
	if params.Has("command") {
		return parseSlashCommand(params, retry), nil
	}
	return &UnsupportedPayload{
		Kind:  "unsupported",
		Raw:   valuesMap(params),
		Retry: retry,
		Type:  "form",
	}, nil
}

func classifyJSONPayload(raw any, retry *Retry) Event {
	obj := recordValue(raw)
	if obj == nil {
		return &UnsupportedPayload{Kind: "unsupported", Raw: raw, Retry: retry, Type: "unknown"}
	}

	typ, _ := obj["type"].(string)
	if typ == "url_verification" {
		if challenge, ok := obj["challenge"].(string); ok {
			return &URLVerificationPayload{
				Challenge: challenge,
				Kind:      "url_verification",
				Raw:       obj,
				Retry:     retry,
			}
		}
	}

	event := recordValue(obj["event"])
	if typ != "event_callback" || event == nil {
		if typ == "" {
			typ = "unknown"
		}
		return &UnsupportedPayload{Kind: "unsupported", Raw: obj, Retry: retry, Type: typ}
	}

	eventType, _ := event["type"].(string)
	if eventType == "app_mention" {
		return parseMessageEvent("app_mention", obj, event, retry)
	}
	if eventType == "message" {
		if channelType, _ := event["channel_type"].(string); channelType == "im" {
			return parseMessageEvent("direct_message", obj, event, retry)
		}
	}
	if eventType == "" {
		eventType = "event_callback"
	}
	return &UnsupportedPayload{Kind: "unsupported", Raw: obj, Retry: retry, Type: eventType}
}

func classifyInteractionPayload(raw any, retry *Retry) Event {
	obj := recordValue(raw)
	if obj == nil {
		return &UnsupportedPayload{Kind: "unsupported", Raw: raw, Retry: retry, Type: "interaction"}
	}
	switch obj["type"] {
	case "block_actions":
		return parseBlockActions(obj, retry)
	case "block_suggestion":
		return parseBlockSuggestion(obj, retry)
	case "view_submission":
		return parseViewSubmission(obj, retry)
	case "view_closed":
		return parseViewClosed(obj, retry)
	default:
		typ, _ := obj["type"].(string)
		if typ == "" {
			typ = "interaction"
		}
		return &UnsupportedPayload{Kind: "unsupported", Raw: obj, Retry: retry, Type: typ}
	}
}

func parseMessageEvent(kind string, envelope, event map[string]any, retry *Retry) Event {
	channelID := stringValue(event["channel"])
	ts := stringValue(event["ts"])
	threadTs := optionalString(event["thread_ts"])
	if threadTs == "" {
		threadTs = ts
	}
	teamID := optionalString(event["team_id"])
	if teamID == "" {
		teamID = optionalString(envelope["team_id"])
	}
	enterpriseID := optionalString(envelope["enterprise_id"])
	if enterpriseID == "" {
		enterpriseID = optionalString(envelope["context_enterprise_id"])
	}
	base := EventBasePayload{
		APIAppID:     optionalString(envelope["api_app_id"]),
		ChannelID:    channelID,
		Continuation: Continuation{ChannelID: channelID, EnterpriseID: enterpriseID, TeamID: teamID, ThreadTs: threadTs},
		EnterpriseID: enterpriseID,
		EventID:      optionalString(envelope["event_id"]),
		EventTime:    numberValue(envelope["event_time"]),
		Files:        parseFiles(event["files"]),
		Raw:          event,
		Retry:        retry,
		TeamID:       teamID,
		Text:         stringValue(event["text"]),
		ThreadTs:     threadTs,
		Ts:           ts,
		UserID:       optionalString(event["user"]),
	}
	if v, ok := envelope["is_ext_shared_channel"].(bool); ok {
		base.IsExtSharedChannel = &v
	}
	if kind == "app_mention" {
		return &AppMentionPayload{EventBasePayload: base, EventType: "app_mention", Kind: kind}
	}
	return &DirectMessagePayload{
		EventBasePayload: base,
		BotID:            optionalString(event["bot_id"]),
		EventType:        "message",
		Kind:             kind,
		Subtype:          optionalString(event["subtype"]),
	}
}

func parseSlashCommand(params url.Values, retry *Retry) Event {
	return &SlashCommandPayload{
		ChannelID:           params.Get("channel_id"),
		ChannelName:         params.Get("channel_name"),
		Command:             params.Get("command"),
		EnterpriseID:        params.Get("enterprise_id"),
		IsEnterpriseInstall: params.Get("is_enterprise_install") == "true",
		Kind:                "slash_command",
		Raw:                 valuesMap(params),
		ResponseURL:         params.Get("response_url"),
		Retry:               retry,
		TeamID:              params.Get("team_id"),
		Text:                params.Get("text"),
		TriggerID:           params.Get("trigger_id"),
		UserID:              params.Get("user_id"),
		UserName:            params.Get("user_name"),
	}
}

func parseBlockActions(raw map[string]any, retry *Retry) Event {
	channel := recordValue(raw["channel"])
	container := recordValue(raw["container"])
	message := recordValue(raw["message"])
	user := parseUser(raw["user"])
	team := recordValue(raw["team"])
	enterprise := recordValue(raw["enterprise"])
	channelID := firstNonEmpty(optionalString(channel["id"]), optionalString(container["channel_id"]))
	messageTs := firstNonEmpty(optionalString(message["ts"]), optionalString(container["message_ts"]))
	threadTs := firstNonEmpty(optionalString(message["thread_ts"]), optionalString(container["thread_ts"]), messageTs)
	teamID := firstNonEmpty(optionalString(team["id"]), user.TeamID)
	enterpriseID := firstNonEmpty(optionalString(enterprise["id"]), optionalString(team["enterprise_id"]))
	var continuation *Continuation
	if channelID != "" && threadTs != "" {
		continuation = &Continuation{ChannelID: channelID, EnterpriseID: enterpriseID, TeamID: teamID, ThreadTs: threadTs}
	}
	var messageBlocks []any
	if blocks, ok := message["blocks"].([]any); ok {
		messageBlocks = blocks
	}
	messagePromptBlock := findPromptBlock(messageBlocks)
	actions := []Action{}
	if rawActions, ok := raw["actions"].([]any); ok {
		actions = make([]Action, 0, len(rawActions))
		for _, a := range rawActions {
			actions = append(actions, parseAction(a, &user))
		}
	}
	out := &BlockActionsPayload{
		Actions:            actions,
		ChannelID:          channelID,
		Continuation:       continuation,
		EnterpriseID:       enterpriseID,
		Kind:               "block_actions",
		MessageBlocks:      messageBlocks,
		MessagePromptBlock: messagePromptBlock,
		MessagePromptText:  readPromptText(messagePromptBlock),
		MessageTs:          messageTs,
		Raw:                raw,
		ResponseURL:        optionalString(raw["response_url"]),
		Retry:              retry,
		TeamID:             teamID,
		ThreadTs:           threadTs,
		TriggerID:          optionalString(raw["trigger_id"]),
		User:               user,
		UserID:             user.ID,
		UserName:           firstNonEmpty(user.Username, user.Name),
	}
	if v, ok := raw["is_enterprise_install"].(bool); ok {
		out.IsEnterpriseInstall = &v
	}
	return out
}

func parseAction(action any, user *User) Action {
	raw := recordValue(action)
	if raw == nil {
		raw = map[string]any{}
	}
	selectedOption := recordValue(raw["selected_option"])
	text := recordValue(raw["text"])
	selectedText := recordValue(selectedOption["text"])
	return Action{
		ActionID:            stringValue(raw["action_id"]),
		BlockID:             optionalString(raw["block_id"]),
		Label:               firstNonEmpty(optionalString(selectedText["text"]), optionalString(text["text"])),
		Raw:                 raw,
		SelectedOptionLabel: optionalString(selectedText["text"]),
		SelectedOptionValue: optionalString(selectedOption["value"]),
		Type:                stringValue(raw["type"]),
		User:                user,
		Value:               optionalString(raw["value"]),
	}
}

func parseBlockSuggestion(raw map[string]any, retry *Retry) Event {
	channel := recordValue(raw["channel"])
	team := recordValue(raw["team"])
	enterprise := recordValue(raw["enterprise"])
	user := recordValue(raw["user"])
	return &BlockSuggestionPayload{
		ActionID:     stringValue(raw["action_id"]),
		BlockID:      stringValue(raw["block_id"]),
		ChannelID:    optionalString(channel["id"]),
		EnterpriseID: firstNonEmpty(optionalString(enterprise["id"]), optionalString(team["enterprise_id"])),
		Kind:         "block_suggestion",
		Raw:          raw,
		Retry:        retry,
		TeamID:       optionalString(team["id"]),
		UserID:       stringValue(user["id"]),
		Value:        stringValue(raw["value"]),
	}
}

func parseViewSubmission(raw map[string]any, retry *Retry) Event {
	team := recordValue(raw["team"])
	enterprise := recordValue(raw["enterprise"])
	user := parseUser(raw["user"])
	view := recordValue(raw["view"])
	if view == nil {
		view = map[string]any{}
	}
	var responseURLs []any
	if urls, ok := view["response_urls"].([]any); ok {
		responseURLs = urls
	}
	return &ViewSubmissionPayload{
		CallbackID:      optionalString(view["callback_id"]),
		EnterpriseID:    firstNonEmpty(optionalString(enterprise["id"]), optionalString(team["enterprise_id"])),
		Kind:            "view_submission",
		PrivateMetadata: optionalString(view["private_metadata"]),
		Raw:             raw,
		ResponseURLs:    responseURLs,
		Retry:           retry,
		TeamID:          optionalString(team["id"]),
		User:            user,
		UserID:          user.ID,
		Values:          parseViewValues(view),
		View:            view,
	}
}

func parseViewClosed(raw map[string]any, retry *Retry) Event {
	team := recordValue(raw["team"])
	enterprise := recordValue(raw["enterprise"])
	user := parseUser(raw["user"])
	view := recordValue(raw["view"])
	if view == nil {
		view = map[string]any{}
	}
	return &ViewClosedPayload{
		EnterpriseID: firstNonEmpty(optionalString(enterprise["id"]), optionalString(team["enterprise_id"])),
		Kind:         "view_closed",
		Raw:          raw,
		Retry:        retry,
		TeamID:       optionalString(team["id"]),
		User:         user,
		UserID:       user.ID,
		View:         view,
	}
}

func parseFiles(value any) []File {
	arr, ok := value.([]any)
	if !ok {
		return []File{}
	}
	out := make([]File, 0, len(arr))
	for _, item := range arr {
		file := recordValue(item)
		if file == nil {
			continue
		}
		mimeType := optionalString(file["mimetype"])
		out = append(out, File{
			DownloadURL: optionalString(file["url_private_download"]),
			Filetype:    optionalString(file["filetype"]),
			ID:          stringValue(file["id"]),
			MimeType:    mimeType,
			Name:        optionalString(file["name"]),
			Raw:         file,
			Size:        numberValue(file["size"]),
			Title:       optionalString(file["title"]),
			Type:        inferFileType(mimeType),
			URL:         optionalString(file["url_private"]),
		})
	}
	return out
}

func inferFileType(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	default:
		return "file"
	}
}

func parseUser(value any) User {
	user := recordValue(value)
	if user == nil {
		user = map[string]any{}
	}
	return User{
		ID:       stringValue(user["id"]),
		Name:     optionalString(user["name"]),
		TeamID:   optionalString(user["team_id"]),
		Username: optionalString(user["username"]),
	}
}

func findPromptBlock(blocks []any) any {
	for _, block := range blocks {
		item := recordValue(block)
		if item == nil {
			continue
		}
		if item["type"] == "section" && recordValue(item["text"]) != nil {
			return block
		}
	}
	return nil
}

func readPromptText(block any) string {
	item := recordValue(block)
	text := recordValue(item["text"])
	return optionalString(text["text"])
}

func parseViewValues(view map[string]any) []ViewStateValue {
	state := recordValue(view["state"])
	values := recordValue(state["values"])
	if values == nil {
		return []ViewStateValue{}
	}
	out := make([]ViewStateValue, 0)
	for blockID, block := range values {
		actions := recordValue(block)
		if actions == nil {
			continue
		}
		for actionID, action := range actions {
			raw := recordValue(action)
			if raw == nil {
				continue
			}
			selectedOption := recordValue(raw["selected_option"])
			selectedText := recordValue(selectedOption["text"])
			out = append(out, ViewStateValue{
				ActionID:            actionID,
				BlockID:             blockID,
				Raw:                 raw,
				SelectedOptionLabel: optionalString(selectedText["text"]),
				SelectedOptionValue: optionalString(selectedOption["value"]),
				Type:                optionalString(raw["type"]),
				Value:               optionalString(raw["value"]),
			})
		}
	}
	return out
}

func valuesMap(params url.Values) map[string]string {
	out := make(map[string]string, len(params))
	for k, vs := range params {
		if len(vs) > 0 {
			out[k] = vs[len(vs)-1]
		}
	}
	return out
}

func numberValue(value any) float64 {
	n, _ := value.(float64)
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func isFalsy(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case bool:
		return !x
	case string:
		return x == ""
	default:
		return false
	}
}
