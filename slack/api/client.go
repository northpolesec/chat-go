// Ported from packages/adapter-slack/src/api/client.ts @ 6adca36 (chat v4.40.0).
// Divergences: free functions + per-call options → Client struct (app-scoped
// values become fields). SlackBotToken string|func → TokenSource.
// Extra is the request-scoped body decorator (Grid extras). Empty token
// omits Authorization (oauth.v2.access has no bot token).
// No retries (upstream has none). nil HTTPClient uses a package-owned
// client, never http.DefaultClient. isSlackAuthUrl lives here (exported)
// so this package does not import the parent slack package.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const defaultAPIURL = "https://slack.com/api/"

var defaultHTTPClient = &http.Client{}

var slackAuthOrigins = map[string]struct{}{
	"https://files.slack.com":     {},
	"https://files.slack-gov.com": {},
	"https://slack-files.com":     {},
	"https://slack-files-gov.com": {},
	"https://slack.com":           {},
	"https://slack-gov.com":       {},
}

// HTTPDoer is satisfied by *http.Client.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// TokenSource resolves a bot token. StaticToken covers the string form;
// a custom implementation covers the upstream async () => string form.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type staticToken string

// StaticToken returns a TokenSource that always yields tok.
func StaticToken(tok string) TokenSource { return staticToken(tok) }

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// Client is the Slack Web API client. App-scoped options that were per-call
// in upstream (token, fetch, apiUrl) live on the struct.
type Client struct {
	HTTPClient HTTPDoer    // nil → package-owned default client (NOT http.DefaultClient)
	APIURL     string      // "" → https://slack.com/api/
	Token      TokenSource // required
	// Extra, if set, rewrites the request body after asMap and before encode.
	// SlackAdapter uses this to inject Grid team_id / client_context_team_id
	// from request ctx. Nil Extra leaves the body unchanged.
	Extra func(context.Context, map[string]any) map[string]any
}

// Encoding selects the Slack Web API body encoding. Empty is form.
type Encoding string

const (
	EncodingForm Encoding = "form"
	EncodingJSON Encoding = "json"
)

// Response is the upstream SlackApiResponse plus the raw body.
type Response struct {
	OK               bool   `json:"ok"`
	Error            string `json:"error"`
	Needed           string `json:"needed"`
	Provided         string `json:"provided"`
	ResponseMetadata struct {
		Messages   []string `json:"messages"`
		NextCursor string   `json:"next_cursor"`
		Warnings   []string `json:"warnings"`
	} `json:"response_metadata"`
	Raw json.RawMessage `json:"-"`
}

// APIError is the upstream SlackApiError. Code is the Slack error string.
type APIError struct {
	Code     string
	Needed   string
	Provided string
	Warnings []string

	method string
	status int
	msg    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.msg != "" {
		return e.msg
	}
	if e.status != 0 {
		return fmt.Sprintf("Slack %s returned HTTP %d", e.method, e.status)
	}
	code := e.Code
	if code == "" {
		code = "unknown_error"
	}
	if e.method == "" {
		return code
	}
	return fmt.Sprintf("Slack %s failed: %s", e.method, code)
}

// MessageOptions is the upstream SlackMessageOptions minus app-scoped fields.
type MessageOptions struct {
	Blocks         []any
	Channel        string
	MarkdownText   string
	Metadata       any
	ReplyBroadcast *bool
	Text           string
	ThreadTS       string
	UnfurlLinks    *bool
	UnfurlMedia    *bool
}

// EphemeralOptions is the upstream SlackEphemeralOptions minus app-scoped fields.
type EphemeralOptions struct {
	MessageOptions
	User string
}

// UpdateOptions is the upstream SlackUpdateOptions minus app-scoped fields.
type UpdateOptions struct {
	MessageOptions
	TS string
}

// PostedMessage is the upstream SlackPostedMessage.
type PostedMessage struct {
	Channel string
	ID      string
	Raw     Response
}

// ResponseURLPayload is the upstream SlackResponseUrlPayload.
type ResponseURLPayload struct {
	Blocks          []any
	DeleteOriginal  *bool
	ReplaceOriginal *bool
	ResponseType    string
	Text            string
	ThreadTS        string
}

// FileUpload is the upstream SlackFileUpload. Data is []byte (no Blob).
type FileUpload struct {
	AltText     string
	Data        []byte
	Filename    string
	SnippetType string
	Title       string
}

// UploadOptions is the upstream SlackUploadOptions plus the files argument.
type UploadOptions struct {
	ChannelID      string
	InitialComment string
	ThreadTS       string
	Files          []FileUpload
}

// UploadedFile is one id from the external upload flow.
type UploadedFile struct {
	ID string
}

