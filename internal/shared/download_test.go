package shared

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func TestGuardedAttachmentDownloads(t *testing.T) {
	t.Parallel()

	t.Run("accepts public HTTPS host files.example.com", func(t *testing.T) {
		t.Parallel()
		u, err := ValidateAttachmentURL("https://files.example.com/file", "test", nil)
		must.NoError(t, err)
		must.Eq(t, "files.example.com", u.Hostname())
	})

	t.Run("accepts public HTTPS host contoso.sharepoint.com", func(t *testing.T) {
		t.Parallel()
		u, err := ValidateAttachmentURL("https://contoso.sharepoint.com/file", "test", nil)
		must.NoError(t, err)
		must.Eq(t, "contoso.sharepoint.com", u.Hostname())
	})

	t.Run("accepts public HTTPS host CONTOSO.SHAREPOINT.COM", func(t *testing.T) {
		t.Parallel()
		u, err := ValidateAttachmentURL("https://CONTOSO.SHAREPOINT.COM/file", "test", nil)
		must.NoError(t, err)
		must.Eq(t, "contoso.sharepoint.com", u.Hostname())
	})

	t.Run("accepts public HTTPS host cdn.example.net:8443", func(t *testing.T) {
		t.Parallel()
		u, err := ValidateAttachmentURL("https://cdn.example.net:8443/file", "test", nil)
		must.NoError(t, err)
		must.Eq(t, "cdn.example.net", u.Hostname())
		must.Eq(t, "8443", u.Port())
	})

	for _, raw := range []string{
		"http://files.example.com/file",
		"ftp://files.example.com/file",
		"file:///etc/passwd",
	} {
		t.Run("rejects non-HTTPS URL "+raw, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateAttachmentURL(raw, "test", nil)
			must.ErrorContains(t, err, "Refusing to fetch an untrusted attachment URL")
		})
	}

	for _, raw := range []string{
		"https://127.0.0.1/file",
		"https://2130706433/file",
		"https://[::1]/file",
		"https://169.254.169.254/file",
		"https://10.0.0.1/file",
	} {
		t.Run("rejects internal file URL "+raw+" with a network error", func(t *testing.T) {
			t.Parallel()
			_, err := ValidateAttachmentURL(raw, "test", nil)
			var netErr *NetworkError
			must.True(t, errors.As(err, &netErr))
			must.ErrorContains(t, err, "Refusing to fetch an internal attachment URL")
		})
	}

	t.Run("returns the validated DNS results to the socket", func(t *testing.T) {
		t.Parallel()
		addresses := []LookupAddress{
			{Address: "93.184.216.34", Family: 4},
			{Address: "2606:2800:220:1:248:1893:25c8:1946", Family: 6},
		}
		var gotHost string
		query := func(_ context.Context, hostname string) ([]LookupAddress, error) {
			gotHost = hostname
			return addresses, nil
		}
		guarded := CreateResolver("test", query)
		got, err := guarded(t.Context(), "files.example.com")
		must.NoError(t, err)
		must.Eq(t, "files.example.com", gotHost)
		must.Eq(t, addresses, got)
	})

	t.Run("rejects mixed public and internal DNS results", func(t *testing.T) {
		t.Parallel()
		guarded := CreateResolver("test", func(context.Context, string) ([]LookupAddress, error) {
			return []LookupAddress{
				{Address: "93.184.216.34", Family: 4},
				{Address: "10.0.0.1", Family: 4},
			}, nil
		})
		_, err := guarded(t.Context(), "files.example.com")
		must.ErrorContains(t, err, "Refusing to fetch an internal attachment URL")
	})

	t.Run("reports an empty DNS result as a resolution failure, not a refusal", func(t *testing.T) {
		t.Parallel()
		guarded := CreateResolver("test", func(context.Context, string) ([]LookupAddress, error) {
			return []LookupAddress{}, nil
		})
		_, err := guarded(t.Context(), "gone.example.com")
		must.ErrorContains(t, err, "Could not resolve the attachment host")
	})

	t.Run("rejects hostnames that resolve to internal addresses", func(t *testing.T) {
		t.Parallel()
		guarded := CreateResolver("test", nil)
		_, err := guarded(t.Context(), "localhost")
		must.ErrorContains(t, err, "Refusing to fetch an internal attachment URL")
	})

	for _, raw := range []string{
		"https://fbsbx.com/file",
		"https://cdn.fbsbx.com/file",
		"https://SContent.XX.FBCDN.NET/file",
	} {
		t.Run("accepts allowlisted host URL "+raw, func(t *testing.T) {
			t.Parallel()
			u, err := ValidateAttachmentURL(raw, "test", []string{"fbsbx.com", "FBCDN.net"})
			must.NoError(t, err)
			must.True(t, u != nil)
		})
	}

	for _, raw := range []string{
		"https://example.com/file",
		"https://fbsbx.com.attacker.example/file",
		"https://cdn.fbsbx.com./file",
	} {
		t.Run("rejects off-allowlist URL "+raw, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateAttachmentURL(raw, "test", []string{"fbsbx.com", "fbcdn.net"})
			must.ErrorContains(t, err, "Refusing to fetch an untrusted attachment URL")
		})
	}

	t.Run("applies the host allowlist to redirect targets", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Location", "https://example.com/file")
			w.WriteHeader(http.StatusFound)
		})
		_, err := DownloadAttachment(t.Context(), nil, "https://cdn.fbsbx.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Hosts:     []string{"fbsbx.com"},
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Refusing to fetch an untrusted attachment URL")
		must.Eq(t, int32(1), calls.Load())
	})

	t.Run("follows redirects between allowlisted hosts", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.Header().Set("Location", "https://scontent.xx.fbcdn.net/file")
				w.WriteHeader(http.StatusFound)
				return
			}
			_, _ = w.Write([]byte("media"))
		})
		got, err := DownloadAttachment(t.Context(), nil, "https://lookaside.fbsbx.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Hosts:     []string{"fbsbx.com", "fbcdn.net"},
			Transport: rewriteTransport(srv),
		})
		must.NoError(t, err)
		must.Eq(t, []byte("media"), got)
	})

	t.Run("resolves headers per hop and drops credentials on redirects", func(t *testing.T) {
		t.Parallel()
		var hops []map[string]string
		srv := hopServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/file" && r.Host == "files.example.com" {
				w.Header().Set("Location", "https://cdn.example.net/file")
				w.WriteHeader(http.StatusFound)
				return
			}
			_, _ = w.Write([]byte("file contents"))
		})
		inner := rewriteTransport(srv)
		transport := func(ctx context.Context, u *url.URL, headers map[string]string) (*http.Response, error) {
			copied := mapsClone(headers)
			hops = append(hops, copied)
			return inner(ctx, u, headers)
		}
		got, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter: "test",
			HeadersFunc: func(u *url.URL) map[string]string {
				if u.Hostname() == "files.example.com" {
					return map[string]string{"authorization": "Bearer secret"}
				}
				return nil
			},
			Transport: transport,
		})
		must.NoError(t, err)
		must.Eq(t, []byte("file contents"), got)
		must.Eq(t, 2, len(hops))
		must.Eq(t, "Bearer secret", hops[0]["authorization"])
		must.Eq(t, "Vercel.ChatSDK", hops[0]["user-agent"])
		_, hasAuth := hops[1]["authorization"]
		must.False(t, hasAuth)
		must.Eq(t, "Vercel.ChatSDK", hops[1]["user-agent"])
	})

	t.Run("rejects responses that fail the onResponse check", func(t *testing.T) {
		t.Parallel()
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>sign in</html>"))
		})
		_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter: "test",
			OnResponse: func(resp *http.Response) error {
				if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
					return networkError("test", "Unexpected HTML response", nil)
				}
				return nil
			},
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Unexpected HTML response")
	})

	t.Run("rejects redirects to internal addresses", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Location", "https://169.254.169.254/latest/meta-data")
			w.WriteHeader(http.StatusFound)
		})
		_, err := DownloadAttachment(t.Context(), nil, "https://contoso.sharepoint.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Refusing to fetch an internal attachment URL")
		must.Eq(t, int32(1), calls.Load())
	})

	t.Run("follows redirects to other public HTTPS hosts", func(t *testing.T) {
		t.Parallel()
		var last *url.URL
		srv := hopServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Host == "contoso.sharepoint.com" {
				w.Header().Set("Location", "https://cdn.example.net/file")
				w.WriteHeader(http.StatusFound)
				return
			}
			_, _ = w.Write([]byte("file contents"))
		})
		inner := rewriteTransport(srv)
		transport := func(ctx context.Context, u *url.URL, headers map[string]string) (*http.Response, error) {
			cloned := *u
			last = &cloned
			return inner(ctx, u, headers)
		}
		got, err := DownloadAttachment(t.Context(), nil, "https://contoso.sharepoint.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Transport: transport,
		})
		must.NoError(t, err)
		must.Eq(t, []byte("file contents"), got)
		must.Eq(t, "https://cdn.example.net/file", last.String())
	})

	t.Run("rejects redirect chains past the redirect limit", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Location", "https://cdn.example.net/file")
			w.WriteHeader(http.StatusFound)
		})
		redirects := 1
		_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Redirects: &redirects,
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Too many attachment redirects")
		must.Eq(t, int32(2), calls.Load())
	})

	t.Run("rejects redirects without a location header", func(t *testing.T) {
		t.Parallel()
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusFound)
		})
		_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Attachment redirect has no location")
	})

	t.Run("rejects error statuses", func(t *testing.T) {
		t.Parallel()
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "", http.StatusNotFound)
		})
		_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Failed to fetch file: 404 Not Found")
	})

	t.Run("times out slow downloads with a distinct error", func(t *testing.T) {
		t.Parallel()
		transport := func(ctx context.Context, _ *url.URL, _ map[string]string) (*http.Response, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Timeout:   20 * time.Millisecond,
			Transport: transport,
		})
		must.ErrorContains(t, err, "Timed out fetching the attachment")
	})

	t.Run("decodes gzip response bodies", func(t *testing.T) {
		t.Parallel()
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gzipBytes([]byte("file contents")))
		})
		got, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Transport: rewriteTransport(srv),
		})
		must.NoError(t, err)
		must.Eq(t, []byte("file contents"), got)
	})

	t.Run("applies the download limit to decompressed bytes", func(t *testing.T) {
		t.Parallel()
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gzipBytes(make([]byte, 64*1024)))
		})
		_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Limit:     1024,
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Attachment exceeds the download limit")
	})

	t.Run("rejects unsupported content encodings", func(t *testing.T) {
		t.Parallel()
		srv := hopServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "zstd")
			_, _ = w.Write([]byte("payload"))
		})
		_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
			Adapter:   "test",
			Transport: rewriteTransport(srv),
		})
		must.ErrorContains(t, err, "Unsupported attachment encoding: zstd")
	})

	t.Run("stops reading attachments at the download limit", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			Header: make(http.Header),
			Body:   io.NopCloser(io.MultiReader(bytes.NewReader([]byte("abc")), bytes.NewReader([]byte("def")))),
		}
		_, err := ReadAttachmentBody(resp, "test", 5)
		must.ErrorContains(t, err, "Attachment exceeds the download limit")
	})

	t.Run("rejects declared sizes over the limit before reading", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			Header: http.Header{"Content-Length": []string{"10"}},
			Body:   io.NopCloser(bytes.NewReader([]byte("abcdef"))),
		}
		_, err := ReadAttachmentBody(resp, "test", 5)
		must.ErrorContains(t, err, "Attachment exceeds the download limit")
	})

	t.Run("reads declared-size bodies into a single buffer", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			Header: http.Header{"Content-Length": []string{"6"}},
			Body:   io.NopCloser(io.MultiReader(bytes.NewReader([]byte("abc")), bytes.NewReader([]byte("def")))),
		}
		got, err := ReadAttachmentBody(resp, "test", 0)
		must.NoError(t, err)
		must.Eq(t, []byte("abcdef"), got)
	})

	t.Run("rejects bodies that exceed their declared length", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			Header: http.Header{"Content-Length": []string{"4"}},
			Body:   io.NopCloser(bytes.NewReader([]byte("abcdef"))),
		}
		_, err := ReadAttachmentBody(resp, "test", 0)
		must.ErrorContains(t, err, "Attachment body exceeds its declared length")
	})
}

// Go divergence (not an upstream it): this port advertises only encodings it
// can decode. Upstream sends "gzip, deflate, br" and decodes brotli.
func TestDownloadAdvertisesOnlyDecodableEncodings(t *testing.T) {
	t.Parallel()
	var got string
	srv := hopServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Accept-Encoding")
		_, _ = w.Write([]byte("ok"))
	})
	_, err := DownloadAttachment(t.Context(), nil, "https://files.example.com/file", DownloadAttachmentOptions{
		Adapter:   "test",
		Transport: rewriteTransport(srv),
	})
	must.NoError(t, err)
	must.Eq(t, "gzip, deflate", got)
}

func hopServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func rewriteTransport(srv *httptest.Server) AttachmentTransport {
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return func(ctx context.Context, u *url.URL, headers map[string]string) (*http.Response, error) {
		dest, err := url.Parse(srv.URL)
		if err != nil {
			return nil, err
		}
		dest.Path = u.Path
		dest.RawQuery = u.RawQuery
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dest.String(), nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Host = u.Host
		return client.Do(req)
	}
}

func gzipBytes(p []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(p)
	_ = zw.Close()
	return buf.Bytes()
}

func mapsClone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
