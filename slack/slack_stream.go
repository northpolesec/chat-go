// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: Stream — native start/append/stop, segment rotation, fallback).
// Divergences: chatStream helper → chat.startStream/appendStream/stopStream
// via api.Client.Call (JSON, chunks payloads as @slack/web-api ChatStreamer
// sends them); Date.now → now();
// nativeStreamingBroken is atomic.Bool (no mutex across network); JS
// streamSegmentMaxAgeMs Infinity → time.Duration(math.MaxInt64) (no NaN);
// context.Canceled returns unwrapped; ChatStreamer 256-char buffer is
// streamBufferSize (tests that mocked flush-every-append set it to 1).
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
)

var _ chat.Streamer = (*SlackAdapter)(nil)

const (
	nativeStreamBufferSize     = 256
	streamSegmentRotationGrace = 30 * time.Second
	// streamFinalizeTimeout bounds the stop-stream calls made after the
	// caller's ctx was canceled (see Stream).
	streamFinalizeTimeout       = 10 * time.Second
	streamExpiredError          = "message_not_in_streaming_state"
	methodStartStream           = "chat.startStream"
	methodAppendStream          = "chat.appendStream"
	methodStopStream            = "chat.stopStream"
	defaultFallbackUpdateMillis = 1000
)

var nativeStreamingUnsupported = map[string]struct{}{
	"feature_not_enabled": {},
	"method_deprecated":   {},
	"unknown_method":      {},
}

var (
	fenceLinePattern      = regexp.MustCompile(`^ {0,3}(` + "`{3,}" + `|~{3,})(.*)$`)
	tableRowPattern       = regexp.MustCompile(`^\|.*\|$`)
	tableSeparatorPattern = regexp.MustCompile(`^\|[\s:]*-+[\s:]*(?:\|[\s:]*-+[\s:]*)*\|$`)
)

type openFence struct {
	marker  string
	opening string
}

type fenceTracker struct {
	open *openFence
}

func (t *fenceTracker) feed(line string) bool {
	match := fenceLinePattern.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	marker, info := match[1], match[2]
	if t.open != nil {
		if marker[0] == t.open.marker[0] && len(marker) >= len(t.open.marker) && strings.TrimSpace(info) == "" {
			t.open = nil
			return true
		}
		return false
	}
	if marker[0] == '`' && strings.Contains(info, "`") {
		return false
	}
	t.open = &openFence{marker: marker, opening: strings.TrimRight(line, "\n")}
	return true
}

func openFenceIn(text string) *openFence {
	var tracker fenceTracker
	for line := range strings.SplitSeq(text, "\n") {
		tracker.feed(line)
	}
	return tracker.open
}

func closesFence(fence *openFence, line string) bool {
	tracker := fenceTracker{open: &openFence{marker: fence.marker, opening: fence.opening}}
	return tracker.feed(line)
}

func segmentCutIndex(text string) int {
	if i := strings.LastIndex(text, "\n\n"); i != -1 {
		return i + 2
	}
	return strings.LastIndex(text, "\n") + 1
}

func tableContinuation(text string) string {
	if !strings.HasSuffix(text, "\n") {
		return ""
	}
	lines := strings.Split(text, "\n")
	lines = lines[:len(lines)-1]
	var rows []string
	for _, line := range slices.Backward(lines) {
		if !tableRowPattern.MatchString(strings.TrimSpace(line)) {
			break
		}
		rows = append(rows, line)
	}
	slices.Reverse(rows)
	separatorAt := -1
	for i, row := range rows {
		if tableSeparatorPattern.MatchString(strings.TrimSpace(row)) {
			separatorAt = i
			break
		}
	}
	if separatorAt < 1 {
		return ""
	}
	return rows[separatorAt-1] + "\n" + rows[separatorAt] + "\n"
}