// Call POSTs method on the Slack Web API. HTTP 2xx + ok:false is not an error
// (assertSlackOk lives on the typed helpers).
func (c *Client) Call(ctx context.Context, method string, body any, enc Encoding) (Response, error) {
	token, err := c.token(ctx)
	if err != nil {
		return Response{}, err
	}
	m, err := asMap(body)
	if err != nil {
		return Response{}, err
	}
	if c != nil && c.Extra != nil {
		m = c.Extra(ctx, m)
		if m == nil {
			m = map[string]any{}
		}
	}
	encoded, contentType := EncodeSlackAPIBody(m, enc)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolve(method), strings.NewReader(encoded))
	if err != nil {
		return Response{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http().Do(req)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, err
	}
	parsed, err := parseResponse(raw)
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parsed, httpAPIError(method, resp.StatusCode, parsed)
	}
	return parsed, nil
}

// PostMessage calls chat.postMessage.
func (c *Client) PostMessage(ctx context.Context, opts MessageOptions) (PostedMessage, error) {
	body, err := slackMessageBody(opts)
	if err != nil {
		return PostedMessage{}, err
	}
	raw, err := c.Call(ctx, "chat.postMessage", body, EncodingForm)
	if err != nil {
		return PostedMessage{}, err
	}
	if err := assertSlackOK("chat.postMessage", raw); err != nil {
		return PostedMessage{}, err
	}
	return postedMessage(raw, false), nil
}

// PostEphemeral calls chat.postEphemeral.
func (c *Client) PostEphemeral(ctx context.Context, opts EphemeralOptions) (PostedMessage, error) {
	body, err := slackMessageBody(opts.MessageOptions)
	if err != nil {
		return PostedMessage{}, err
	}
	body["user"] = opts.User
	raw, err := c.Call(ctx, "chat.postEphemeral", body, EncodingForm)
	if err != nil {
		return PostedMessage{}, err
	}
	if err := assertSlackOK("chat.postEphemeral", raw); err != nil {
		return PostedMessage{}, err
	}
	return postedMessage(raw, true), nil
}

// UpdateMessage calls chat.update.
func (c *Client) UpdateMessage(ctx context.Context, opts UpdateOptions) (PostedMessage, error) {
	body, err := slackMessageBody(opts.MessageOptions)
	if err != nil {
		return PostedMessage{}, err
	}
	body["ts"] = opts.TS
	raw, err := c.Call(ctx, "chat.update", body, EncodingForm)
	if err != nil {
		return PostedMessage{}, err
	}
	if err := assertSlackOK("chat.update", raw); err != nil {
		return PostedMessage{}, err
	}
	return postedMessage(raw, false), nil
}

// DeleteMessage calls chat.delete.
func (c *Client) DeleteMessage(ctx context.Context, channel, ts string) error {
	raw, err := c.Call(ctx, "chat.delete", map[string]any{
		"channel": channel,
		"ts":      ts,
	}, EncodingForm)
	if err != nil {
		return err
	}
	return assertSlackOK("chat.delete", raw)
}

// SendResponseURL POSTs JSON to a Slack response_url. No bearer token.
// The host must be hooks.slack.com or hooks.slack-gov.com over HTTPS,
// with no userinfo and no explicit port.
func (c *Client) SendResponseURL(ctx context.Context, rawURL string, body any) error {
	if !TrustedResponseURL(rawURL) {
		return &APIError{method: "response_url", msg: "Refusing to send content to an untrusted Slack response_url"}
	}
	payload, err := encodeResponseURL(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{
			method: "response_url",
			status: resp.StatusCode,
			msg:    fmt.Sprintf("Slack response_url returned HTTP %d", resp.StatusCode),
		}
	}
	return nil
}

