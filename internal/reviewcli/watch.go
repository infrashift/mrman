// watch.go streams session changes as newline-delimited JSON.
//
// It exists to replace polling. Without it an agent re-reads the whole
// comment array every 30 seconds, pays for those tokens each time, and still
// has to ask the human "are you done" because nothing in the data says so.
//
// The two terminal events are what make an agent loop deterministic:
//
//   - submitted — comments left local_draft, so the human pushed the review
//   - closed    — the owning TUI's process is gone
//
// Neither is new information. Both were already observable through `review
// comments` and `review list`; they were simply never delivered as events.
package reviewcli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"time"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
)

// Watch event names.
const (
	WatchSnapshot       = "snapshot"
	WatchCommentAdded   = "comment_added"
	WatchCommentChanged = "comment_changed"
	WatchCommentRemoved = "comment_removed"
	WatchSubmitted      = "submitted"
	WatchClosed         = "closed"
)

// Reasons carried by a closed event.
const (
	ClosedTUIExited = "tui_exited"
	ClosedTimeout   = "timeout"
	ClosedCanceled  = "canceled"
)

// WatchEvent is one line of the stream.
type WatchEvent struct {
	Event string `json:"event"`
	// Comments is set on snapshot only.
	Comments []CommentOutput `json:"comments,omitempty"`
	// Comment is set on comment_added and comment_changed.
	Comment *CommentOutput `json:"comment,omitempty"`
	// ID is set on comment_removed.
	ID string `json:"id,omitempty"`
	// LifecycleState is set on submitted: pushed_draft or submitted.
	LifecycleState string `json:"lifecycle_state,omitempty"`
	// Reason is set on closed.
	Reason string `json:"reason,omitempty"`
}

// WatchOptions configures a stream.
type WatchOptions struct {
	// Interval between disk checks. Zero uses watchDefaultInterval.
	Interval time.Duration
	// Timeout ends the stream with closed/timeout. Zero never times out.
	Timeout time.Duration
	// Since suppresses the opening snapshot and replays only comments
	// added after that id, for resuming without re-reading everything.
	Since string
	// Done ends the stream with closed/canceled when it fires.
	Done <-chan struct{}
}

const watchDefaultInterval = time.Second

// watchNow is the clock seam.
var watchNow = time.Now

// Watch streams changes to a session until it closes, times out, or the
// caller cancels.
func Watch(store *persistence.Store, opts Options, w WatchOptions, out io.Writer) error {
	path, err := resolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	interval := w.Interval
	if interval <= 0 {
		interval = watchDefaultInterval
	}
	deadline := time.Time{}
	if w.Timeout > 0 {
		deadline = watchNow().Add(w.Timeout)
	}

	enc := json.NewEncoder(out)
	flush := func() {
		if f, ok := out.(interface{ Flush() error }); ok {
			_ = f.Flush()
		}
	}

	session, err := store.LoadSession(path)
	if err != nil {
		return err
	}
	known := indexComments(collectComments(session))

	// A resuming caller already has the earlier comments; replay only what
	// it missed rather than re-sending the whole array.
	if w.Since == "" {
		if err := enc.Encode(WatchEvent{
			Event: WatchSnapshot, Comments: collectComments(session),
		}); err != nil {
			return err
		}
	} else {
		for _, c := range commentsAfter(collectComments(session), w.Since) {
			if err := enc.Encode(WatchEvent{Event: WatchCommentAdded, Comment: &c}); err != nil {
				return err
			}
		}
	}
	flush()

	if state, ok := submittedState(session); ok {
		_ = enc.Encode(WatchEvent{Event: WatchSubmitted, LifecycleState: state})
		flush()
	}

	last := statSession(path)
	for {
		select {
		case <-w.Done:
			err := enc.Encode(WatchEvent{Event: WatchClosed, Reason: ClosedCanceled})
			flush()
			return err
		case <-time.After(interval):
		}

		if !deadline.IsZero() && !watchNow().Before(deadline) {
			err := enc.Encode(WatchEvent{Event: WatchClosed, Reason: ClosedTimeout})
			flush()
			return err
		}

		current := statSession(path)
		if current != nil && last != nil && *current == *last {
			// Unchanged file: only liveness can have moved.
			if !sessionIsLive(store, path) {
				err := enc.Encode(WatchEvent{Event: WatchClosed, Reason: ClosedTUIExited})
				flush()
				return err
			}
			continue
		}
		last = current

		session, err := store.LoadSession(path)
		if err != nil {
			// A session deleted underneath us is a close, not a crash: the
			// TUI removes an empty session on exit.
			if errors.Is(err, os.ErrNotExist) {
				err := enc.Encode(WatchEvent{Event: WatchClosed, Reason: ClosedTUIExited})
				flush()
				return err
			}
			return err
		}

		currentComments := collectComments(session)
		for _, ev := range diffComments(known, currentComments) {
			if err := enc.Encode(ev); err != nil {
				return err
			}
		}
		known = indexComments(currentComments)

		if state, ok := submittedState(session); ok {
			if err := enc.Encode(WatchEvent{Event: WatchSubmitted, LifecycleState: state}); err != nil {
				return err
			}
			flush()
			return nil
		}
		flush()

		if !sessionIsLive(store, path) {
			err := enc.Encode(WatchEvent{Event: WatchClosed, Reason: ClosedTUIExited})
			flush()
			return err
		}
	}
}

