package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func hostInAllowlist(host string, allow []string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	for _, a := range allow {
		if strings.EqualFold(strings.TrimSpace(a), h) {
			return true
		}
	}
	return false
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 169.254.0.0/16 (metadata)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}

// FetchRemoteImage downloads an image over HTTP or HTTPS after allowlist + DNS checks.
// HTTP is supported for rehosting third-party image links; prefer HTTPS in production.
func FetchRemoteImage(ctx context.Context, rawURL string, allowHosts []string, maxBytes int64, timeout time.Duration) ([]byte, string, error) {
	if len(allowHosts) == 0 {
		return nil, "", fmt.Errorf("remote image fetch disabled (empty allowlist)")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, "", fmt.Errorf("invalid url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return nil, "", fmt.Errorf("invalid url scheme")
	}
	host := u.Hostname()
	if !hostInAllowlist(host, allowHosts) {
		return nil, "", fmt.Errorf("host not in url_fetch_hosts allowlist")
	}
	resolver := net.DefaultResolver
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, "", fmt.Errorf("dns: %w", err)
	}
	if len(addrs) == 0 {
		return nil, "", fmt.Errorf("dns: no addresses")
	}
	allowLoopback := hostInAllowlist("127.0.0.1", allowHosts) || hostInAllowlist("localhost", allowHosts)
	for _, a := range addrs {
		if isBlockedIP(a.IP) {
			if allowLoopback && a.IP.IsLoopback() {
				continue
			}
			return nil, "", fmt.Errorf("resolved to disallowed address")
		}
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			last := via[len(via)-1]
			u2, err := url.Parse(last.URL.String())
			if err != nil {
				return err
			}
			if !hostInAllowlist(u2.Hostname(), allowHosts) {
				return fmt.Errorf("redirect to disallowed host")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	lr := io.LimitReader(resp.Body, maxBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("response exceeds max_fetch_bytes")
	}
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	return body, ct, nil
}

// FetchRemoteHTTPSImage is an alias for [FetchRemoteImage] (both http and https schemes are accepted).
func FetchRemoteHTTPSImage(ctx context.Context, rawURL string, allowHosts []string, maxBytes int64, timeout time.Duration) ([]byte, string, error) {
	return FetchRemoteImage(ctx, rawURL, allowHosts, maxBytes, timeout)
}