// UploadFiles runs Slack's external upload flow (get URL, POST bytes, complete).
func (c *Client) UploadFiles(ctx context.Context, opts UploadOptions) ([]UploadedFile, error) {
	if len(opts.Files) == 0 {
		return []UploadedFile{}, nil
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(opts.Files))
	for _, file := range opts.Files {
		upload, err := c.Call(ctx, "files.getUploadURLExternal", uploadURLBody(file), EncodingForm)
		if err != nil {
			return nil, err
		}
		if err := assertSlackOK("files.getUploadURLExternal", upload); err != nil {
			return nil, err
		}
		uploadURL, fileID := uploadURLAndID(upload)
		if uploadURL == "" || fileID == "" {
			return nil, &APIError{
				method: "files.getUploadURLExternal",
				msg:    "Slack files.getUploadURLExternal returned no upload URL",
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(file.Data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := c.http().Do(req)
		if err != nil {
			return nil, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &APIError{
				method: "files.upload",
				status: resp.StatusCode,
				msg:    fmt.Sprintf("Slack file upload returned HTTP %d", resp.StatusCode),
			}
		}
		ids = append(ids, fileID)
	}
	complete := map[string]any{
		"files": completeFiles(opts.Files, ids),
	}
	if opts.ChannelID != "" {
		complete["channel_id"] = opts.ChannelID
	}
	if opts.InitialComment != "" {
		complete["initial_comment"] = opts.InitialComment
	}
	if opts.ThreadTS != "" {
		complete["thread_ts"] = opts.ThreadTS
	}
	raw, err := c.Call(ctx, "files.completeUploadExternal", complete, EncodingForm)
	if err != nil {
		return nil, err
	}
	if err := assertSlackOK("files.completeUploadExternal", raw); err != nil {
		return nil, err
	}
	out := make([]UploadedFile, len(ids))
	for i, id := range ids {
		out[i] = UploadedFile{ID: id}
	}
	return out, nil
}

// FetchFile GETs a Slack file URL. Bearer auth is added only when
// IsSlackAuthURL is true. Non-Slack hosts, http, userinfo, and non-default
// ports are refused.
func (c *Client) FetchFile(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	if !trustedSlackFileURL(rawURL, c.apiURL()) {
		return nil, &APIError{method: "files.fetch", msg: "Refusing to fetch a non-Slack file URL"}
	}
	var token string
	if IsSlackAuthURL(rawURL, c.APIURL) {
		var err error
		token, err = c.token(ctx)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, &APIError{
			method: "files.fetch",
			status: resp.StatusCode,
			msg:    fmt.Sprintf("Slack file fetch returned HTTP %d", resp.StatusCode),
		}
	}
	return resp.Body, nil
}

// EncodeSlackAPIBody is the upstream encodeSlackApiBody. Empty enc is form.
func EncodeSlackAPIBody(body map[string]any, enc Encoding) (string, string) {
	if enc == EncodingJSON {
		raw, err := json.Marshal(removeUndefined(body))
		if err != nil {
			return "", "application/json"
		}
		return string(raw), "application/json"
	}
	params := url.Values{}
	for key, value := range body {
		if value == nil {
			continue
		}
		params.Set(key, encodeSlackAPIValue(value))
	}
	return params.Encode(), "application/x-www-form-urlencoded"
}

// TrustedResponseURL reports whether raw is an https Slack response_url
// on hooks.slack.com or hooks.slack-gov.com, with no userinfo and no port.
func TrustedResponseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Port() != "" || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "hooks.slack.com", "hooks.slack-gov.com":
		return true
	default:
		return false
	}
}

func trustedSlackFileURL(rawURL, apiURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	return IsSlackAuthURL(rawURL, apiURL)
}

// IsSlackAuthURL is the upstream isSlackAuthUrl. apiURL "" is unset.
func IsSlackAuthURL(rawURL, apiURL string) bool {
	origin, ok := httpOrigin(rawURL)
	if !ok {
		return false
	}
	if _, listed := slackAuthOrigins[origin]; listed {
		return true
	}
	if apiURL == "" {
		return false
	}
	apiOrigin, ok := httpOrigin(apiURL)
	if !ok {
		return false
	}
	return origin == apiOrigin
}

func (c *Client) http() HTTPDoer {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPClient
}

func (c *Client) apiURL() string {
	if c != nil && c.APIURL != "" {
		return c.APIURL
	}
	return defaultAPIURL
}

func (c *Client) resolve(method string) string {
	base, err := url.Parse(c.apiURL())
	if err != nil {
		return strings.TrimRight(c.apiURL(), "/") + "/" + method
	}
	rel, err := url.Parse(method)
	if err != nil {
		return c.apiURL() + method
	}
	return base.ResolveReference(rel).String()
}

func (c *Client) token(ctx context.Context) (string, error) {
	if c == nil || c.Token == nil {
		return "", errors.New("slack API token is required")
	}
	return c.Token.Token(ctx)
}

func asMap(body any) (map[string]any, error) {
	if body == nil {
		return map[string]any{}, nil
	}
	if m, ok := body.(map[string]any); ok {
		return m, nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return map[string]any{}, nil
	}
	return m, nil
}

func parseResponse(raw []byte) (Response, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, err
	}
	resp.Raw = append(json.RawMessage(nil), raw...)
	return resp, nil
}

