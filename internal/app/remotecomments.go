// remotecomments.go renders the forge's own review discussions inside the
// diff, porting tuicr's src/forge/remote_comments.rs.
//
// These are deliberately source-of-truth-on-remote: mrman never mutates,
// replies to, resolves, or persists them past the in-memory cache. Their
// annotation kinds exist so hit-testing, scroll math and search stay correct
// while the cursor passes over them — not so they can be edited.

package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
)

// RemoteCommentsLoadRequest asks the UI layer to fetch remote discussions.
// Like the PR tab, the app records what it wants and the UI performs it.
type RemoteCommentsLoadRequest struct {
	// Gen is the PrThreads generation; a result carrying a different one is
	// stale and dropped.
	Gen uint64
	// Key identifies the PR the fetch belongs to, so a result arriving
	// after the user switched pull requests is discarded.
	Key PrKey
}

// RequestRemoteComments arms a threads+summaries fetch for the open PR. It
// reports false outside PR mode or when visibility is "hide" — there is no
// point paying for a fetch nothing will render.
func (a *App) RequestRemoteComments() bool {
	if !a.InPrMode() || a.RemoteCommentsVisibility() == forgetypes.VisibilityHide {
		return false
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok {
		return false
	}
	a.Pr.Gens.PrThreads++
	a.Pr.ThreadsLoading = true
	a.Pr.ThreadsPending = true
	a.Pr.PendingThreads = RemoteCommentsLoadRequest{Gen: a.Pr.Gens.PrThreads, Key: key}
	return true
}

// TakeRemoteCommentsLoad drains a pending remote-comments fetch.
func (a *App) TakeRemoteCommentsLoad() (RemoteCommentsLoadRequest, bool) {
	if a.Pr == nil || !a.Pr.ThreadsPending {
		return RemoteCommentsLoadRequest{}, false
	}
	a.Pr.ThreadsPending = false
	return a.Pr.PendingThreads, true
}

// ApplyRemoteComments installs fetched discussions, discarding stale results.
func (a *App) ApplyRemoteComments(
	gen uint64,
	key PrKey,
	threads []forge.RemoteReviewThread,
	summaries []forge.RemoteReviewSummary,
) {
	if !a.InPrMode() {
		return
	}
	current, ok := a.Pr.CurrentPrKey()
	if !ok || gen != a.Pr.Gens.PrThreads || key != current {
		return // superseded by a reload or a different pull request
	}
	a.Pr.ThreadsLoading = false
	a.Pr.Threads = threads
	a.Pr.Summaries = summaries
	a.RebuildAnnotations()
}

// FailRemoteComments records a failed fetch, discarding stale results.
func (a *App) FailRemoteComments(gen uint64, key PrKey, message string) {
	if !a.InPrMode() {
		return
	}
	current, ok := a.Pr.CurrentPrKey()
	if !ok || gen != a.Pr.Gens.PrThreads || key != current {
		return
	}
	a.Pr.ThreadsLoading = false
	// A missing discussion list must not look like an empty one, but it is
	// also not worth an error banner over a diff the user can still review.
	a.SetWarning("Could not load existing comments: " + message)
}

// RemoteCommentsVisibility returns the session's thread visibility mode,
// defaulting to unresolved-only.
func (a *App) RemoteCommentsVisibility() forgetypes.PrCommentsVisibility {
	if a.Session == nil || a.Session.RemoteCommentsVisibility == "" {
		return forgetypes.VisibilityUnresolved
	}
	return a.Session.RemoteCommentsVisibility
}

// SetRemoteCommentsVisibility applies `:comments unresolved|all|hide`. It
// returns true when a fetch is now needed — switching away from "hide" for
// the first time has nothing cached to show.
func (a *App) SetRemoteCommentsVisibility(v forgetypes.PrCommentsVisibility) bool {
	if !a.InPrMode() {
		a.SetMessage("Remote comments are only available while reviewing a merge request")
		return false
	}
	a.Session.RemoteCommentsVisibility = v
	a.Dirty = true

	needsFetch := v != forgetypes.VisibilityHide && len(a.Pr.Threads) == 0 && len(a.Pr.Summaries) == 0
	a.RebuildAnnotations()

	switch v {
	case forgetypes.VisibilityHide:
		a.SetMessage("Remote comments: hidden")
	case forgetypes.VisibilityAll:
		a.SetMessage("Remote comments: all")
	case forgetypes.VisibilityUnresolved:
		a.SetMessage("Remote comments: unresolved only")
	}
	return needsFetch
}

// VisibleRemoteThreads returns the threads passing the visibility filter, in
// the forge's posted order.
func (a *App) VisibleRemoteThreads() []forge.RemoteReviewThread {
	if !a.InPrMode() {
		return nil
	}
	switch a.RemoteCommentsVisibility() {
	case forgetypes.VisibilityHide:
		return nil
	case forgetypes.VisibilityAll:
		return a.Pr.Threads
	}
	var active []forge.RemoteReviewThread
	for i := range a.Pr.Threads {
		if a.Pr.Threads[i].IsActive() {
			active = append(active, a.Pr.Threads[i])
		}
	}
	return active
}

// VisibleRemoteSummaries returns the review-level summaries to render.
// Summaries carry no resolved state, so "unresolved" and "all" show the same
// set; only "hide" suppresses them.
func (a *App) VisibleRemoteSummaries() []forge.RemoteReviewSummary {
	if !a.InPrMode() || a.RemoteCommentsVisibility() == forgetypes.VisibilityHide {
		return nil
	}
	return a.Pr.Summaries
}

// CursorOnRemoteComment reports whether the cursor rests on a forge-owned
// discussion row. Used to explain why edit and delete do nothing there,
// instead of the misleading "no comment at cursor".
func (a *App) CursorOnRemoteComment() bool {
	idx := a.DiffState.CursorLine
	if idx >= len(a.LineAnnotations) {
		return false
	}
	kind := a.LineAnnotations[idx].Kind
	return kind == AnnRemoteThreadLine || kind == AnnRemoteReviewSummaryLine
}

// remoteThreadAnchor is the (path, side, line) a thread renders under.
type remoteThreadAnchor struct {
	Path string
	Side model.LineSide
	Line uint32
}

// refreshRemoteThreadIndex groups visible threads by anchor so the
// annotation builder can look them up per diff line instead of rescanning
// the list. Threads with no line anchor (fully outdated) are indexed by file
// and render at file scope, where they stay reachable instead of vanishing.
//
// The index is stored on the App rather than returned because the height
// math (fileRenderBodyHeight) must read the exact same grouping the
// annotation builder emitted from — if the two disagree the diff pane
// truncates at the point they diverge. RebuildAnnotations refreshes it.
func (a *App) refreshRemoteThreadIndex() {
	byLine := map[remoteThreadAnchor][]int{}
	byFile := map[string][]int{}
	for idx, thread := range a.VisibleRemoteThreads() {
		if thread.Path == "" {
			continue
		}
		if thread.Line == nil {
			byFile[thread.Path] = append(byFile[thread.Path], idx)
			continue
		}
		anchor := remoteThreadAnchor{
			Path: thread.Path,
			Side: remoteSideToModel(thread.Side),
			Line: *thread.Line,
		}
		byLine[anchor] = append(byLine[anchor], idx)
	}
	a.remoteThreadsByLine = byLine
	a.remoteThreadsByFile = byFile
}

// remoteThreadsHeight is the total rendered height of the given threads.
func (a *App) remoteThreadsHeight(threadIdxs []int) int {
	if len(threadIdxs) == 0 {
		return 0
	}
	threads := a.VisibleRemoteThreads()
	height := 0
	for _, idx := range threadIdxs {
		if idx < len(threads) {
			height += RemoteThreadDisplayLines(&threads[idx], a.DiffState.ViewportWidth)
		}
	}
	return height
}

// remoteSideToModel maps a forge diff side onto the session's line side.
func remoteSideToModel(side forge.Side) model.LineSide {
	if side == forge.SideOld {
		return model.LineSideOld
	}
	return model.LineSideNew
}

// RemoteThreadDisplayLines is the rendered height of a thread box: a top
// border, one row per wrapped body line of every comment (each preceded by
// an author row), and a bottom border.
func RemoteThreadDisplayLines(thread *forge.RemoteReviewThread, viewportWidth int) int {
	// Same content-area arithmetic as CommentDisplayLines so local and
	// remote boxes wrap identically.
	contentArea := satSub(viewportWidth, 10)
	rows := 0
	for i := range thread.Comments {
		rows++ // author / timestamp row
		for line := range strings.SplitSeq(thread.Comments[i].Body, "\n") {
			rows += len(WrapSegments(line, contentArea))
		}
	}
	return 2 + rows
}

// RemoteSummaryDisplayLines is the rendered height of a review summary box.
func RemoteSummaryDisplayLines(summary *forge.RemoteReviewSummary, viewportWidth int) int {
	contentArea := satSub(viewportWidth, 10)
	rows := 0
	for line := range strings.SplitSeq(summary.Body, "\n") {
		rows += len(WrapSegments(line, contentArea))
	}
	return 2 + rows
}

// RemoteThreadSegments returns the body rows of a thread box: for each
// comment an author header followed by its wrapped body lines.
func RemoteThreadSegments(thread *forge.RemoteReviewThread, viewportWidth int) []string {
	contentArea := max(viewportWidth-10, 1)
	var segments []string
	for i := range thread.Comments {
		comment := &thread.Comments[i]
		header := "@" + comment.Author
		if comment.Author == "" {
			header = "@unknown"
		}
		if comment.CreatedAt != nil {
			header += " · " + relativeAge(*comment.CreatedAt)
		}
		if i > 0 {
			header = "↳ " + header
		}
		segments = append(segments, header)
		for line := range strings.SplitSeq(comment.Body, "\n") {
			segments = append(segments, WrapSegments(line, contentArea)...)
		}
	}
	return segments
}

// RemoteSummarySegments returns the wrapped body rows of a review summary.
func RemoteSummarySegments(summary *forge.RemoteReviewSummary, viewportWidth int) []string {
	contentArea := max(viewportWidth-10, 1)
	var segments []string
	for line := range strings.SplitSeq(summary.Body, "\n") {
		segments = append(segments, WrapSegments(line, contentArea)...)
	}
	return segments
}

// RemoteThreadBadge is the status badge shown on a thread's top border. A
// forge that distinguishes more than resolved/unresolved supplies its own
// label in Disposition — "won't fix" says something "resolved" does not —
// and forges without one fall back to the generic wording.
func RemoteThreadBadge(thread *forge.RemoteReviewThread) string {
	state := thread.Disposition
	if state == "" && thread.IsResolved {
		state = "resolved"
	}
	switch {
	case state != "" && thread.IsOutdated:
		return state + " · outdated"
	case state != "":
		return state
	case thread.IsOutdated:
		return "outdated"
	}
	return ""
}

// relativeAge formats a remote comment's age the way the selector formats
// commit ages, so every timestamp in the UI reads the same.
func relativeAge(when time.Time) string {
	d := now().Sub(when)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
}
