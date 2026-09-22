// Ported from packages/adapter-linear/src/index.ts (handleWebhook,
// onAgentSessionEvent) @ vercel/chat v4.40.0. Go additions: delivery claim
// via the state adapter, stop-signal handling (upstream treats stop as a
// normal prompt), null-issue and self drops, 500 on dispatch failure.
package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/northpolesec/chat-go/chat"
)

const (
	// maxWebhookBody mirrors the GitHub adapter's cap; Linear payloads are
	// far smaller.
	maxWebhookBody = 25 << 20

	headerSignature = "Linear-Signature"
	headerTimestamp = "Linear-Timestamp"

	// claimTTL bounds the dedupe key; Linear's last retry is 6 h after the first.
	claimTTL = 24 * time.Hour
	// postDeadline bounds the terminal post after a stop, which must survive
	// the request context.
	postDeadline = 10 * time.Second
	stoppedBody  = "Stopped."
)

// HandleWebhook verifies, claims, and dispatches one AgentSessionEvent.
func (a *Adapter) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Could not read body", http.StatusBadRequest)
		return
	}
	if a.webhookVerifier != nil {
		if err := a.webhookVerifier(r, body); err != nil {
			a.logger.Warn("Linear webhook verifier rejected the request", "error", err)
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	} else if !verifySignature(a.webhookSecret, body, r.Header.Get(headerSignature)) {
		a.logger.Debug("Linear webhook signature verification failed", "bytes", len(body))
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}
	var p sessionEventPayload
	if err := json.Unmarshal(body, &p); err != nil {
		a.logger.Error("Linear webhook invalid JSON", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if !checkTimestamp(time.Now(), r.Header.Get(headerTimestamp), p.WebhookTimestamp) {
		a.logger.Warn("Linear webhook timestamp outside the replay window")
		http.Error(w, "Webhook expired", http.StatusUnauthorized)
		return
	}
	if p.Type != eventType {
		a.logger.Debug("Ignoring Linear webhook type", "type", p.Type)
		_, _ = io.WriteString(w, "ok")
		return
	}
	if p.AgentSession.AppUserID != a.BotUserID() {
		a.logger.Debug("Ignoring agent session for another app user", "appUserId", p.AgentSession.AppUserID)
		_, _ = io.WriteString(w, "ok")
		return
	}
	if issueID(p.AgentSession) == "" {
		a.logger.Debug("Ignoring agent session without an issue", "sessionId", p.AgentSession.ID)
		_, _ = io.WriteString(w, "ok")
		return
	}
	c := a.chatInstance()
	if c == nil {
		a.logger.Warn("Chat instance not initialized, ignoring agent session event")
		_, _ = io.WriteString(w, "ok")
		return
	}
	ctx := context.WithoutCancel(r.Context())
	key, ok := claimKey(p)
	if !ok {
		a.logger.Warn("Ignoring agent session event with an unknown action", "action", p.Action)
		_, _ = io.WriteString(w, "ok")
		return
	}
	var claimed bool
	st := c.State()
	if st == nil {
		a.logger.Warn("no state adapter; processing without delivery dedupe")
	} else {
		var err error
		claimed, err = st.SetIfNotExists(ctx, key, json.RawMessage("true"), claimTTL)
		if err != nil {
			a.logger.Warn("delivery claim failed; processing without dedupe", "key", key, "error", err)
		} else if !claimed {
			a.logger.Debug("Duplicate Linear delivery", "key", key)
			_, _ = io.WriteString(w, "ok")
			return
		}
	}
	if claimed {
		defer func() {
			if v := recover(); v != nil {
				_ = st.Delete(ctx, key)
				panic(v)
			}
		}()
	}
	if err := a.dispatch(ctx, c, p); err != nil {
		if st != nil {
			_ = st.Delete(ctx, key)
		}
		a.logger.Error("Linear agent session dispatch failed", "action", p.Action, "sessionId", p.AgentSession.ID, "error", err)
		http.Error(w, "Dispatch failed", http.StatusInternalServerError)
		return
	}
	_, _ = io.WriteString(w, "ok")
}

// claimKey is stable per event: session id for created, activity id for
// prompted. (webhookId is the webhook configuration id, constant across
// events; Linear-Delivery reuse on retry is undocumented.)
func claimKey(p sessionEventPayload) (string, bool) {
	switch p.Action {
	case actionCreated:
		return "linear:seen:created:" + p.AgentSession.ID, true
	case actionPrompted:
		if p.AgentActivity == nil {
			return "", false
		}
		return "linear:seen:prompted:" + p.AgentActivity.ID, true
	}
	return "", false
}

// dispatch routes one claimed event. An error means "ask Linear to retry".
func (a *Adapter) dispatch(ctx context.Context, c chat.ChatInstance, p sessionEventPayload) error {
	rm := RawMessage{Action: p.Action, Session: p.AgentSession, Activity: p.AgentActivity, PromptContext: p.PromptContext, OrganizationID: p.OrganizationID}
	if p.Action == actionPrompted {
		act := p.AgentActivity
		if act.User != nil && act.User.ID == a.BotUserID() {
			a.logger.Debug("Ignoring prompt from self", "activityId", act.ID)
			return nil
		}
		if act.Signal == signalStop {
			return a.handleStop(ctx, c, rm)
		}
		if act.Content.Type != contentPrompt {
			a.logger.Warn("Ignoring prompted event with non-prompt content", "type", act.Content.Type)
			return nil
		}
	}
	msg, threadID, err := a.message(rm)
	if err != nil {
		a.logger.Warn("Ignoring unparseable agent session event", "error", err)
		return nil
	}
	return c.ProcessMessage(ctx, chat.ProcessMessageInput{Adapter: a, Message: msg, ThreadID: threadID})
}

// handleStop cancels the thread's work (when the chat instance can) and
// closes the session with one response, as Linear's stop signal requires.
// Known race (PORTING gotcha 1): an activity already in flight from the
// cancelled turn can land after "Stopped." and reopen the session.
func (a *Adapter) handleStop(ctx context.Context, c chat.ChatInstance, rm RawMessage) error {
	threadID, err := encodeThreadID(ThreadID{IssueID: issueID(rm.Session), SessionID: rm.Session.ID})
	if err != nil {
		return err
	}
	if aborter, ok := c.(chat.TurnAborter); ok {
		if err := aborter.AbortTurn(ctx, threadID); err != nil {
			a.logger.Warn("abort turn on stop", "threadId", threadID, "error", err)
		}
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postDeadline)
	defer cancel()
	if _, err := a.createActivity(pctx, rm.Session.ID, ActivityContent{Type: typeResponse, Body: stoppedBody}, false); err != nil {
		return fmt.Errorf("post stop response: %w", err)
	}
	return nil
}