// BuildFeedbackButtonsBlock is upstream buildFeedbackButtonsBlock.
func BuildFeedbackButtonsBlock(opts *FeedbackButtonsOptions) map[string]any {
	if opts == nil {
		opts = &FeedbackButtonsOptions{}
	}
	actionID := opts.ActionID
	if actionID == "" {
		actionID = "message_feedback"
	}
	posLabel := opts.PositiveLabel
	if posLabel == "" {
		posLabel = "Good response"
	}
	posVal := opts.PositiveValue
	if posVal == "" {
		posVal = "positive"
	}
	negLabel := opts.NegativeLabel
	if negLabel == "" {
		negLabel = "Bad response"
	}
	negVal := opts.NegativeValue
	if negVal == "" {
		negVal = "negative"
	}
	return map[string]any{
		"type": "context_actions",
		"elements": []any{
			map[string]any{
				"type":      "feedback_buttons",
				"action_id": actionID,
				"positive_button": map[string]any{
					"text":  map[string]any{"type": "plain_text", "text": posLabel},
					"value": posVal,
				},
				"negative_button": map[string]any{
					"text":  map[string]any{"type": "plain_text", "text": negLabel},
					"value": negVal,
				},
			},
		},
	}
}

func slackPlatformErrorCode(err error) string {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func (a *SlackAdapter) clockNow() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *SlackAdapter) nativeBufferSize() int {
	if a.streamBufferSize > 0 {
		return a.streamBufferSize
	}
	return nativeStreamBufferSize
}

type nativeStreamer struct {
	a       *SlackAdapter
	ctx     context.Context
	channel string
	thread  string
	recipU  string
	recipT  string
	taskDM  string
	buffer  string
	ts      string
}

type streamerAppend struct {
	markdownText string
	chunks       []any
	hasChunks    bool
}

type streamerStop struct {
	markdownText  string
	blocks        []any
	sessionStatus string
}

func (a *SlackAdapter) newNativeStreamer(ctx context.Context, channel, threadTS string, opts chat.StreamOptions) *nativeStreamer {
	return &nativeStreamer{
		a:       a,
		ctx:     ctx,
		channel: channel,
		thread:  threadTS,
		recipU:  opts.RecipientUserID,
		recipT:  opts.RecipientTeamID,
		taskDM:  string(opts.TaskDisplayMode),
	}
}

func (s *nativeStreamer) startBody() map[string]any {
	body := map[string]any{"channel": s.channel, "thread_ts": s.thread}
	if s.recipU != "" {
		body["recipient_user_id"] = s.recipU
	}
	if s.recipT != "" {
		body["recipient_team_id"] = s.recipT
	}
	if s.taskDM != "" {
		body["task_display_mode"] = s.taskDM
	}
	return body
}

func (s *nativeStreamer) call(method string, body map[string]any) (api.Response, error) {
	resp, err := s.a.apiClient().Call(s.ctx, method, body, api.EncodingJSON)
	if err != nil {
		return resp, err
	}
	if !resp.OK {
		code := resp.Error
		if code == "" {
			code = "unknown_error"
		}
		return resp, &api.APIError{Code: code}
	}
	return resp, nil
}

func tsFrom(resp api.Response) string {
	var payload struct {
		TS string `json:"ts"`
	}
	_ = json.Unmarshal(resp.Raw, &payload)
	return payload.TS
}

func (s *nativeStreamer) flush(chunks []any) (bool, error) {
	var body map[string]any
	method := methodAppendStream
	if s.ts == "" {
		method = methodStartStream
		body = s.startBody()
	} else {
		body = map[string]any{"channel": s.channel, "ts": s.ts}
	}
	if out := s.takeChunks(chunks); len(out) > 0 {
		body["chunks"] = out
	}
	resp, err := s.call(method, body)
	if err != nil {
		return false, err
	}
	if s.ts == "" {
		s.ts = tsFrom(resp)
	}
	return true, nil
}

// takeChunks drains the text buffer into a markdown_text chunk ahead of
// chunks. Every start/append/stop call sends `chunks`, never a bare
// `markdown_text`: Slack pins a stream to whichever it was started with
// (streaming_mode_mismatch otherwise), and a task card can arrive at any
// point, so chunks is the only mode that works for every turn.
func (s *nativeStreamer) takeChunks(chunks []any) []any {
	var out []any
	if s.buffer != "" {
		out = append(out, map[string]any{"type": "markdown_text", "text": s.buffer})
		s.buffer = ""
	}
	return append(out, chunks...)
}

