// prcontext.go serves hidden-context expansion in PR mode, where the diff's
// surrounding lines live on the forge rather than in a local checkout.
//
// Expansion has to stay synchronous — ExpandGap is called from the key
// dispatch and from the {N}G / :{N} jump planner, which expands repeatedly
// while walking toward a target — so this provider never performs I/O. It
// serves from a per-file snapshot cache and reports ErrContextNotLoaded when
// a file has not been fetched yet. The UI layer catches that, fetches the
// whole file once off the render loop, installs it, and replays the
// expansion. After the first press every later expansion in that file is
// instant, which is how a reviewer actually uses the key.
package app

import (
	"errors"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
)

// ErrContextNotLoaded means the file's snapshot has not been fetched from
// the forge yet. It is a control-flow signal, not a failure.
var ErrContextNotLoaded = errors.New("pull request file context not loaded yet")

// prContextKey identifies a cached file snapshot: one path per diff side.
type prContextKey struct {
	Path string
	Side forge.FileSide
}

// prFileSnapshot is a whole file at one PR revision, kept in new-side line
// order so a range slice is O(1).
type prFileSnapshot struct {
	Lines []model.DiffLine
	Count uint32
}

// PrContextProvider serves gap expansion from fetched PR file snapshots.
type PrContextProvider struct {
	files map[prContextKey]*prFileSnapshot
	// missing records files the forge could not serve, so a file that is
	// genuinely unavailable is not refetched on every keystroke.
	missing map[prContextKey]bool
	// inflight records fetches already running, so a keypress storm cannot
	// fan out into duplicate requests for the same file.
	inflight map[prContextKey]bool
}

// NewPrContextProvider returns an empty snapshot cache.
func NewPrContextProvider() *PrContextProvider {
	return &PrContextProvider{
		files:    map[prContextKey]*prFileSnapshot{},
		missing:  map[prContextKey]bool{},
		inflight: map[prContextKey]bool{},
	}
}

// keyFor resolves the cache key for a file, choosing the side the forge
// should be read from and the path that exists on that side.
func prContextKeyFor(oldPath, newPath *string, status model.FileStatus) (prContextKey, bool) {
	side := forge.SideForStatus(status)
	path, ok := forge.PathForSide(side, oldPath, newPath)
	if !ok {
		return prContextKey{}, false
	}
	return prContextKey{Path: path, Side: side}, true
}

// FetchContextLines implements ContextProvider from the snapshot cache.
func (p *PrContextProvider) FetchContextLines(
	oldPath, newPath *string, status model.FileStatus, start, end uint32,
) ([]model.DiffLine, error) {
	snapshot, err := p.snapshot(oldPath, newPath, status)
	if err != nil {
		return nil, err
	}
	if start < 1 || start > end {
		return nil, nil
	}
	// Lines are stored 1-indexed by position; clamp to what we hold.
	lo := int(start - 1)
	hi := min(int(end), len(snapshot.Lines))
	if lo >= hi {
		return nil, nil
	}
	out := make([]model.DiffLine, hi-lo)
	copy(out, snapshot.Lines[lo:hi])
	return out, nil
}

// FileLineCount implements ContextProvider from the snapshot cache.
func (p *PrContextProvider) FileLineCount(
	oldPath, newPath *string, status model.FileStatus,
) (uint32, error) {
	snapshot, err := p.snapshot(oldPath, newPath, status)
	if err != nil {
		return 0, err
	}
	return snapshot.Count, nil
}

func (p *PrContextProvider) snapshot(oldPath, newPath *string, status model.FileStatus) (*prFileSnapshot, error) {
	key, ok := prContextKeyFor(oldPath, newPath, status)
	if !ok {
		return nil, errors.New("file has no path on either side")
	}
	if p.missing[key] {
		return nil, errors.New("the forge could not serve " + key.Path)
	}
	snapshot, ok := p.files[key]
	if !ok {
		return nil, ErrContextNotLoaded
	}
	return snapshot, nil
}

// Install caches a fetched file snapshot.
func (p *PrContextProvider) Install(key prContextKey, lines []model.DiffLine, count uint32) {
	p.files[key] = &prFileSnapshot{Lines: lines, Count: count}
	delete(p.inflight, key)
}

// MarkMissing records that the forge cannot serve a file, so the UI stops
// retrying it.
func (p *PrContextProvider) MarkMissing(key prContextKey) {
	p.missing[key] = true
	delete(p.inflight, key)
}

// settled reports whether a file needs no further fetch: already cached,
// known unavailable, or a fetch is already running for it.
func (p *PrContextProvider) settled(key prContextKey) bool {
	if p.missing[key] || p.inflight[key] {
		return true
	}
	_, ok := p.files[key]
	return ok
}

// PrContextRequest asks the UI layer to fetch one file's whole snapshot.
type PrContextRequest struct {
	// Key identifies the cache slot to fill.
	Key prContextKey
	// FileIdx is the diff file the request came from.
	FileIdx int
	// Status lets the driver skip recomputing the side mapping.
	Status model.FileStatus
	// Replay is the expansion to retry once the snapshot lands; nil when
	// the fetch was triggered by something other than an expander press.
	Replay *PrContextReplay
}

