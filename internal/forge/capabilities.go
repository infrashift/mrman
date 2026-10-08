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
	// ReviewSummaries means the forge has a review object whose body is
	// distinct from its threads. The app asks every driver for summaries
	// regardless: a forge without review objects returns its general,
	// file-less discussions there instead.
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
	// CommitRangeDiff means the forge serves a diff for a commit range.
	// Without it the commit strip still shows, but narrowing to a range
	// keeps the whole merge request's diff and says so.
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