func (s *nativeStreamer) append(args streamerAppend) (bool, error) {
	if args.markdownText != "" {
		s.buffer += args.markdownText
	}
	if len(s.buffer) >= s.a.nativeBufferSize() || args.hasChunks {
		return s.flush(args.chunks)
	}
	return false, nil
}

func (s *nativeStreamer) stop(args streamerStop) (string, error) {
	if args.markdownText != "" {
		s.buffer += args.markdownText
	}
	if s.ts == "" {
		resp, err := s.call(methodStartStream, s.startBody())
		if err != nil {
			return "", err
		}
		s.ts = tsFrom(resp)
		if s.ts == "" {
			return "", errors.New("failed to stop stream: stream not started")
		}
	}
	body := map[string]any{"channel": s.channel, "ts": s.ts}
	if out := s.takeChunks(nil); len(out) > 0 {
		body["chunks"] = out
	}
	if len(args.blocks) > 0 {
		body["blocks"] = args.blocks
	}
	if args.sessionStatus != "" {
		body["session_status"] = args.sessionStatus
	}
	resp, err := s.call(methodStopStream, body)
	if err != nil {
		return s.ts, err
	}
	if ts := tsFrom(resp); ts != "" {
		return ts, nil
	}
	return s.ts, nil
}

var unknownStructuredOnce sync.Once

func slackStructuredChunk(logger *slog.Logger, chunk chat.StreamChunk) map[string]any {
	switch v := chunk.(type) {
	case chat.PlanUpdateChunk:
		return map[string]any{"type": "plan_update", "title": v.Title}
	case chat.TaskUpdateChunk:
		m := map[string]any{
			"type":   "task_update",
			"id":     v.ID,
			"title":  v.Title,
			"status": string(v.Status),
		}
		if v.Details != "" {
			m["details"] = v.Details
		}
		if v.Output != "" {
			m["output"] = v.Output
		}
		return m
	case chat.MarkdownTextChunk:
		return map[string]any{"type": "markdown_text", "text": v.Text}
	default:
		unknownStructuredOnce.Do(func() {
			if logger == nil {
				logger = slog.New(slog.DiscardHandler)
			}
			logger.Debug("Slack: unknown structured stream chunk skipped")
		})
		return nil
	}
}

type streamSession struct {
	a        *SlackAdapter
	ctx      context.Context
	threadID string
	channel  string
	threadTS string
	opts     chat.StreamOptions

	streamer        *nativeStreamer
	startedAt       time.Time
	hasStarted      bool
	flushNextAppend bool
	reopenFence     string
	tableHeader     string
	rotations       int

	lastAppended       string
	lastFlushed        string
	renderer           *chat.StreamingMarkdownRenderer
	resolvedCommitted  string
	resolvedSourceDone int
	fences             fenceTracker

	fallbackMode     bool
	fallbackMessage  *chat.RawMessage
	nativeRendered   bool
	fallbackSent     string
	lastFallbackEdit time.Time
	updateInterval   time.Duration

	structuredOK bool
	openTasks    []chat.TaskUpdateChunk
	currentPlan  *chat.PlanUpdateChunk
}

func (s *streamSession) createStreamer() *nativeStreamer {
	return s.a.newNativeStreamer(s.ctx, s.channel, s.threadTS, s.opts)
}

func (s *streamSession) markSegmentStarted() {
	if !s.hasStarted {
		s.startedAt = s.a.clockNow()
		s.hasStarted = true
	}
	s.flushNextAppend = false
	s.nativeRendered = true
}

func (s *streamSession) segmentAge() time.Duration {
	if !s.hasStarted {
		return 0
	}
	return s.a.clockNow().Sub(s.startedAt)
}

func (s *streamSession) rotationDue(atBlockBoundary bool) bool {
	if !s.hasStarted {
		return false
	}
	age := s.segmentAge()
	if age < s.a.streamSegmentMaxAge {
		return false
	}
	return atBlockBoundary || age >= s.a.streamSegmentMaxAge+streamSegmentRotationGrace
}

