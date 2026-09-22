// Ported from packages/chat/src/types.ts @ 6adca36 (chat v4.40.0).
// Divergences: Message lives in message.go; EmojiValue is completed in emoji.go; SuggestedPrompts
// is SlackSuggestedPromptsOptions (absent from types.ts); process* payloads flattened
// into *Input structs; TS unions use typed strings or any; StreamOptions.Signal
// omitted (callers pass ctx); PostableAst.AST is any (mdast Root); Card lives in
// cards.go; Modal lives in modals.go (children stay []any); Author.IsBot is *bool (nil = "unknown"); StartTyping is on
// Adapter (required, types.ts:577) with status string + TypingOptions; TypingNotifier
// is EndTyping only (optional, types.ts:297, AgentSessionStatus; zero = unset);
// fetchSubject is SubjectFetcher (optional, types.ts:363); FormattedContent is any
// (goldmark ast.Node or JSON); Attachment.FetchData is func() ([]byte, error).
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"time"
)

// StreamChunk is the upstream StreamChunk union.
type StreamChunk interface{ isStreamChunk() }

type MarkdownTextChunk struct{ Text string }

func (MarkdownTextChunk) isStreamChunk() {}

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskComplete   TaskStatus = "complete"
	TaskError      TaskStatus = "error"
)

type TaskUpdateChunk struct {
	ID      string
	Title   string
	Status  TaskStatus
	Details string // optional
	Output  string // optional
}

func (TaskUpdateChunk) isStreamChunk() {}

type PlanUpdateChunk struct{ Title string }

func (PlanUpdateChunk) isStreamChunk() {}

type Lock struct {
	ThreadID  string
	Token     string
	ExpiresAt time.Time
}

type QueueEntry struct {
	EnqueuedAt time.Time
	ExpiresAt  time.Time
	Message    *Message
}

type StateKV interface {
	Get(ctx context.Context, key string) (json.RawMessage, error)                        // (nil, nil) when missing
	Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error // ttl 0 = no expiry
	SetIfNotExists(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, key string) error
}

type StateLists interface {
	GetList(ctx context.Context, key string) ([]json.RawMessage, error)
	AppendToList(ctx context.Context, key string, value json.RawMessage, maxLength int, ttl time.Duration) error
}

type StateLocks interface {
	AcquireLock(ctx context.Context, threadID string, ttl time.Duration) (*Lock, error) // (nil, nil) when already held
	ExtendLock(ctx context.Context, lock *Lock, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, lock *Lock) error
	ForceReleaseLock(ctx context.Context, threadID string) error
}

type StateQueue interface {
	Enqueue(ctx context.Context, threadID string, entry QueueEntry, maxSize int) (int, error)
	Dequeue(ctx context.Context, threadID string) (*QueueEntry, error)
	QueueDepth(ctx context.Context, threadID string) (int, error)
}

type StateSubscriptions interface {
	Subscribe(ctx context.Context, threadID string) error
	Unsubscribe(ctx context.Context, threadID string) error
	IsSubscribed(ctx context.Context, threadID string) (bool, error)
}

type StateAdapter interface {
	StateKV
	StateLists
	StateLocks
	StateQueue
	StateSubscriptions
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
}

func StateGet[T any](ctx context.Context, s StateKV, key string) (T, bool, error) {
	var zero T
	raw, err := s.Get(ctx, key)
	if err != nil {
		return zero, false, err
	}
	if len(raw) == 0 {
		return zero, false, nil
	}
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, false, fmt.Errorf("unmarshal state key %q: %w", key, err)
	}
	return zero, true, nil
}

func StateSet[T any](ctx context.Context, s StateKV, key string, v T, ttl time.Duration) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal state key %q: %w", key, err)
	}
	return s.Set(ctx, key, raw, ttl)
}

type FetchDirection string

const (
	FetchBackward FetchDirection = "backward" // zero-value-adjacent default; empty string means backward
	FetchForward  FetchDirection = "forward"
)

type FetchOptions struct {
	Cursor    string
	Direction FetchDirection
	Limit     int
}

type FetchResult struct {
	Messages   []*Message
	NextCursor string
}