// PrContextReplay is the gap expansion to re-run after a snapshot arrives.
type PrContextReplay struct {
	Gap       GapID
	Direction ExpandDirection
	Limit     *int
	// Goto re-runs the source-line jump that triggered the fetch, so {N}G
	// into hidden context lands on the line rather than merely revealing
	// it. Nil when a plain expander press triggered the fetch.
	Goto *GotoTarget
}

// GotoTarget is a source-line jump to replay.
type GotoTarget struct {
	Line uint32
	Side model.LineSide
}

// requestContextForJump arms a snapshot fetch on behalf of a source-line
// jump that ran into unfetched PR context.
func (a *App) requestContextForJump(
	fileIdx int, gap GapID, direction ExpandDirection, limit *int,
	line uint32, side model.LineSide,
) {
	replay := &PrContextReplay{
		Gap: gap, Direction: direction, Limit: limit,
		Goto: &GotoTarget{Line: line, Side: side},
	}
	req, ok := a.PrContextRequestFor(fileIdx, replay)
	if !ok {
		return
	}
	a.PendingContextRequest = &req
	a.SetMessage("Fetching context for " + req.Path() + "…")
}

// TakePrContextRequest drains a snapshot fetch armed from inside the app
// state machine (the jump planner), for the UI layer to perform.
func (a *App) TakePrContextRequest() (PrContextRequest, bool) {
	if a.PendingContextRequest == nil {
		return PrContextRequest{}, false
	}
	req := *a.PendingContextRequest
	a.PendingContextRequest = nil
	return req, true
}

// Path is the file path to fetch.
func (r PrContextRequest) Path() string { return r.Key.Path }

// Side is the PR revision side to read from.
func (r PrContextRequest) Side() forge.FileSide { return r.Key.Side }

// ensurePrContext lazily creates the PR snapshot cache.
func (a *App) ensurePrContext() *PrContextProvider {
	if a.PrContext == nil {
		a.PrContext = NewPrContextProvider()
	}
	return a.PrContext
}

// PrContextRequestFor builds the fetch request for a diff file, false when
// the file has no usable path, is already cached, is known unavailable, or
// already has a fetch running. Returning true marks the fetch in flight, so
// callers must actually issue it.
func (a *App) PrContextRequestFor(fileIdx int, replay *PrContextReplay) (PrContextRequest, bool) {
	if fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return PrContextRequest{}, false
	}
	file := &a.DiffFiles[fileIdx]
	key, ok := prContextKeyFor(file.OldPath, file.NewPath, file.Status)
	if !ok {
		return PrContextRequest{}, false
	}
	provider := a.ensurePrContext()
	if provider.settled(key) {
		return PrContextRequest{}, false
	}
	provider.inflight[key] = true
	return PrContextRequest{
		Key: key, FileIdx: fileIdx, Status: file.Status, Replay: replay,
	}, true
}

// PrefetchContextForCurrentFile arms a snapshot fetch for the file the
// cursor is in.
//
// Without this, a file whose only hidden context sits after its last hunk
// would never become expandable: the end-of-file gap needs the file's line
// count, which only the snapshot provides, so no expander would render and
// the user would have nothing to press. Prefetching the file they are
// actually reading costs one blob per visited file and makes every gap
// reachable.
func (a *App) PrefetchContextForCurrentFile() {
	if !a.InPrMode() || a.PendingContextRequest != nil {
		return
	}
	if req, ok := a.PrContextRequestFor(a.DiffState.CurrentFileIdx, nil); ok {
		a.PendingContextRequest = &req
	}
}

// ApplyPrContextSnapshot installs a fetched snapshot and replays the
// expansion that triggered the fetch. It reports whether the replay ran.
func (a *App) ApplyPrContextSnapshot(req PrContextRequest, lines []model.DiffLine) bool {
	provider := a.ensurePrContext()
	count := uint32(len(lines)) //nolint:gosec // file line counts stay well inside uint32
	provider.Install(req.Key, lines, count)

	// The end-of-file gap needs the count, which only exists now.
	delete(a.FileLineCountCache, req.FileIdx)
	a.ensureFileLineCountCached(req.FileIdx)

	if req.Replay == nil {
		a.RebuildAnnotations()
		return false
	}
	if err := a.ExpandGap(req.Replay.Gap, req.Replay.Direction, req.Replay.Limit); err != nil {
		a.SetError("Expand failed: " + err.Error())
		return false
	}
	if target := req.Replay.Goto; target != nil {
		a.GoToSourceLine(target.Line, target.Side)
	}
	return true
}

// FailPrContextSnapshot records that a file cannot be fetched.
func (a *App) FailPrContextSnapshot(req PrContextRequest, message string) {
	a.ensurePrContext().MarkMissing(req.Key)
	a.SetWarning("Cannot expand " + req.Path() + ": " + message)
}