func (s *streamSession) rememberStructured(chunk chat.StreamChunk) {
	switch v := chunk.(type) {
	case chat.PlanUpdateChunk:
		c := v
		s.currentPlan = &c
	case chat.TaskUpdateChunk:
		if v.Status == chat.TaskComplete || v.Status == chat.TaskError {
			kept := s.openTasks[:0]
			for _, t := range s.openTasks {
				if t.ID != v.ID {
					kept = append(kept, t)
				}
			}
			s.openTasks = kept
			return
		}
		for i, t := range s.openTasks {
			if t.ID == v.ID {
				s.openTasks[i] = v
				return
			}
		}
		s.openTasks = append(s.openTasks, v)
	}
}

func (s *streamSession) disableStructured(chunkType string, err error) {
	s.structuredOK = false
	s.a.logger.Warn(
		"Structured streaming chunk failed, falling back to text-only streaming. "+
			"Ensure your Slack app manifest includes the agent/assistant feature "+
			"and the assistant:write scope",
		"chunkType", chunkType,
		"error", err,
	)
}

func (s *streamSession) startNextSegment(sent string) error {
	s.streamer = s.createStreamer()
	s.hasStarted = false
	s.startedAt = time.Time{}
	s.flushNextAppend = true
	fence := openFenceIn(sent)
	if fence != nil {
		s.reopenFence = fence.opening + "\n"
		s.tableHeader = ""
	} else {
		s.reopenFence = ""
		s.tableHeader = tableContinuation(sent)
	}
	s.rotations++
	var replay []any
	if s.currentPlan != nil {
		if m := slackStructuredChunk(s.a.logger, *s.currentPlan); m != nil {
			replay = append(replay, m)
		}
	}
	for _, t := range s.openTasks {
		if m := slackStructuredChunk(s.a.logger, t); m != nil {
			replay = append(replay, m)
		}
	}
	if len(replay) > 0 && s.structuredOK {
		flushed, err := s.streamer.append(streamerAppend{chunks: replay, hasChunks: true})
		if err != nil {
			s.disableStructured("replay", err)
			return nil
		}
		if flushed {
			s.markSegmentStarted()
		}
	}
	return nil
}

func (s *streamSession) withSegmentPrefix(text string) string {
	firstLine, _, _ := strings.Cut(text, "\n")
	header := ""
	if tableRowPattern.MatchString(strings.TrimSpace(firstLine)) {
		header = s.tableHeader
	}
	prefixed := s.reopenFence + header + text
	s.reopenFence = ""
	s.tableHeader = ""
	return prefixed
}

func (s *streamSession) rotateSegment(delta string) (string, error) {
	cutAt := segmentCutIndex(delta)
	head := delta[:cutAt]
	tail := delta[cutAt:]
	sent := s.lastAppended + head
	fence := openFenceIn(sent)
	if fence != nil && strings.HasSuffix(sent, "\n") && closesFence(fence, tail) {
		head += tail
		sent += tail
		tail = ""
		fence = nil
	}
	closer := ""
	if fence != nil {
		if !strings.HasSuffix(sent, "\n") {
			closer = "\n"
		}
		closer += fence.marker
	}
	finalText := ""
	if len(head) > 0 {
		finalText = s.withSegmentPrefix(head)
	}
	finalText += closer
	ageMs := s.segmentAge().Milliseconds()
	sessionStatus := ""
	if s.a.agentView {
		// chat.stopStream defaults to "active"; the reply continues in the next segment.
		sessionStatus = string(chat.AgentSessionProcessing)
	}
	_, err := s.streamer.stop(streamerStop{markdownText: finalText, sessionStatus: sessionStatus})
	if err != nil {
		if slackPlatformErrorCode(err) != streamExpiredError {
			return "", err
		}
		s.a.logger.Warn(
			"Slack: stream segment expired before rotation, continuing in a new message",
			"channel", s.channel,
			"ageMs", ageMs,
		)
		tail = s.lastAppended[len(s.lastFlushed):] + delta
		sent = s.lastFlushed
	} else {
		s.lastFlushed = sent
		s.a.logger.Debug("Slack: rotated stream segment", "channel", s.channel, "ageMs", ageMs)
	}
	if err := s.startNextSegment(sent); err != nil {
		return "", err
	}
	return tail, nil
}