type ThreadSummary struct {
	ID          string
	LastReplyAt time.Time
	ReplyCount  int
	RootMessage *Message
}

type ListThreadsResult struct {
	NextCursor string
	Threads    []ThreadSummary
}

type ChannelVisibility string

const (
	ChannelPrivate   ChannelVisibility = "private"
	ChannelWorkspace ChannelVisibility = "workspace"
	ChannelExternal  ChannelVisibility = "external"
	ChannelUnknown   ChannelVisibility = "unknown"
)

type ChannelInfo struct {
	ID                string
	Name              string
	IsDM              bool
	MemberCount       int
	ChannelVisibility ChannelVisibility
	Metadata          map[string]any
}

// ThreadInfo is adapter.fetchThread's return (upstream ThreadInfo).
type ThreadInfo struct {
	ID                string
	ChannelID         string
	ChannelName       string
	ChannelVisibility ChannelVisibility
	Metadata          map[string]any
}

// UserInfo is adapter.getUser's return (upstream UserInfo). Tz is the
// IANA zone when the platform exposes one, else "".
type UserInfo struct {
	AvatarURL string
	Email     string
	FullName  string
	IsBot     bool
	Tz        string
	UserID    string
	UserName  string
}

// AdapterPostableMessage is what adapters accept for post/edit.
type AdapterPostableMessage interface{ isPostable() }

// PostableText is the TS string variant of AdapterPostableMessage.
type PostableText string

func (PostableText) isPostable() {}

type AttachmentType string

const (
	AttachmentImage AttachmentType = "image"
	AttachmentFile  AttachmentType = "file"
	AttachmentVideo AttachmentType = "video"
	AttachmentAudio AttachmentType = "audio"
)

type Attachment struct {
	Data          []byte
	FetchData     func() ([]byte, error)
	FetchMetadata map[string]string
	Height        int
	MIMEType      string
	Name          string
	Size          int
	Type          AttachmentType
	URL           string
	Width         int
}

type FileUpload struct {
	Data     []byte
	Filename string
	MIMEType string
}

type PostableRaw struct {
	Attachments []Attachment
	Files       []FileUpload
	Raw         string
}

func (PostableRaw) isPostable() {}

type PostableMarkdown struct {
	Attachments []Attachment
	Files       []FileUpload
	Markdown    string
}

func (PostableMarkdown) isPostable() {}

type PostableAst struct {
	AST         any
	Attachments []Attachment
	Files       []FileUpload
}

func (PostableAst) isPostable() {}

type PostableCard struct {
	Card         Card
	FallbackText string
	Files        []FileUpload
}

func (PostableCard) isPostable() {}

type RawMessage struct {
	ID      string
	Channel string
	Raw     any
}

// FormattedContent is message formatting. TS is mdast Root; here it's any
// (goldmark ast.Node at runtime, or the JSON object from SerializedMessage).
type FormattedContent = any

type MessageMetadata struct {
	DateSent time.Time
	Edited   bool
	EditedAt *time.Time
}

type LinkPreview struct {
	Description  string
	FetchMessage func() (*Message, error)
	ImageURL     string
	SiteName     string
	Title        string
	URL          string
}

type SubjectPerson struct {
	ID   string
	Name string
}

type MessageSubject struct {
	Assignee    *SubjectPerson
	Author      *SubjectPerson
	Description string
	ID          string
	Labels      []string
	Raw         any
	Status      string
	Title       string
	Type        string
	URL         string
}

// SubjectFetcher is adapter.fetchSubject? (types.ts:363).
type SubjectFetcher interface {
	FetchSubject(ctx context.Context, raw any) (*MessageSubject, error)
}

// EmojiValue is a comparable emoji name. String/JSON and GetEmoji live in emoji.go.
type EmojiValue struct {
	Name string
}

