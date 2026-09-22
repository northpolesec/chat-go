// Ported from packages/adapter-shared/src/download.ts @ 6adca36 (chat v4.40.0).
// Divergences: DownloadAttachment takes (ctx, client HTTPDoer, url, opts);
// hops are issued manually (Go's Client follows redirects itself — per-hop
// host/allowlist/header policy cannot live in CheckRedirect alone). A nil
// client uses a package-owned *http.Client (never http.DefaultClient) with
// CheckRedirect=ErrUseLastResponse and DNS pinning via CreateResolver.
// AttachmentTransport still replaces the built-in hop (upstream transport).
// Timeout is ctx + opts.Timeout (default 30s). Hostnames are lowercased and
// WHATWG IPv4-canonicalized because net/url does neither (Node URL does both).
// upstream advertises+decodes br; this port advertises only what it decodes
// ("gzip, deflate") so a CDN cannot send brotli we reject. Do not omit
// Accept-Encoding (that would enable Transport auto-gzip and double-decode).
// NetworkError lives in errors.go (adapter-shared/errors.ts). CreateResolver
// is ctx-aware.
package shared

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLimit     = 25 * 1024 * 1024
	defaultRedirects = 5
	defaultTimeout   = 30 * time.Second
	userAgent        = "Vercel.ChatSDK"
	acceptEncoding   = "gzip, deflate"
)

var redirectStatuses = map[int]struct{}{
	301: {},
	302: {},
	303: {},
	307: {},
	308: {},
}

var (
	blocked4 = mustCIDRs(
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.88.99.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
	)
	blocked6 = mustCIDRs(
		"::/3",
		"2001::/23",
		"2001:db8::/32",
		"2002::/16",
		"3fff::/20",
		"4000::/2",
		"8000::/1",
	)
)

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, len(cidrs))
	for i, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out[i] = n
	}
	return out
}

// HTTPDoer is satisfied by *http.Client.
// Implementations must NOT follow redirects internally (return 3xx as the final
// response); a redirect-following Doer bypasses the per-hop SSRF validation.
// Only *http.Client instances are automatically made non-following.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

var _ HTTPDoer = (*http.Client)(nil)

// AttachmentTransport issues one hop. ctx carries the overall deadline.
type AttachmentTransport func(ctx context.Context, u *url.URL, headers map[string]string) (*http.Response, error)

// DownloadAttachmentOptions is the upstream DownloadAttachmentOptions.
type DownloadAttachmentOptions struct {
	Adapter     string
	Headers     map[string]string
	HeadersFunc func(*url.URL) map[string]string
	Hosts       []string
	Limit       int
	OnResponse  func(*http.Response) error
	Redirects   *int
	Timeout     time.Duration
	Transport   AttachmentTransport
}

// LookupAddress is one DNS result (Node dns.LookupAddress).
type LookupAddress struct {
	Address string
	Family  int
}

// ResolverQuery looks up every address for hostname.
type ResolverQuery func(ctx context.Context, hostname string) ([]LookupAddress, error)

// Resolver is the guarded lookup returned by CreateResolver.
type Resolver func(ctx context.Context, hostname string) ([]LookupAddress, error)

func networkError(adapter, msg string, cause error) *NetworkError {
	return NewNetworkError(adapter, msg, cause)
}

func refusal(adapter string) *NetworkError {
	return networkError(adapter, "Refusing to fetch an internal attachment URL", nil)
}

// CreateResolver returns a lookup that refuses empty or internal results.
func CreateResolver(adapter string, query ResolverQuery) Resolver {
	if query == nil {
		query = defaultDNSQuery
	}
	return func(ctx context.Context, hostname string) ([]LookupAddress, error) {
		addresses, err := query(ctx, hostname)
		if err != nil {
			return nil, err
		}
		if len(addresses) == 0 {
			return nil, networkError(adapter, "Could not resolve the attachment host", nil)
		}
		for _, a := range addresses {
			if blockedAddress(a.Address, a.Family) {
				return nil, refusal(adapter)
			}
		}
		return addresses, nil
	}
}

func defaultDNSQuery(ctx context.Context, hostname string) ([]LookupAddress, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, err
	}
	out := make([]LookupAddress, len(ips))
	for i, ip := range ips {
		family := 6
		addr := ip.IP.String()
		if v4 := ip.IP.To4(); v4 != nil {
			family = 4
			addr = v4.String()
		}
		out[i] = LookupAddress{Address: addr, Family: family}
	}
	return out, nil
}

// ValidateAttachmentURL parses and applies the SSRF host policy.
func ValidateAttachmentURL(value, adapter string, hosts []string) (*url.URL, error) {
	u, err := parseAttachmentURL(value)
	if err != nil {
		return nil, err
	}
	return validateParsed(u, adapter, hosts)
}

func parseAttachmentURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil {
		return nil, err
	}
	return normalizeParsed(u)
}

