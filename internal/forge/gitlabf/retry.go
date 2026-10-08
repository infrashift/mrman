package gitlabf

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// checkRetry decides whether client-go resends a request. Its default policy
// retries a 5xx response for any method, but a 5xx from a proxy in front of
// GitLab (a 502 on gitlab.com, say) can arrive after GitLab already created
// the discussion or note, so resending a POST posts it twice. This keeps the
// retries that cannot duplicate anything:
//
//   - 429: the server refused the request without acting on it;
//   - 5xx, only for GET and HEAD, which have no side effects;
//   - transport errors raised before a connection existed (dial and DNS),
//     when nothing reached the server.
func checkRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return failedBeforeSending(err), nil
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return true, nil
	case resp.StatusCode == 0 || (resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented):
		return resp.Request != nil && (resp.Request.Method == http.MethodGet || resp.Request.Method == http.MethodHead), nil
	}
	return false, nil
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
