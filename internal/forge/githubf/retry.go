package githubf

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// retryTransport resends requests that cannot do anything twice, the same
// rule as the GitLab driver's checkRetry (Vizzini ISSUE-031):
//
//   - GET and HEAD, on a 5xx other than 501 or a transport error: a read has
//     no side effects;
//   - any method, on an error raised before a connection existed (dial,
//     DNS), when nothing reached the server.
//
// A POST (a REST create, a GraphQL mutation) that got a 5xx or lost its
// connection mid-way is never resent: GitHub may already have acted on it.
// Seen live: a GitHub Actions job failed on one 503 to GET /pulls/N.
type retryTransport struct {
	base     http.RoundTripper
	attempts int
}

// retryDelay is the wait before the n-th retry (1-based). Tests set it to
// zero.
var retryDelay = func(n int) time.Duration { return time.Duration(n) * time.Second }

// RoundTrip sends req, resending it under the rule above, up to t.attempts
// times in all.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	idempotent := req.Method == http.MethodGet || req.Method == http.MethodHead
	for attempt := 1; ; attempt++ {
		resp, err := t.base.RoundTrip(req)
		retry := false
		switch {
		case err != nil:
			retry = failedBeforeSending(err) || (idempotent && req.Context().Err() == nil)
		case idempotent && resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented:
			retry = true
		}
		if !retry || attempt >= t.attempts || (req.Body != nil && req.GetBody == nil) {
			return resp, err
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
		}
		if req.GetBody != nil {
			body, berr := req.GetBody()
			if berr != nil {
				return nil, berr
			}
			req.Body = body
		}
		if werr := wait(req.Context(), retryDelay(attempt)); werr != nil {
			return nil, werr
		}
	}
}

func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// failedBeforeSending reports whether err happened while connecting, before
// any byte of the request went out.
func failedBeforeSending(err error) bool {
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return !dnsErr.IsNotFound
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		return opErr.Op == "dial"
	}
	return false
}

// withRetries returns a client like c (nil: http.DefaultClient) whose
// transport retries as retryTransport does.
func withRetries(c *http.Client) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}
	out := *c
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	out.Transport = &retryTransport{base: base, attempts: 3}
	return &out
}
