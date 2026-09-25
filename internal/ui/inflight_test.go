package ui

import "testing"

func TestShutdownCancelsEveryRequestInFlight(t *testing.T) {
	var r inflightRequests
	reload := r.replace(&r.reload)
	threads := r.replace(&r.threads)

	r.shutdown()

	if reload.Err() == nil || threads.Err() == nil {
		t.Error("shutdown must cancel every request still running")
	}
}

// TestRequestsAfterAShutdownAreLive is the live-GitLab regression: a
// head-moved reload shuts the outgoing session down and then fetches for the
// new one. shutdown cancelled the shared root but kept it, so every later
// request derived from an already-cancelled context — the new head's comments
// failed at once ("Could not load existing comments: context canceled") and
// every reload after that failed the same way until mrman was restarted.
func TestRequestsAfterAShutdownAreLive(t *testing.T) {
	var r inflightRequests
	r.replace(&r.threads)
	r.shutdown()

	if err := r.replace(&r.threads).Err(); err != nil {
		t.Fatalf("a request issued after shutdown starts cancelled: %v", err)
	}
	if err := r.replace(&r.reload).Err(); err != nil {
		t.Fatalf("a reload issued after shutdown starts cancelled: %v", err)
	}
}