func (s *streamSession) sendDelta(delta string, atBlockBoundary bool) error {
	text := delta
	if s.rotationDue(atBlockBoundary || strings.Contains(delta, "\n\n")) {
		rotated, err := s.rotateSegment(delta)
		if err != nil {
			return err
		}
		text = rotated
	}
	if len(text) > 0 {
		flushed, err := s.streamer.append(streamerAppend{
			markdownText: s.withSegmentPrefix(text),
			hasChunks:    s.flushNextAppend,
			chunks:       []any{},
		})
		if err != nil {
			return err
		}
		if flushed {
			s.markSegmentStarted()
			s.lastFlushed = s.resolvedCommitted
		}
	}
	s.lastAppended = s.resolvedCommitted
	return nil
}

func (s *streamSession) flushFallback(force bool) error {
	committable := s.renderer.CommittableText()
	if committable == "" || committable == s.fallbackSent {
		return nil
	}
	now := s.a.clockNow()
	if !force && now.Sub(s.lastFallbackEdit) < s.updateInterval {
		return nil
	}
	msg := chat.PostableMarkdown{Markdown: committable}
	var err error
	if s.fallbackMessage != nil {
		_, err = s.a.EditMessage(s.ctx, s.threadID, s.fallbackMessage.ID, msg)
	} else {
		s.fallbackMessage, err = s.a.PostMessage(s.ctx, s.threadID, msg)
	}
	if err != nil {
		return err
	}
	s.fallbackSent = committable
	s.lastFallbackEdit = now
	return nil
}

func (s *streamSession) switchToFallback(err error) {
	s.fallbackMode = true
	if code := slackPlatformErrorCode(err); code != "" {
		if _, ok := nativeStreamingUnsupported[code]; ok {
			s.a.nativeBroken.Store(true)
		}
	}
	s.a.logger.Warn("Slack native streaming unavailable, falling back to post-and-edit",
		"channel", s.channel, "error", err)
}

func (s *streamSession) resolveCommitted(committable string) error {
	for s.resolvedSourceDone < len(committable) {
		lineStart := strings.LastIndex(committable[:s.resolvedSourceDone], "\n") + 1
		if s.resolvedSourceDone == 0 {
			lineStart = 0
		}
		newlineAt := strings.IndexByte(committable[s.resolvedSourceDone:], '\n')
		lineEnd := len(committable)
		if newlineAt != -1 {
			lineEnd = s.resolvedSourceDone + newlineAt + 1
		}
		piece := committable[s.resolvedSourceDone:lineEnd]
		lineEndNoNL := lineEnd
		if newlineAt != -1 {
			lineEndNoNL = s.resolvedSourceDone + newlineAt
		}
		line := committable[lineStart:lineEndNoNL]
		if s.fences.open != nil || fenceLinePattern.MatchString(line) {
			s.resolvedCommitted += piece
		} else {
			resolved, err := s.a.resolveOutgoingMentions(s.ctx, piece, s.threadID)
			if err != nil {
				return err
			}
			s.resolvedCommitted += resolved
		}
		if newlineAt != -1 {
			s.fences.feed(line)
		}
		s.resolvedSourceDone = lineEnd
	}
	return nil
}

func (s *streamSession) flushCommitted(force bool) error {
	if s.fallbackMode {
		return s.flushFallback(force)
	}
	if err := s.resolveCommitted(s.renderer.CommittableText()); err != nil {
		return err
	}
	delta := s.resolvedCommitted[len(s.lastAppended):]
	if delta == "" {
		return nil
	}
	if err := s.sendDelta(delta, false); err != nil {
		if s.nativeRendered {
			return err
		}
		s.switchToFallback(err)
		return s.flushFallback(force)
	}
	return nil
}