// sessionState is the mtime+size pair the TUI uses to spot external writes.
type sessionState struct {
	modTime time.Time
	size    int64
}

func statSession(path string) *sessionState {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &sessionState{modTime: info.ModTime(), size: info.Size()}
}

// sessionIsLive reports whether a TUI still holds the session open. A
// session that was never opened by a TUI is treated as live, so a headless
// agent watching its own session is not immediately told it closed.
func sessionIsLive(store *persistence.Store, path string) bool {
	paths, err := store.ActiveSessionPaths()
	if err != nil || len(paths) == 0 {
		return true
	}
	for active := range paths {
		if active == path {
			return true
		}
	}
	// Some session is active but not this one: this one's TUI exited.
	return false
}

func indexComments(comments []CommentOutput) map[string]CommentOutput {
	byID := make(map[string]CommentOutput, len(comments))
	for _, c := range comments {
		byID[c.ID] = c
	}
	return byID
}

// diffComments turns two snapshots into add/change/remove events. Ordering
// follows the current snapshot, which collectComments already sorts, and
// removals are sorted by id, so the stream is deterministic.
func diffComments(known map[string]CommentOutput, current []CommentOutput) []WatchEvent {
	var events []WatchEvent
	seen := make(map[string]bool, len(current))
	for i := range current {
		c := current[i]
		seen[c.ID] = true
		prev, existed := known[c.ID]
		switch {
		case !existed:
			events = append(events, WatchEvent{Event: WatchCommentAdded, Comment: &c})
		case !sameComment(prev, c):
			events = append(events, WatchEvent{Event: WatchCommentChanged, Comment: &c})
		}
	}
	var removed []string
	for id := range known {
		if !seen[id] {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		events = append(events, WatchEvent{Event: WatchCommentRemoved, ID: id})
	}
	return events
}

// sameComment compares two snapshots by value.
//
// CommentOutput carries pointers for its optional fields, so == compares
// addresses: two structurally identical reads of the same unchanged comment
// would differ, and every poll would emit a spurious comment_changed.
func sameComment(a, b CommentOutput) bool {
	return a.ID == b.ID &&
		a.Location == b.Location &&
		a.CommentType == b.CommentType &&
		a.LifecycleState == b.LifecycleState &&
		a.CreatedAt == b.CreatedAt &&
		a.Content == b.Content &&
		sameStr(a.Path, b.Path) &&
		sameStr(a.Side, b.Side) &&
		sameU32(a.StartLine, b.StartLine) &&
		sameU32(a.EndLine, b.EndLine)
}

func sameStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameU32(a, b *uint32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func commentsAfter(comments []CommentOutput, since string) []CommentOutput {
	for i := range comments {
		if comments[i].ID == since {
			return comments[i+1:]
		}
	}
	// An unknown id means the caller is resuming from something we no
	// longer have; replaying everything is safer than replaying nothing.
	return comments
}

// submittedState reports the lifecycle a submit moved comments into, and
// whether any comment has left local_draft at all.
func submittedState(session *model.ReviewSession) (string, bool) {
	state := ""
	for _, c := range allComments(session) {
		switch c.LifecycleState {
		case model.LifecycleSubmitted:
			return string(model.LifecycleSubmitted), true
		case model.LifecyclePushedDraft:
			state = string(model.LifecyclePushedDraft)
		}
	}
	return state, state != ""
}

func allComments(session *model.ReviewSession) []*model.Comment {
	out := append([]*model.Comment(nil), session.ReviewComments...)
	for _, review := range session.Files {
		out = append(out, review.FileComments...)
		for _, comments := range review.LineComments {
			out = append(out, comments...)
		}
	}
	return out
}
