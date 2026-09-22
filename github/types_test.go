package github

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestEncodeThreadID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		id   ThreadID
		want string
	}{
		{"pr-level", ThreadID{Owner: "vercel", Repo: "chat", Number: 123}, "github:vercel/chat:123"},
		{"review comment", ThreadID{Owner: "vercel", Repo: "chat", Number: 123, ReviewCommentID: 456}, "github:vercel/chat:123:rc:456"},
		{"issue", ThreadID{Owner: "vercel", Repo: "chat", Number: 7, Type: ThreadTypeIssue}, "github:vercel/chat:issue:7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := encodeThreadID(tc.id)
			must.NoError(t, err)
			must.Eq(t, tc.want, got)
		})
	}
}

func TestEncodeThreadIDRejectsReviewCommentOnIssue(t *testing.T) {
	t.Parallel()
	_, err := encodeThreadID(ThreadID{Owner: "o", Repo: "r", Number: 1, Type: ThreadTypeIssue, ReviewCommentID: 9})
	must.ErrorContains(t, err, "Review comments are not supported on issue threads")
}

func TestDecodeThreadID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want ThreadID
	}{
		{"github:vercel/chat:123", ThreadID{Owner: "vercel", Repo: "chat", Number: 123, Type: ThreadTypePR}},
		{"github:vercel/chat:123:rc:456", ThreadID{Owner: "vercel", Repo: "chat", Number: 123, Type: ThreadTypePR, ReviewCommentID: 456}},
		{"github:vercel/chat:issue:7", ThreadID{Owner: "vercel", Repo: "chat", Number: 7, Type: ThreadTypeIssue}},
		{"github:my-org/my.repo:42", ThreadID{Owner: "my-org", Repo: "my.repo", Number: 42, Type: ThreadTypePR}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := decodeThreadID(tc.in)
			must.NoError(t, err)
			must.Eq(t, tc.want, got)
		})
	}
}

func TestDecodeThreadIDRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"slack:C1:1.2", "github:", "github:vercel:123", "github:vercel/chat:abc", "github:vercel/chat:123:rc:x"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := decodeThreadID(in)
			must.ErrorContains(t, err, "Invalid GitHub thread ID")
		})
	}
}