func (s *streamSession) sendStructuredChunk(chunk chat.StreamChunk) error {
	if err := s.flushCommitted(false); err != nil {
		return err
	}
	if s.fallbackMode || !s.structuredOK {
		chunkType := "structured"
		switch chunk.(type) {
		case chat.PlanUpdateChunk:
			chunkType = "plan_update"
		case chat.TaskUpdateChunk:
			chunkType = "task_update"
		}
		s.a.logger.Debug("Slack: structured chunk skipped", "chunkType", chunkType, "mode", fallbackModeName(s.fallbackMode))
		return nil
	}
	if err := s.sendDelta("", true); err != nil {
		return err
	}
	payload := slackStructuredChunk(s.a.logger, chunk)
	if payload == nil {
		return nil
	}
	flushed, err := s.streamer.append(streamerAppend{
		chunks:    []any{payload},
		hasChunks: true,
	})
	if err != nil {
		s.disableStructured(structuredType(chunk), err)
		return nil
	}
	if flushed {
		s.markSegmentStarted()
	}
	s.lastFlushed = s.lastAppended
	s.rememberStructured(chunk)
	return nil
}

func fallbackModeName(fallback bool) string {
	if fallback {
		return "fallback"
	}
	return "native"
}

func structuredType(chunk chat.StreamChunk) string {
	switch chunk.(type) {
	case chat.PlanUpdateChunk:
		return "plan_update"
	case chat.TaskUpdateChunk:
		return "task_update"
	default:
		return "markdown_text"
	}
}

func (s *streamSession) finishNative() (*chat.RawMessage, error) {
	stopBlocks := append([]any{}, s.opts.StopBlocks...)
	if s.a.feedbackButtons != nil {
		stopBlocks = append(stopBlocks, BuildFeedbackButtonsBlock(s.a.feedbackButtons))
	}
	sessionStatus := ""
	if s.a.agentView {
		status := s.opts.SessionStatus
		if status == "" {
			status = chat.AgentSessionActive
		}
		sessionStatus = string(status)
	}
	ts, err := s.streamer.stop(streamerStop{blocks: stopBlocks, sessionStatus: sessionStatus})
	if err != nil {
		if !s.nativeRendered {
			s.switchToFallback(err)
			if ferr := s.flushFallback(true); ferr != nil {
				return nil, ferr
			}
			s.a.logger.Debug("Slack: fallback stream complete", "messageId", idOf(s.fallbackMessage))
			_ = s.a.EndTyping(s.ctx, s.threadID, endStatus(s.opts))
			return s.fallbackMessage, nil
		}
		if slackPlatformErrorCode(err) != streamExpiredError {
			return nil, err
		}
		unconfirmed := s.lastAppended[len(s.lastFlushed):]
		if unconfirmed == "" {
			expiredTS := s.streamer.ts
			if expiredTS == "" {
				return nil, err
			}
			s.a.logger.Warn("Slack: stream expired before stop, stream-end blocks skipped",
				"channel", s.channel, "messageId", expiredTS, "skippedBlocks", len(stopBlocks))
			_ = s.a.EndTyping(s.ctx, s.threadID, endStatus(s.opts))
			return &chat.RawMessage{ID: expiredTS, Channel: s.threadID, Raw: map[string]any{"ts": expiredTS}}, nil
		}
		s.a.logger.Warn("Slack: stream expired before stop, delivering the rest in a new message",
			"channel", s.channel)
		if err := s.startNextSegment(s.lastFlushed); err != nil {
			return nil, err
		}
		ts, err = s.streamer.stop(streamerStop{
			markdownText:  s.withSegmentPrefix(unconfirmed),
			blocks:        stopBlocks,
			sessionStatus: sessionStatus,
		})
		if err != nil {
			return nil, err
		}
	}
	s.a.logger.Debug("Slack: stream complete", "messageId", ts, "segments", s.rotations+1)
	return &chat.RawMessage{ID: ts, Channel: s.threadID, Raw: map[string]any{"ts": ts}}, nil
}

func idOf(m *chat.RawMessage) string {
	if m == nil {
		return ""
	}
	return m.ID
}

func endStatus(opts chat.StreamOptions) chat.AgentSessionStatus {
	if opts.SessionStatus != "" {
		return opts.SessionStatus
	}
	return chat.AgentSessionActive
}

