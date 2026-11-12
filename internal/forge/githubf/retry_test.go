package githubf

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/forge"
)

func init() { retryDelay = func(int) time.Duration { return 0 } }

// flaky answers status for the first fails requests, then 200.
func flaky(fails int32, status int) (http.Handler, *atomic.Int32) {
	var n atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) <= fails {
			w.WriteHeader(status)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}), &n
}

func do(t *testing.T, srv *httptest.Server, method string) (int, error) {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(`{"query":"mutation {}"}`)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := withRetries(srv.Client()).Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func TestRetryReadsOnServerErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		h, n := flaky(2, status)
		srv := httptest.NewServer(h)
		got, err := do(t, srv, http.MethodGet)
		if err != nil || got != http.StatusOK || n.Load() != 3 {
			t.Errorf("%d: status %d, err %v, %d requests; want 200 after 3", status, got, err, n.Load())
		}
		srv.Close()
	}
	// It gives up after three attempts and returns the last answer.
	h, n := flaky(10, http.StatusServiceUnavailable)
	srv := httptest.NewServer(h)
	defer srv.Close()
	if got, err := do(t, srv, http.MethodGet); err != nil || got != http.StatusServiceUnavailable || n.Load() != 3 {
		t.Errorf("persistent 503: %d %v, %d requests", got, err, n.Load())
	}
}

func TestNoRetryForWritesOrClientErrors(t *testing.T) {
	// A POST (a mutation, a REST create) that got a 5xx may have been applied.
	h, n := flaky(1, http.StatusServiceUnavailable)
	srv := httptest.NewServer(h)
	defer srv.Close()
	if got, err := do(t, srv, http.MethodPost); err != nil || got != http.StatusServiceUnavailable || n.Load() != 1 {
		t.Errorf("POST 503 was resent: %d %v, %d requests", got, err, n.Load())
	}
	// 4xx and 501 are answers, not outages.
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusNotImplemented} {
		h, n := flaky(1, status)
		s := httptest.NewServer(h)
		if got, err := do(t, s, http.MethodGet); err != nil || got != status || n.Load() != 1 {
			t.Errorf("GET %d was retried: %d requests", status, n.Load())
		}
		s.Close()
	}
}

// failingRT fails each request with err, counting them.
type failingRT struct {
	err error
	n   int
}

func (f *failingRT) RoundTrip(*http.Request) (*http.Response, error) { f.n++; return nil, f.err }

func TestRetryTransportErrors(t *testing.T) {
	dial := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	reset := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
	for _, tc := range []struct {
		name   string
		method string
		err    error
		want   int
	}{
		{"GET reset", http.MethodGet, reset, 3},
		{"POST dial (never sent)", http.MethodPost, dial, 3},
		{"POST reset (may have been applied)", http.MethodPost, reset, 1},
	} {
		rt := &failingRT{err: tc.err}
		c := withRetries(&http.Client{Transport: rt})
		var body io.Reader
		if tc.method == http.MethodPost {
			body = strings.NewReader("{}")
		}
		req, _ := http.NewRequestWithContext(context.Background(), tc.method, "http://x.invalid/", body)
		resp, err := c.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil || rt.n != tc.want {
			t.Errorf("%s: %d attempts, want %d (err %v)", tc.name, rt.n, tc.want, err)
		}
	}
	// A cancelled context stops the retries.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt := &failingRT{err: reset}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://x.invalid/", nil)
	resp, err := withRetries(&http.Client{Transport: rt}).Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || rt.n != 1 {
		t.Errorf("cancelled: %d attempts", rt.n)
	}
}

// The driver's REST calls go through the retrying client: one 503 on the
// pull request read no longer fails it.
func TestDriverRetriesAReadOn503(t *testing.T) {
	var n atomic.Int32
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && n.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	repo := testRepo()
	_, _ = d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	if n.Load() < 2 {
		t.Errorf("the 503 was not retried (%d GETs)", n.Load())
	}
}
