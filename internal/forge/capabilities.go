package forge

// Capabilities declares what a forge driver supports so the app can gate
// commands and views before any network call, replacing silent defaults
// with an explicit, honest matrix per forge.
type Capabilities struct {
	// DraftReviews means the forge holds a server-side pending review.
	DraftReviews bool
	// Approve means the forge supports an approval review event.
	Approve bool
	// RequestChanges means the forge supports a changes-requested event.
	RequestChanges bool
	// ReviewSummaries means review-level bodies exist distinct from
	// threads.
	ReviewSummaries bool
	// ReviewThreads means the forge exposes discussion threads.
	ReviewThreads bool
	// ThreadResolution means the resolved state is faithfully exposed
	// (false when approximated).
	ThreadResolution bool
	// ThreadOutdated means outdated detection is faithful (false when
	// approximated).
	ThreadOutdated bool
	// MultiLineComments means inline comments can span line ranges;
	// false triggers the submit.DowngradeMultiline pass.
	MultiLineComments bool
	// CommitRangeDiff means the forge serves a server-side range diff;
	// false means the driver synthesizes one or the app disables the
	// commit-range selector.
	CommitRangeDiff bool
	// ReviewRequestedFilter means listing can filter to review-requested
	// pull requests.
	ReviewRequestedFilter bool
	// AtomicSubmit means reviews post in one request; false means an
	// N+1 sequence that can partially fail (drives progress UI and
	// PartialFailure handling).
	AtomicSubmit bool
	// CommitScopedReviews means review records carry commit OIDs, so
	// "commits since my last review" works.
	CommitScopedReviews bool
}