// Stream implements chat.Streamer.
func (a *SlackAdapter) Stream(ctx context.Context, threadID string, stream iter.Seq2[chat.StreamChunk, error], opts chat.StreamOptions) (*chat.RawMessage, error) {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	threadTS := id.ThreadTS
	if threadTS == "" {
		a.logger.Debug("Slack: using fallback stream - no thread context")
		return nil, nil
	}
	if !strings.HasPrefix(id.Channel, "D") && (opts.RecipientUserID == "" || opts.RecipientTeamID == "") {
		a.logger.Debug("Slack: using fallback stream - no recipient context")
		return nil, nil
	}
	if a.disableNativeStreaming || a.nativeBroken.Load() {
		a.logger.Debug("Slack: using fallback stream - native streaming disabled",
			"configured", !a.disableNativeStreaming, "broken", a.nativeBroken.Load())
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, unwrapCanceled(err)
	}
	if _, err := a.getToken(ctx); err != nil {
		return nil, unwrapCanceled(err)
	}
	a.logger.Debug("Slack: starting stream", "channel", id.Channel, "threadTs", threadTS)

	interval := time.Duration(opts.UpdateIntervalMs) * time.Millisecond
	if opts.UpdateIntervalMs == 0 {
		interval = defaultFallbackUpdateMillis * time.Millisecond
	}
	s := &streamSession{
		a:              a,
		ctx:            ctx,
		threadID:       threadID,
		channel:        id.Channel,
		threadTS:       threadTS,
		opts:           opts,
		streamer:       a.newNativeStreamer(ctx, id.Channel, threadTS, opts),
		renderer:       chat.NewStreamingMarkdownRenderer(chat.StreamingMarkdownOptions{DisableTableWrapForAppend: true}),
		structuredOK:   true,
		updateInterval: interval,
	}

	var streamErr error
	for chunk, err := range stream {
		if err != nil {
			streamErr = err
			break
		}
		if ctx.Err() != nil { // TS: `if (options?.signal?.aborted) break`
			break
		}
		switch v := chunk.(type) {
		case chat.MarkdownTextChunk:
			s.renderer.Push(v.Text)
			err = s.flushCommitted(false)
		default:
			err = s.sendStructuredChunk(chunk)
		}
		if err != nil {
			if ctx.Err() != nil { // canceled mid-call: finalize below
				break
			}
			return nil, unwrapCanceled(err)
		}
	}

	s.renderer.Finish()
	if ctx.Err() != nil || errors.Is(streamErr, context.Canceled) {
		// The turn was stopped (Slack's stop button, a deadline). The TS
		// adapter's fetches ignore the abort signal, so its stop() still
		// runs; here every call carried ctx, so finalize under a detached,
		// bounded one. Otherwise the message never leaves the streaming
		// state and every open task card spins forever. Slack has no
		// "cancelled" task status: an interrupted task ends as error.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), streamFinalizeTimeout)
		defer cancel()
		s.ctx, s.streamer.ctx = fctx, fctx
		for _, t := range slices.Clone(s.openTasks) { // sendStructuredChunk edits openTasks in place
			t.Status = chat.TaskError
			if err := s.sendStructuredChunk(t); err != nil {
				return nil, unwrapCanceled(err)
			}
		}
	}
	if err := s.flushCommitted(true); err != nil {
		return nil, unwrapCanceled(err)
	}

	if s.fallbackMode {
		if len(opts.StopBlocks) > 0 || a.feedbackButtons != nil {
			a.logger.Warn("Slack: stream-end blocks (stopBlocks/feedbackButtons) skipped - post-and-edit fallback cannot attach stream blocks",
				"channel", id.Channel)
		}
		a.logger.Debug("Slack: fallback stream complete", "messageId", idOf(s.fallbackMessage))
		_ = a.EndTyping(s.ctx, threadID, endStatus(opts))
		if streamErr != nil {
			return s.fallbackMessage, unwrapCanceled(streamErr)
		}
		return s.fallbackMessage, nil
	}

	msg, err := s.finishNative()
	if err != nil {
		return nil, unwrapCanceled(err)
	}
	if streamErr != nil {
		return msg, unwrapCanceled(streamErr)
	}
	if err := ctx.Err(); err != nil {
		return msg, unwrapCanceled(err)
	}
	return msg, nil
}

func unwrapCanceled(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}
