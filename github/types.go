// Package github is the Go port of @chat-adapter/github v4.40.0 (upstream
// packages/adapter-github). Single-tenant GitHub App and token auth; the
// multi-tenant installation map is not ported (see PORTING.md).
package github

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
)

const adapterName = "github"

// Thread types (upstream GitHubThreadId.type; omitted upstream means "pr").
const (
	ThreadTypePR    = "pr"
	ThreadTypeIssue = "issue"
)

// ThreadID is the decoded thread id (upstream GitHubThreadId). Number is the
// issue or PR number (GitHub shares one number space). ReviewCommentID is the
// root comment of an inline review thread; 0 for conversation threads.
type ThreadID struct {
	Owner           string
	Repo            string
	Number          int
	ReviewCommentID int
	Type            string // ThreadTypePR or ThreadTypeIssue; "" decodes as PR
}

var (
	reviewCommentThreadRe = regexp.MustCompile(`^([^/]+)/([^:]+):(\d+):rc:(\d+)$`)
	issueThreadRe         = regexp.MustCompile(`^([^/]+)/([^:]+):issue:(\d+)$`)
	prThreadRe            = regexp.MustCompile(`^([^/]+)/([^:]+):(\d+)$`)
)

// encodeThreadID is upstream encodeThreadId:
//   - github:{owner}/{repo}:{n}
//   - github:{owner}/{repo}:{n}:rc:{commentId}
//   - github:{owner}/{repo}:issue:{n}
func encodeThreadID(id ThreadID) (string, error) {
	if id.Type == ThreadTypeIssue && id.ReviewCommentID != 0 {
		return "", shared.NewValidationError(adapterName, "Review comments are not supported on issue threads")
	}
	base := adapterName + ":" + id.Owner + "/" + id.Repo + ":"
	switch {
	case id.Type == ThreadTypeIssue:
		return base + "issue:" + strconv.Itoa(id.Number), nil
	case id.ReviewCommentID != 0:
		return base + strconv.Itoa(id.Number) + ":rc:" + strconv.Itoa(id.ReviewCommentID), nil
	default:
		return base + strconv.Itoa(id.Number), nil
	}
}

// decodeThreadID is upstream decodeThreadId. Type is always set on return.
func decodeThreadID(threadID string) (ThreadID, error) {
	const prefix = adapterName + ":"
	if len(threadID) <= len(prefix) || threadID[:len(prefix)] != prefix {
		return ThreadID{}, shared.NewValidationError(adapterName, fmt.Sprintf("Invalid GitHub thread ID: %s", threadID))
	}
	rest := threadID[len(prefix):]
	if m := reviewCommentThreadRe.FindStringSubmatch(rest); m != nil {
		return ThreadID{Owner: m[1], Repo: m[2], Number: atoi(m[3]), ReviewCommentID: atoi(m[4]), Type: ThreadTypePR}, nil
	}
	if m := issueThreadRe.FindStringSubmatch(rest); m != nil {
		return ThreadID{Owner: m[1], Repo: m[2], Number: atoi(m[3]), Type: ThreadTypeIssue}, nil
	}
	if m := prThreadRe.FindStringSubmatch(rest); m != nil {
		return ThreadID{Owner: m[1], Repo: m[2], Number: atoi(m[3]), Type: ThreadTypePR}, nil
	}
	return ThreadID{}, shared.NewValidationError(adapterName, fmt.Sprintf("Invalid GitHub thread ID format: %s", threadID))
}

// atoi is for regexp-matched \d+ groups only.
func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// User is the GitHub user object subset the adapter reads.
type User struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Type      string `json:"type"` // "User", "Bot", "Organization"
	AvatarURL string `json:"avatar_url"`
	Name      string `json:"name"`  // GET /user/{id} only
	Email     string `json:"email"` // GET /user/{id} only
}

// Repository is the repository object subset the adapter reads.
type Repository struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	Owner           User   `json:"owner"`
	Description     string `json:"description"`
	Visibility      string `json:"visibility"`
	DefaultBranch   string `json:"default_branch"`
	OpenIssuesCount int    `json:"open_issues_count"`
}

// Comment is one struct for both upstream GitHubIssueComment and
// GitHubReviewComment (Go divergence from the TS union): the review-only
// fields are zero on an issue comment. AuthorAssociation is not in the
// upstream types; callers use it to tell how the author relates to the repo.
type Comment struct {
	ID                int64     `json:"id"`
	Body              string    `json:"body"`
	User              User      `json:"user"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	HTMLURL           string    `json:"html_url"`
	AuthorAssociation string    `json:"author_association"` // OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR, NONE
	// Review comments only.
	InReplyToID int64  `json:"in_reply_to_id"`
	Path        string `json:"path"`
	Line        int    `json:"line"`
	DiffHunk    string `json:"diff_hunk"`
	CommitID    string `json:"commit_id"`
	Side        string `json:"side"`
}

// Label is a label object subset.
type Label struct {
	Name string `json:"name"`
}

// Ref is a PR head/base subset.
type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// Issue is the issue / pull request object subset the adapter reads. A PR
// fetched from /issues has PullRequest set; one fetched from /pulls has
// Head and Base.
type Issue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	HTMLURL     string    `json:"html_url"`
	User        *User     `json:"user"`
	Assignees   []User    `json:"assignees"`
	Labels      []Label   `json:"labels"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	Head *Ref `json:"head"`
	Base *Ref `json:"base"`
}

// Reaction is a reaction object subset.
type Reaction struct {
	ID      int64  `json:"id"`
	Content string `json:"content"`
	User    User   `json:"user"`
}

// issueCommentPayload is the issue_comment webhook body subset.
type issueCommentPayload struct {
	Action     string     `json:"action"`
	Comment    Comment    `json:"comment"`
	Issue      Issue      `json:"issue"`
	Repository Repository `json:"repository"`
	Sender     User       `json:"sender"`
}

// reviewCommentPayload is the pull_request_review_comment webhook body subset.
type reviewCommentPayload struct {
	Action      string     `json:"action"`
	Comment     Comment    `json:"comment"`
	PullRequest Issue      `json:"pull_request"`
	Repository  Repository `json:"repository"`
	Sender      User       `json:"sender"`
}

// RawMessage is chat.Message.Raw and chat.RawMessage.Raw for GitHub
// (upstream GitHubRawMessage). Type is "issue_comment" or "review_comment".
// ThreadType is ThreadTypePR or ThreadTypeIssue for issue comments.
type RawMessage struct {
	Type       string     `json:"type"`
	Comment    Comment    `json:"comment"`
	Repository Repository `json:"repository"`
	Number     int        `json:"prNumber"`
	ThreadType string     `json:"threadType,omitempty"`
}

const (
	rawIssueComment  = "issue_comment"
	rawReviewComment = "review_comment"
)
