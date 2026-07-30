// Package patch reads standalone patch artifacts — a bare unified diff, a
// single `git format-patch` file, or an mbox holding a whole series — into a
// structured Series that mrman can review without a repository.
//
// It exists for two audiences the VCS backends cannot serve: organisations
// where code crosses an airgap as a .patch file, and mailing-list projects
// (the Linux kernel most of all) where the unit of review *is* a posted
// series and there may be no checkout of the tree it applies to.
//
// The package is deliberately pure — stdlib plus errs, no VCS, no app state —
// and it does not parse diffs. Its job is to work out what kind of artifact it
// has, split a mailbox into messages, pull the mail metadata off each one, and
// hand the diff body to internal/vcs/diffparser in a shape indistinguishable
// from `git diff` output. Keeping the mail knowledge here is what stops the
// diff parser — which every VCS backend depends on — from having to learn
// about mailboxes.
package patch

import (
	"time"
)

// Patch is one reviewable unit: a commit's worth of change, plus whatever
// the mail around it said about the change.
type Patch struct {
	// MessageID is the mail Message-Id, without angle brackets. Empty for a
	// bare diff. It is the stable identity of a posted patch and what a reply
	// threads against.
	MessageID string
	// InReplyTo and References are the thread the patch was posted into,
	// without angle brackets; used to thread a review reply correctly.
	InReplyTo  string
	References []string

	// Subject is the mail subject with any [PATCH ...] prefix removed, i.e.
	// the commit summary line.
	Subject string
	// RawSubject is the subject as posted, prefix included.
	RawSubject string
	// Version is the series revision from a "v2" in the subject prefix; 1
	// when unstated.
	Version int
	// SeriesPos and SeriesLen come from an "n/m" in the subject prefix; both
	// zero when the subject carries no position.
	SeriesPos, SeriesLen int

	// Author is the From: display name and address as posted.
	Author string
	// Date is the mail Date:, zero when absent or unparsable.
	Date time.Time

	// Changelog is the commit message body between the subject and the diff:
	// the prose, the Signed-off-by trailers, and any "Changes since v1"
	// section. Reviewers comment on this as much as on the code.
	Changelog string

	// DiffText is the patch's diff, normalised into canonical git shape and
	// ready for diffparser.Parse.
	DiffText string
}

// IsSeriesMember reports whether the subject carried an n/m position.
func (p *Patch) IsSeriesMember() bool { return p.SeriesLen > 0 }

// Series is an ordered set of patches read from one artifact, in the order
// they appeared in it — which for a posted series is the order the author
// intended them to be read and applied.
type Series struct {
	// Patches in source order.
	Patches []Patch
	// Source is the path the series was read from.
	Source string
	// ContentHash is a hash of the artifact's bytes. It is the session
	// identity: an edited or re-sent patch is a different review.
	ContentHash uint64
	// Kind records how the artifact was recognised.
	Kind Kind
}

// Len is the patch count.
func (s *Series) Len() int { return len(s.Patches) }

// CoverLetter returns the series' 0/N cover letter when it has one. A cover
// letter carries no diff, so it is not reviewable code, but its prose is
// often where the design argument lives.
func (s *Series) CoverLetter() (*Patch, bool) {
	for i := range s.Patches {
		if s.Patches[i].SeriesPos == 0 && s.Patches[i].SeriesLen > 0 &&
			s.Patches[i].DiffText == "" {
			return &s.Patches[i], true
		}
	}
	return nil, false
}
