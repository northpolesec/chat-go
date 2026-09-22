package linear

import (
	"errors"
	"testing"

	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func TestThreadIDRoundTrip(t *testing.T) {
	t.Parallel()
	enc, err := encodeThreadID(ThreadID{IssueID: "iss-1", SessionID: "sess-1"})
	must.NoError(t, err)
	must.Eq(t, "linear:iss-1:s:sess-1", enc)
	dec, err := decodeThreadID(enc)
	must.NoError(t, err)
	must.Eq(t, ThreadID{IssueID: "iss-1", SessionID: "sess-1"}, dec)
}

func TestEncodeRequiresBothIDs(t *testing.T) {
	t.Parallel()
	_, err := encodeThreadID(ThreadID{IssueID: "iss-1"})
	var ve *shared.ValidationError
	must.True(t, errors.As(err, &ve))
}

func TestDecodeRejectsCommentsModeShapes(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"linear:iss-1", "linear:iss-1:c:c-9", "linear:iss-1:c:c-9:s:sess-1", "github:o/r:5", "linear::s:x", "garbage"} {
		_, err := decodeThreadID(id)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve), must.Sprintf("%s", id))
	}
	_, err := decodeThreadID("linear:iss-1:c:c-9")
	must.StrContains(t, err.Error(), "comments mode")
}
