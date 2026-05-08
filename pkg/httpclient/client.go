package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/logger"
)

const (
	defaultMaxRetries  = 3
	defaultMaxBodySize = 64 << 20 // 64 MiB
)

// Client wraps http.Client with retry and logging.
type Client struct {
	inner      *http.Client
	maxRetries int
	maxBody    int64
	log        *slog.Logger
}

// Option configures a Client.
type Option func(*Client)

// WithMaxRetries sets the maximum number of retry attempts (default 3, max on 429/502/503/504).
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithMaxBodyBytes sets the maximum response body size (default 64 MiB).
func WithMaxBodyBytes(n int64) Option { return func(c *Client) { c.maxBody = n } }

// WithTimeout sets the HTTP client timeout. For per-request deadlines, pass a
// context with a deadline to Do instead.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.inner.Timeout = d }
}

// New creates a Client with sensible defaults.
func New(opts ...Option) *Client {
	c := &Client{
		inner:      &http.Client{Timeout: 0}, // context deadline used instead
		maxRetries: defaultMaxRetries,
		maxBody:    defaultMaxBodySize,
		log:        slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Do executes the request, retrying on transient upstream errors.
// The caller must close the body of the returned *http.Response when not nil.
//
// The body argument is read-to-completion for every attempt; callers should
// supply a []byte body (via DoJSON or DoRaw) rather than an io.Reader so that
// retries receive the full body.
func (c *Client) Do(ctx context.Context, method, url string, headers map[string]string, body []byte) (*http.Response, []byte, error) {
	log := logger.FromContext(ctx)

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			sleep := backoff(attempt)
			log.Debug("httpclient retry", "attempt", attempt, "sleep_ms", sleep.Milliseconds(), "url", url)
			select {
			case <-ctx.Done():
				return nil, nil, pkgerrors.Wrap(http.StatusGatewayTimeout, pkgerrors.TypeUpstream, "context_cancelled", "request cancelled before retry", ctx.Err())
			case <-time.After(sleep):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, nil, pkgerrors.Wrap(http.StatusInternalServerError, pkgerrors.TypeInternal, "build_request_failed", "could not build HTTP request", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		log.Debug("httpclient request", "method", method, "url", url, "attempt", attempt)
		resp, err := c.inner.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, pkgerrors.UpstreamTimeout()
			}
			lastErr = err
			continue // network error — retry
		}

		rawBody, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxBody))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, nil, pkgerrors.Wrap(http.StatusBadGateway, pkgerrors.TypeUpstream, "read_error", "failed to read upstream response body", readErr)
		}

		log.Debug("httpclient response", "status", resp.StatusCode, "bytes", len(rawBody))

		// Retry on transient upstream errors only when idempotent (GET/POST for generations
		// with an idempotency key is handled at a higher layer).
		if shouldRetry(resp.StatusCode) && attempt < c.maxRetries {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil {
					select {
					case <-ctx.Done():
						return nil, nil, pkgerrors.UpstreamTimeout()
					case <-time.After(time.Duration(secs) * time.Second):
					}
				}
			}
			lastErr = fmt.Errorf("upstream returned %d", resp.StatusCode)
			continue
		}

		return resp, rawBody, nil
	}

	if lastErr != nil {
		return nil, nil, pkgerrors.UpstreamErr("exhausted_retries", "upstream unavailable after retries", lastErr)
	}
	return nil, nil, pkgerrors.UpstreamErr("exhausted_retries", "upstream unavailable after retries", nil)
}

func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

// backoff returns a jittered exponential wait duration for attempt n (1-based).
func backoff(n int) time.Duration {
	base := time.Duration(1<<uint(n-1)) * 500 * time.Millisecond
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	jitter := time.Duration(rand.Int64N(int64(base / 2)))
	return base + jitter
}