type Adapter interface {
	Initialize(ctx context.Context, chat ChatInstance) error
	EncodeThreadID(platformData any) (string, error)
	DecodeThreadID(threadID string) (any, error)
	ChannelIDFromThreadID(threadID string) string
	BotUserID() string
	ParseMessage(raw any) (*Message, error)
	PostMessage(ctx context.Context, threadID string, msg AdapterPostableMessage) (*RawMessage, error)
	EditMessage(ctx context.Context, threadID, messageID string, msg AdapterPostableMessage) (*RawMessage, error)
	DeleteMessage(ctx context.Context, threadID, messageID string) error
	AddReaction(ctx context.Context, threadID, messageID string, emoji EmojiValue) error
	RemoveReaction(ctx context.Context, threadID, messageID string, emoji EmojiValue) error
	FetchMessages(ctx context.Context, threadID string, opts FetchOptions) (FetchResult, error)
	StartTyping(ctx context.Context, threadID string, status string, opts TypingOptions) error
}

type EphemeralPoster interface {
	PostEphemeral(ctx context.Context, threadID, userID string, msg AdapterPostableMessage) (*RawMessage, error)
}

type TypingNotifier interface {
	EndTyping(ctx context.Context, threadID string, status AgentSessionStatus) error
}

type ObjectPoster interface {
	PostObject(ctx context.Context, threadID, kind string, data any) (*RawMessage, error)
	EditObject(ctx context.Context, threadID, messageID, kind string, data any) (*RawMessage, error)
}

type Streamer interface {
	Stream(ctx context.Context, threadID string, stream iter.Seq2[StreamChunk, error], opts StreamOptions) (*RawMessage, error)
}

type MessageScheduler interface {
	ScheduleMessage(ctx context.Context, threadID string, msg AdapterPostableMessage, postAt time.Time) (*RawMessage, error)
}

type DMOpener interface {
	OpenDM(ctx context.Context, userID string) (string, error)
}

type MessageFetcher interface {
	FetchMessage(ctx context.Context, threadID, messageID string) (*Message, error)
}

type ChannelReader interface {
	FetchChannelInfo(ctx context.Context, channelID string) (ChannelInfo, error)
	FetchChannelMessages(ctx context.Context, channelID string, opts FetchOptions) (FetchResult, error)
	ListThreads(ctx context.Context, channelID string, opts FetchOptions) (ListThreadsResult, error)
	PostChannelMessage(ctx context.Context, channelID string, msg AdapterPostableMessage) (*RawMessage, error)
}

type ModalOpener interface {
	OpenModal(ctx context.Context, triggerID string, modal *Modal) (string, error)
	UpdateModal(ctx context.Context, viewID string, modal *Modal) error
}

type AgentViewPublisher interface {
	PublishHomeView(ctx context.Context, userID string, card any) error
	SetSuggestedPrompts(ctx context.Context, threadID string, prompts SuggestedPrompts) error
	SetAssistantStatus(ctx context.Context, threadID, status string) error
	SetSessionStatus(ctx context.Context, threadID string, status AgentSessionStatus) error
	SetAssistantTitle(ctx context.Context, threadID, title string) error
}

type Disconnecter interface {
	Disconnect(ctx context.Context) error
}

// ErrorPoster posts a turn failure in the platform's failure shape (a Linear
// error activity). Consumers fall back to PostMessage when absent.
type ErrorPoster interface {
	PostError(ctx context.Context, threadID, text string) (*RawMessage, error)
}

// TurnAborter is implemented by a ChatInstance that can stop work on a
// thread: cancel the running turn and drop the turns queued behind it.
// Adapters call it when the platform signals a stop (Slack assistant stop,
// Linear's stop signal).
type TurnAborter interface {
	AbortTurn(ctx context.Context, threadID string) error
}

type StreamTaskDisplayMode string

const (
	StreamTaskTimeline StreamTaskDisplayMode = "timeline"
	StreamTaskPlan     StreamTaskDisplayMode = "plan"
)

type StreamOptions struct {
	FallbackStreamingPlaceholderText *string
	RecipientTeamID                  string
	RecipientUserID                  string
	SessionStatus                    AgentSessionStatus
	StopBlocks                       []any
	TaskDisplayMode                  StreamTaskDisplayMode
	UpdateIntervalMs                 int
}

type TypingOptions struct {
	InitiatorUserID string
}

type AgentSessionStatus string

