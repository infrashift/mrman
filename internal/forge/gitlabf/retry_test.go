package gitlabf

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestCheckRetry(t *testing.T) {
	cases := []struct {
		name   string
		method string
		status int   // 0 with err set: no response
		err    error // a transport error
		want   bool
	}{
		{"GET 502", http.MethodGet, http.StatusBadGateway, nil, true},
		{"POST 502 may already have posted", http.MethodPost, http.StatusBadGateway, nil, false},
		{"PUT 503", http.MethodPut, http.StatusServiceUnavailable, nil, false},
		{"POST 429 was refused unprocessed", http.MethodPost, http.StatusTooManyRequests, nil, true},
		{"GET 501", http.MethodGet, http.StatusNotImplemented, nil, false},
		{"GET 404", http.MethodGet, http.StatusNotFound, nil, false},
		{"dial error", "", 0, &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{"read error after sending", "", 0, &net.OpError{Op: "read", Err: errors.New("reset")}, false},
		{"NXDOMAIN", "", 0, &net.DNSError{IsNotFound: true}, false},
		{"DNS timeout", "", 0, &net.DNSError{IsTimeout: true}, true},
	}
	for _, tc := range cases {
		var resp *http.Response
		if tc.err == nil {
			resp = &http.Response{StatusCode: tc.status, Request: &http.Request{Method: tc.method}, Body: http.NoBody}
		}
		got, err := checkRetry(context.Background(), resp, tc.err)
		if err != nil || got != tc.want {
			t.Errorf("%s: checkRetry = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp := &http.Response{StatusCode: http.StatusBadGateway, Request: &http.Request{Method: http.MethodGet}, Body: http.NoBody}
	if got, err := checkRetry(ctx, resp, nil); got || err == nil {
		t.Errorf("cancelled context: checkRetry = %v, %v; want no retry and the context error", got, err)
	}
}

// TestPostIsNotResentAfterA5xx drives the real client: a proxy 502 on a note
// POST must reach GitLab once, not up to six times.
func TestPostIsNotResentAfterA5xx(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	d, err := New(Options{Host: "gitlab.example", APIBase: srv.URL, Token: "t", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = d.client.Notes.CreateMergeRequestNote("g/p", 1,
		&gitlab.CreateMergeRequestNoteOptions{Body: new("body")})
	if err == nil {
		t.Fatal("a 502 must fail the call")
	}
	if n := posts.Load(); n != 1 {
		t.Fatalf("the note POST reached the server %d times, want 1", n)
	}
}