func normalizeParsed(u *url.URL) (*url.URL, error) {
	host := u.Hostname()
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	switch {
	case host == "":
	case net.ParseIP(host) != nil:
		host = net.ParseIP(host).String()
	default:
		if ip, ok := parseIPv4Host(host); ok {
			host = ip.String()
		} else if isIPv4Shaped(host) {
			return nil, fmt.Errorf("invalid attachment url")
		} else {
			host = strings.ToLower(host)
		}
	}
	out := *u
	port := u.Port()
	switch {
	case port != "":
		out.Host = net.JoinHostPort(host, port)
	case isBareIPv6(host):
		out.Host = "[" + host + "]"
	default:
		out.Host = host
	}
	return &out, nil
}

func isBareIPv6(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

func validateParsed(u *url.URL, adapter string, hosts []string) (*url.URL, error) {
	hostname := u.Hostname()
	hostname = strings.TrimPrefix(hostname, "[")
	hostname = strings.TrimSuffix(hostname, "]")
	if ip := net.ParseIP(hostname); ip != nil {
		family := 6
		if ip.To4() != nil {
			family = 4
		}
		if blockedAddress(ip.String(), family) {
			return nil, refusal(adapter)
		}
	}
	if u.Scheme != "https" || (hosts != nil && !hostAllowed(hostname, hosts)) {
		return nil, networkError(adapter, "Refusing to fetch an untrusted attachment URL", nil)
	}
	return u, nil
}

func hostAllowed(hostname string, hosts []string) bool {
	hostname = strings.ToLower(hostname)
	for _, host := range hosts {
		allowed := strings.ToLower(host)
		if hostname == allowed || strings.HasSuffix(hostname, "."+allowed) {
			return true
		}
	}
	return false
}

func blockedAddress(address string, family int) bool {
	if family != 4 && family != 6 {
		return true
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return true
	}
	if family == 4 {
		ip = ip.To4()
		if ip == nil {
			return true
		}
		return ipIn(ip, blocked4)
	}
	if ip.To4() != nil {
		ip = ip.To16()
	}
	return ipIn(ip, blocked6)
}

func ipIn(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func parseIPv4Number(input string) (uint64, bool) {
	if input == "" {
		return 0, false
	}
	base := 10
	digits := input
	if len(input) >= 2 && input[0] == '0' && (input[1] == 'x' || input[1] == 'X') {
		base = 16
		digits = input[2:]
	} else if len(input) >= 2 && input[0] == '0' {
		base = 8
		digits = input[1:]
	}
	if digits == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func ipv4Parts(host string) []string {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func isIPv4Shaped(host string) bool {
	parts := ipv4Parts(host)
	if len(parts) == 0 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if _, ok := parseIPv4Number(p); !ok {
			return false
		}
	}
	return true
}

func parseIPv4Host(host string) (net.IP, bool) {
	parts := ipv4Parts(host)
	if len(parts) == 0 || len(parts) > 4 {
		return nil, false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := parseIPv4Number(p)
		if !ok {
			return nil, false
		}
		nums[i] = n
	}
	for i := 0; i < len(nums)-1; i++ {
		if nums[i] > 255 {
			return nil, false
		}
	}
	n := len(nums)
	maxLast := uint64(1)<<(8*uint(5-n)) - 1
	if nums[n-1] > maxLast {
		return nil, false
	}
	ipv4 := nums[n-1]
	for i := 0; i < n-1; i++ {
		ipv4 += nums[i] << (8 * uint(3-i))
	}
	return net.IPv4(byte(ipv4>>24), byte(ipv4>>16), byte(ipv4>>8), byte(ipv4)).To4(), true
}

// ReadAttachmentBody reads a response body with the upstream size/encoding rules.
func ReadAttachmentBody(resp *http.Response, adapter string, limit int) ([]byte, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if resp == nil {
		return nil, networkError(adapter, "Failed to fetch file: 0", nil)
	}
	defer drainClose(resp)

	header := strings.TrimSpace(strings.ToLower(resp.Header.Get("Content-Encoding")))
	if header == "" {
		header = "identity"
	}
	var wrap func(io.Reader) (io.ReadCloser, error)
	switch header {
	case "identity":
	case "gzip", "x-gzip":
		wrap = func(r io.Reader) (io.ReadCloser, error) { return gzip.NewReader(r) }
	case "deflate":
		wrap = func(r io.Reader) (io.ReadCloser, error) { return flate.NewReader(r), nil }
	default:
		return nil, networkError(adapter, "Unsupported attachment encoding: "+header, nil)
	}

	declared := int64(-1)
	if wrap == nil {
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			n, err := strconv.ParseInt(cl, 10, 64)
			if err == nil {
				declared = n
			}
		}
	}
	if declared >= 0 && declared > int64(limit) {
		return nil, networkError(adapter, "Attachment exceeds the download limit", nil)
	}

	src := io.Reader(resp.Body)
	if wrap != nil {
		decoded, err := wrap(resp.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = decoded.Close() }()
		src = decoded
	}

	if declared >= 0 {
		body, err := io.ReadAll(io.LimitReader(src, declared+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > declared {
			return nil, networkError(adapter, "Attachment body exceeds its declared length", nil)
		}
		return body, nil
	}

	body, err := io.ReadAll(io.LimitReader(src, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, networkError(adapter, "Attachment exceeds the download limit", nil)
	}
	return body, nil
}

// DownloadAttachment downloads an untrusted attachment URL with SSRF protection.
func DownloadAttachment(ctx context.Context, client HTTPDoer, value string, opts DownloadAttachmentOptions) ([]byte, error) {
	adapter := opts.Adapter
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	redirects := defaultRedirects
	if opts.Redirects != nil {
		redirects = *opts.Redirects
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	send := opts.Transport
	if send == nil {
		send = httpDoerTransport(client, adapter)
	}

	u, err := ValidateAttachmentURL(value, adapter, opts.Hosts)
	if err != nil {
		return nil, wrapTimeout(ctx, adapter, err)
	}

	body, err := followHops(ctx, send, u, opts, adapter, limit, redirects)
	if err != nil {
		return nil, wrapTimeout(ctx, adapter, err)
	}
	return body, nil
}

func followHops(ctx context.Context, send AttachmentTransport, u *url.URL, opts DownloadAttachmentOptions, adapter string, limit, redirects int) ([]byte, error) {
	for hop := 0; hop <= redirects; hop++ {
		headers := map[string]string{
			"accept-encoding": acceptEncoding,
			"user-agent":      userAgent,
		}
		extra := opts.Headers
		if opts.HeadersFunc != nil {
			extra = opts.HeadersFunc(u)
		}
		maps.Copy(headers, extra)

		resp, err := send(ctx, u, headers)
		if err != nil {
			return nil, err
		}
		status := resp.StatusCode
		if _, ok := redirectStatuses[status]; ok {
			loc := resp.Header.Get("Location")
			drainClose(resp)
			if loc == "" {
				return nil, networkError(adapter, "Attachment redirect has no location", nil)
			}
			if hop == redirects {
				return nil, networkError(adapter, "Too many attachment redirects", nil)
			}
			next, err := resolveAndValidate(loc, u, adapter, opts.Hosts)
			if err != nil {
				return nil, err
			}
			u = next
			continue
		}
		if status < 200 || status >= 300 {
			reason := statusReason(resp, status)
			drainClose(resp)
			return nil, networkError(adapter, strings.TrimSpace(fmt.Sprintf("Failed to fetch file: %d %s", status, reason)), nil)
		}
		if opts.OnResponse != nil {
			if err := opts.OnResponse(resp); err != nil {
				drainClose(resp)
				return nil, err
			}
		}
		return ReadAttachmentBody(resp, adapter, limit)
	}
	return nil, networkError(adapter, "Too many attachment redirects", nil)
}

func resolveAndValidate(location string, base *url.URL, adapter string, hosts []string) (*url.URL, error) {
	rel, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeParsed(base.ResolveReference(rel))
	if err != nil {
		return nil, err
	}
	return validateParsed(normalized, adapter, hosts)
}

func statusReason(resp *http.Response, status int) string {
	if resp != nil && resp.Status != "" {
		if reason, ok := strings.CutPrefix(resp.Status, strconv.Itoa(status)+" "); ok {
			return reason
		}
	}
	return http.StatusText(status)
}

func wrapTimeout(ctx context.Context, adapter string, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	var netErr *NetworkError
	if errors.As(err, &netErr) {
		return err
	}
	return networkError(adapter, "Timed out fetching the attachment", err)
}

func httpDoerTransport(client HTTPDoer, adapter string) AttachmentTransport {
	if client == nil {
		client = newPinnedClient(adapter)
	} else {
		client = withoutRedirects(client)
	}
	return func(ctx context.Context, u *url.URL, headers map[string]string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return client.Do(req)
	}
}

func withoutRedirects(client HTTPDoer) HTTPDoer {
	c, ok := client.(*http.Client)
	if !ok {
		return client
	}
	clone := *c
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func newPinnedClient(adapter string) *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:           pinnedDialContext(adapter),
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func pinnedDialContext(adapter string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	resolve := CreateResolver(adapter, nil)
	d := &net.Dialer{}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			family := 6
			if ip.To4() != nil {
				family = 4
			}
			if blockedAddress(ip.String(), family) {
				return nil, refusal(adapter)
			}
			return d.DialContext(ctx, network, addr)
		}
		addrs, err := resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		return d.DialContext(ctx, network, net.JoinHostPort(addrs[0].Address, port))
	}
}

func drainClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
