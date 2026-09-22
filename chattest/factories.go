// Test factories ported from packages/tests/src/factories.ts @ 6adca36 (chat v4.40.0)
// (partial: createMockChatInstance). See PORTING.md.
package chattest

import (
	"context"
	"sync"

	"github.com/northpolesec/chat-go/chat"
)

// MockChat is createMockChatInstance: every process* records a dispatch.
type MockChat struct {
	mu    sync.Mutex
	state chat.StateAdapter
	calls map[string]int
}

// NewMockChat returns a fresh mock. state may be nil.
func NewMockChat(state chat.StateAdapter) *MockChat {
	return &MockChat{state: state, calls: map[string]int{}}
}

func (m *MockChat) record(handler string) {
	m.mu.Lock()
	m.calls[handler]++
	m.mu.Unlock()
}

// Dispatched is toHaveDispatched: true when the named process* was called.
func (m *MockChat) Dispatched(handler string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[handler] > 0
}

// Calls is how many times handler was dispatched.
func (m *MockChat) Calls(handler string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[handler]
}

func (m *MockChat) State() chat.StateAdapter { return m.state }

func (m *MockChat) ProcessMessage(context.Context, chat.ProcessMessageInput) error {
	m.record("processMessage")
	return nil
}

func (m *MockChat) ProcessMessageUpdated(context.Context, chat.ProcessMessageInput) error {
	m.record("processMessageUpdated")
	return nil
}

func (m *MockChat) ProcessMessageDeleted(context.Context, string, string) error {
	m.record("processMessageDeleted")
	return nil
}

func (m *MockChat) ProcessReaction(context.Context, chat.ProcessReactionInput) error {
	m.record("processReaction")
	return nil
}

func (m *MockChat) ProcessSlashCommand(context.Context, chat.ProcessSlashCommandInput) error {
	m.record("processSlashCommand")
	return nil
}

func (m *MockChat) ProcessAction(context.Context, chat.ProcessActionInput) error {
	m.record("processAction")
	return nil
}

func (m *MockChat) ProcessModalSubmit(context.Context, chat.ProcessModalInput) (any, error) {
	m.record("processModalSubmit")
	return nil, nil
}

func (m *MockChat) ProcessModalClose(context.Context, chat.ProcessModalInput) error {
	m.record("processModalClose")
	return nil
}

func (m *MockChat) ProcessOptionsLoad(context.Context, chat.ProcessOptionsLoadInput) (any, error) {
	m.record("processOptionsLoad")
	return nil, nil
}

func (m *MockChat) ProcessMemberJoinedChannel(context.Context, chat.ProcessMemberJoinedInput) error {
	m.record("processMemberJoinedChannel")
	return nil
}

func (m *MockChat) ProcessAssistantThreadStarted(context.Context, chat.ProcessAssistantThreadInput) error {
	m.record("processAssistantThreadStarted")
	return nil
}

func (m *MockChat) ProcessAssistantContextChanged(context.Context, chat.ProcessAssistantThreadInput) error {
	m.record("processAssistantContextChanged")
	return nil
}

var _ chat.ChatInstance = (*MockChat)(nil)
var _ DispatchWatcher = (*MockChat)(nil)
