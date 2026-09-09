package ui

import (
	"context"

	"github.com/infrashift/mrman/internal/app"
)

// inflightRequests is one cancel per async family that can be superseded:
// listing pull requests, opening one, reloading it, fetching its
// discussions, and narrowing it to a commit range. Submits are deliberately
// absent — a submit interrupted midway is a partial post, which is worse
// than a late one — and context expansion runs several requests at once,
// so it shares only the shutdown parent.
type inflightRequests struct {
	parent    context.Context
	cancelAll context.CancelFunc
	list      context.CancelFunc
	open      context.CancelFunc
	reload    context.CancelFunc
	threads   context.CancelFunc
	rangeDiff context.CancelFunc
}

// root returns the context every request derives from, created on first
// use and cancelled by shutdown.
func (r *inflightRequests) root() context.Context {
	if r.parent == nil {
		r.parent, r.cancelAll = context.WithCancel(context.Background())
	}
	return r.parent
}

// replace cancels whatever the family was doing and returns a context for
// its next request.
func (r *inflightRequests) replace(slot *context.CancelFunc) context.Context {
	if *slot != nil {
		(*slot)()
	}
	ctx, cancel := context.WithCancel(r.root())
	*slot = cancel
	return ctx
}

// shutdown cancels every request still running.
func (r *inflightRequests) shutdown() {
	if r.cancelAll != nil {
		r.cancelAll()
	}
}

// shutdown ends the session's bookkeeping and stops every request still in
// flight. Every path that closes or replaces a session goes through here.
func (m *Model) shutdown(a *app.App) {
	m.inflight.shutdown()
	if m.session != nil {
		m.session.finish(a)
	}
}