func assertSlackOK(method string, resp Response) error {
	if resp.OK {
		return nil
	}
	code := resp.Error
	if code == "" {
		code = "unknown_error"
	}
	return &APIError{
		Code:     code,
		Needed:   resp.Needed,
		Provided: resp.Provided,
		Warnings: append([]string(nil), resp.ResponseMetadata.Warnings...),
		method:   method,
		msg:      fmt.Sprintf("Slack %s failed: %s", method, code),
	}
}

func httpAPIError(method string, status int, resp Response) *APIError {
	return &APIError{
		Code:     resp.Error,
		Needed:   resp.Needed,
		Provided: resp.Provided,
		Warnings: append([]string(nil), resp.ResponseMetadata.Warnings...),
		method:   method,
		status:   status,
		msg:      fmt.Sprintf("Slack %s returned HTTP %d", method, status),
	}
}

func slackMessageBody(opts MessageOptions) (map[string]any, error) {
	if err := assertSlackMessageContent(opts); err != nil {
		return nil, err
	}
	body := map[string]any{"channel": opts.Channel}
	if opts.Blocks != nil {
		body["blocks"] = opts.Blocks
	}
	if opts.MarkdownText != "" {
		body["markdown_text"] = opts.MarkdownText
	}
	if opts.Metadata != nil {
		body["metadata"] = opts.Metadata
	}
	if opts.ReplyBroadcast != nil {
		body["reply_broadcast"] = *opts.ReplyBroadcast
	}
	if opts.Text != "" {
		body["text"] = opts.Text
	}
	if opts.ThreadTS != "" {
		body["thread_ts"] = opts.ThreadTS
	}
	if opts.UnfurlLinks != nil {
		body["unfurl_links"] = *opts.UnfurlLinks
	}
	if opts.UnfurlMedia != nil {
		body["unfurl_media"] = *opts.UnfurlMedia
	}
	return body, nil
}

func assertSlackMessageContent(opts MessageOptions) error {
	if opts.MarkdownText != "" && (opts.Text != "" || opts.Blocks != nil) {
		return errors.New("markdownText cannot be used with text or blocks")
	}
	return nil
}

func encodeResponseURL(body any) ([]byte, error) {
	switch b := body.(type) {
	case ResponseURLPayload:
		return json.Marshal(responseURLBody(b))
	case map[string]any:
		return json.Marshal(removeUndefined(b))
	case json.RawMessage:
		return b, nil
	case []byte:
		return b, nil
	default:
		return json.Marshal(body)
	}
}

func responseURLBody(p ResponseURLPayload) map[string]any {
	out := map[string]any{}
	if p.Blocks != nil {
		out["blocks"] = p.Blocks
	}
	if p.DeleteOriginal != nil {
		out["delete_original"] = *p.DeleteOriginal
	}
	if p.ReplaceOriginal != nil {
		out["replace_original"] = *p.ReplaceOriginal
	}
	if p.ResponseType != "" {
		out["response_type"] = p.ResponseType
	}
	if p.Text != "" {
		out["text"] = p.Text
	}
	if p.ThreadTS != "" {
		out["thread_ts"] = p.ThreadTS
	}
	return out
}

func encodeSlackAPIValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func removeUndefined(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		if v != nil {
			out[k] = v
		}
	}
	return out
}

type postedWire struct {
	Channel string `json:"channel"`
	TS      string `json:"ts"`
	MsgTS   string `json:"message_ts"`
}

func postedMessage(resp Response, ephemeral bool) PostedMessage {
	var wire postedWire
	_ = json.Unmarshal(resp.Raw, &wire)
	id := wire.TS
	if ephemeral {
		id = wire.MsgTS
	}
	return PostedMessage{Channel: wire.Channel, ID: id, Raw: resp}
}

func uploadURLBody(file FileUpload) map[string]any {
	body := map[string]any{
		"filename": file.Filename,
		"length":   len(file.Data),
	}
	if file.AltText != "" {
		body["alt_txt"] = file.AltText
	}
	if file.SnippetType != "" {
		body["snippet_type"] = file.SnippetType
	}
	return body
}

type uploadWire struct {
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
}

func uploadURLAndID(resp Response) (string, string) {
	var wire uploadWire
	_ = json.Unmarshal(resp.Raw, &wire)
	return wire.UploadURL, wire.FileID
}

type completeFile struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func completeFiles(files []FileUpload, ids []string) []completeFile {
	out := make([]completeFile, len(files))
	for i, file := range files {
		title := file.Title
		if title == "" {
			title = file.Filename
		}
		out[i] = completeFile{ID: ids[i], Title: title}
	}
	return out
}

func httpOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	switch {
	case port == "", scheme == "https" && port == "443", scheme == "http" && port == "80":
		return scheme + "://" + host, true
	default:
		return scheme + "://" + host + ":" + port, true
	}
}
