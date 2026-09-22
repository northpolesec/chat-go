package statememory_test

import (
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/statememory"
)

func TestStateAdapterConformance(t *testing.T) {
	t.Parallel()
	chattest.RunStateAdapterTests(t, func(t *testing.T) chat.StateAdapter {
		return statememory.New()
	})
}
