// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: createAttachment fetchData, fetchSlackFile, rehydrateAttachment).
// Divergences: FetchData is func() ([]byte, error) (no ctx); token snapshot
// is the request-ctx string at parse time (`ctxToken ?? getToken()`, matching
// ALS capture); auth headers attach only when api.IsSlackAuthURL is true
// (token resolver is not invoked for non-Slack URLs); bytes go through
// shared.DownloadAttachment (SSRF policy; buffers like upstream).
package slack

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
)

const (
	installNotFoundTeam       = "Installation not found for team "
	installNotFoundEnterprise = "Installation not found for enterprise "
)

// RehydrateAttachment is adapter.rehydrateAttachment: attach fetchData that
// resolves the bot token via installationProvider / GetInstallation.
func (a *SlackAdapter) RehydrateAttachment(attachment chat.Attachment) chat.Attachment {
	fileURL := ""
	if attachment.FetchMetadata != nil {
		fileURL = attachment.FetchMetadata["url"]
	}
	if fileURL == "" {
		fileURL = attachment.URL
	}
	if fileURL == "" {
		return attachment
	}
	teamID := ""
	enterpriseID := ""
	isEnterprise := false
	if attachment.FetchMetadata != nil {
		teamID = attachment.FetchMetadata["teamId"]
		enterpriseID = attachment.FetchMetadata["enterpriseId"]
		isEnterprise = attachment.FetchMetadata["isEnterpriseInstall"] == "true"
	}
	out := attachment
	out.FetchData = func() ([]byte, error) {
		return a.fetchSlackFile(context.Background(), fileURL, func(ctx context.Context) (string, error) {
			installationID := teamID
			if isEnterprise {
				installationID = enterpriseID
			}
			if installationID == "" {
				return a.getToken(ctx)
			}
			tok, ok := a.resolveTokenForTeam(ctx, installationID, isEnterprise)
			if !ok {
				prefix := installNotFoundTeam
				if isEnterprise {
					prefix = installNotFoundEnterprise
				}
				return "", shared.NewAuthenticationError(adapterName, prefix+installationID)
			}
			return tok.token, nil
		})
	}
	return out
}

func (a *SlackAdapter) fetchSlackFile(ctx context.Context, rawURL string, token func(context.Context) (string, error)) ([]byte, error) {
	var value string
	if isSlackAuthURL(rawURL, a.apiURL) {
		tok, err := token(ctx)
		if err != nil {
			return nil, err
		}
		value = tok
	}
	opts := shared.DownloadAttachmentOptions{
		Adapter: adapterName,
		HeadersFunc: func(target *url.URL) map[string]string {
			if value != "" && isSlackAuthURL(target.String(), a.apiURL) {
				return map[string]string{"authorization": "Bearer " + value}
			}
			return nil
		},
		Transport: a.createFileTransport(),
		OnResponse: func(resp *http.Response) error {
			ct := resp.Header.Get("Content-Type")
			if strings.Contains(ct, "text/html") {
				return shared.NewNetworkError(adapterName,
					"Failed to download file from Slack: received HTML login page instead of file data. "+
						`Ensure your Slack app has the "files:read" OAuth scope. `+
						"URL: "+rawURL, nil)
			}
			return nil
		},
	}
	data, err := shared.DownloadAttachment(ctx, a.httpClient, rawURL, opts)
	if err != nil {
		var netErr *shared.NetworkError
		if errors.As(err, &netErr) {
			return nil, err
		}
		return nil, shared.NewNetworkError(adapterName, "Failed to fetch Slack file", err)
	}
	return data, nil
}

func (a *SlackAdapter) createFileTransport() shared.AttachmentTransport {
	return a.fileTransport
}