const (
	AgentSessionActive     AgentSessionStatus = "active"
	AgentSessionClosed     AgentSessionStatus = "closed"
	AgentSessionProcessing AgentSessionStatus = "processing"
	AgentSessionSuspended  AgentSessionStatus = "suspended"
)

type SuggestedPrompt struct {
	Message string
	Title   string
}

type SuggestedPrompts struct {
	Prompts []SuggestedPrompt
	Title   string
}

type AppContextKind string

const (
	AppContextChannel AppContextKind = "channel"
	AppContextCanvas  AppContextKind = "canvas"
	AppContextList    AppContextKind = "list"
	AppContextMessage AppContextKind = "message"
	AppContextUnknown AppContextKind = "unknown"
)

type AppContextEntity struct {
	CanvasID     string
	ChannelID    string
	EnterpriseID string
	Kind         AppContextKind
	ListID       string
	MessageTs    string
	TeamID       string
	Type         string
	Value        any
}

type Author struct {
	Email    string
	FullName string
	IsBot    *bool // nil = TS "unknown"
	IsMe     bool
	IsSystem bool
	UserID   string
	UserName string
}

type AssistantThreadContext struct {
	ChannelID        string
	EnterpriseID     string
	ForceSearch      bool
	TeamID           string
	ThreadEntryPoint string
}

type ProcessMessageInput struct {
	Adapter         Adapter
	Message         *Message
	PreviousMessage *Message
	ThreadID        string
}

type ProcessReactionInput struct {
	Adapter   Adapter
	Added     bool
	Emoji     EmojiValue
	Message   *Message
	MessageID string
	Raw       any
	RawEmoji  string
	ThreadID  string
	User      Author
}

type ProcessSlashCommandInput struct {
	Adapter   Adapter
	ChannelID string
	Command   string
	Raw       any
	Text      string
	TriggerID string
	User      Author
}

type ProcessModalInput struct {
	Adapter         Adapter
	CallbackID      string
	ContextID       string
	PrivateMetadata string
	Raw             any
	User            Author
	Values          map[string]string
	ViewID          string
}

type ProcessOptionsLoadInput struct {
	ActionID string
	Adapter  Adapter
	Query    string
	Raw      any
	User     Author
}

type ProcessActionInput struct {
	ActionID  string
	Adapter   Adapter
	MessageID string
	Raw       any
	ThreadID  string
	TriggerID string
	User      Author
	Value     string
}

type ProcessMemberJoinedInput struct {
	Adapter   Adapter
	ChannelID string
	InviterID string
	UserID    string
}

type ProcessAssistantThreadInput struct {
	Adapter   Adapter
	ChannelID string
	Context   AssistantThreadContext
	ThreadID  string
	ThreadTs  string
	UserID    string
}

// ChatInstance is the chat core the adapter dispatches inbound events to.
//
// Process* implementations must return promptly — the Slack adapter awaits
// them before acking the webhook, and Slack redelivers after ~3 seconds; the
// async split belongs in the Chat core.
type ChatInstance interface {
	State() StateAdapter
	ProcessMessage(ctx context.Context, in ProcessMessageInput) error
	ProcessMessageUpdated(ctx context.Context, in ProcessMessageInput) error
	ProcessMessageDeleted(ctx context.Context, threadID, messageID string) error
	ProcessReaction(ctx context.Context, in ProcessReactionInput) error
	ProcessSlashCommand(ctx context.Context, in ProcessSlashCommandInput) error
	ProcessAction(ctx context.Context, in ProcessActionInput) error
	ProcessModalSubmit(ctx context.Context, in ProcessModalInput) (any, error)
	ProcessModalClose(ctx context.Context, in ProcessModalInput) error
	ProcessOptionsLoad(ctx context.Context, in ProcessOptionsLoadInput) (any, error)
	ProcessMemberJoinedChannel(ctx context.Context, in ProcessMemberJoinedInput) error
	ProcessAssistantThreadStarted(ctx context.Context, in ProcessAssistantThreadInput) error
	ProcessAssistantContextChanged(ctx context.Context, in ProcessAssistantThreadInput) error
}
