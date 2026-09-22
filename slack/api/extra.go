// Ported from packages/adapter-slack/src/api/extra.ts @ 6adca36 (chat v4.40.0).
// Divergences: OpenView takes triggerID + view (brief); interactivityPointer
// is not on the signature (call Call("views.open", ...) for that path).
package api

import (
	"context"
	"encoding/json"
	"errors"
)

// RepliesOptions is the upstream SlackThreadRepliesOptions minus app-scoped fields.
type RepliesOptions struct {
	Channel            string
	Cursor             string
	IncludeAllMetadata *bool
	Inclusive          *bool
	Latest             string
	Limit              int
	Oldest             string
	TS                 string
}

// FetchThreadReplies calls conversations.replies. Messages stay on Raw.
func (c *Client) FetchThreadReplies(ctx context.Context, opts RepliesOptions) (Response, error) {
	raw, err := c.Call(ctx, "conversations.replies", repliesBody(opts), EncodingForm)
	if err != nil {
		return Response{}, err
	}
	if err := assertSlackOK("conversations.replies", raw); err != nil {
		return Response{}, err
	}
	return raw, nil
}

// OpenView calls views.open.
func (c *Client) OpenView(ctx context.Context, triggerID string, view json.RawMessage) (Response, error) {
	if triggerID == "" {
		return Response{}, errors.New("triggerId or interactivityPointer is required")
	}
	raw, err := c.Call(ctx, "views.open", map[string]any{
		"trigger_id": triggerID,
		"view":       view,
	}, EncodingForm)
	if err != nil {
		return Response{}, err
	}
	if err := assertSlackOK("views.open", raw); err != nil {
		return Response{}, err
	}
	return raw, nil
}

func repliesBody(opts RepliesOptions) map[string]any {
	body := map[string]any{
		"channel": opts.Channel,
		"ts":      opts.TS,
	}
	if opts.Cursor != "" {
		body["cursor"] = opts.Cursor
	}
	if opts.IncludeAllMetadata != nil {
		body["include_all_metadata"] = *opts.IncludeAllMetadata
	}
	if opts.Inclusive != nil {
		body["inclusive"] = *opts.Inclusive
	}
	if opts.Latest != "" {
		body["latest"] = opts.Latest
	}
	if opts.Limit != 0 {
		body["limit"] = opts.Limit
	}
	if opts.Oldest != "" {
		body["oldest"] = opts.Oldest
	}
	return body
}
