package model

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// SessionVersion is the persisted schema version, kept aligned with tuicr's
// v1.3 format for structural compatibility.
const SessionVersion = "1.3"

// ClearScope selects what :clear removes.
type ClearScope int

// Clear scopes.
const (
	ClearCommentsOnly ClearScope = iota
	ClearCommentsAndReviewed
)

// SessionDiffSource discriminates what a session reviews.
type SessionDiffSource string

// Session diff sources, serialized snake_case.
const (
	SourceWorkingTree              SessionDiffSource = "working_tree"
	SourceStaged                   SessionDiffSource = "staged"
	SourceUnstaged                 SessionDiffSource = "unstaged"
	SourceStagedAndUnstaged        SessionDiffSource = "staged_and_unstaged"
	SourceCommitRange              SessionDiffSource = "commit_range"
	SourceWorkingTreeAndCommits    SessionDiffSource = "working_tree_and_commits"
	SourceStagedUnstagedAndCommits SessionDiffSource = "staged_unstaged_and_commits"
	SourcePullRequest              SessionDiffSource = "pull_request"
	SourcePristine                 SessionDiffSource = "pristine"
	SourcePatch                    SessionDiffSource = "patch"
)

// StringSet is a sorted set of strings that marshals as a sorted JSON array,
// matching serde's BTreeSet output determinism.
type StringSet []string

// Contains reports set membership.
func (s StringSet) Contains(v string) bool {
	i := sort.SearchStrings(s, v)
	return i < len(s) && s[i] == v
}

// Insert adds v, keeping order; reports whether it was newly added.
func (s *StringSet) Insert(v string) bool {
	i := sort.SearchStrings(*s, v)
	if i < len(*s) && (*s)[i] == v {
		return false
	}
	*s = append(*s, "")
	copy((*s)[i+1:], (*s)[i:])
	(*s)[i] = v
	return true
}

// Remove deletes v; reports whether it was present.
func (s *StringSet) Remove(v string) bool {
	i := sort.SearchStrings(*s, v)
	if i >= len(*s) || (*s)[i] != v {
		return false
	}
	*s = append((*s)[:i], (*s)[i+1:]...)
	return true
}

// Retain keeps only members for which keep returns true.
func (s *StringSet) Retain(keep func(string) bool) {
	kept := (*s)[:0]
	for _, v := range *s {
		if keep(v) {
			kept = append(kept, v)
		}
	}
	*s = kept
}

// UnmarshalJSON parses an array and normalizes it to sorted order.
func (s *StringSet) UnmarshalJSON(data []byte) error {
	var raw []string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	sort.Strings(raw)
	*s = raw
	return nil
}

// IndexRange is a (start, end) index pair, serialized as a two-element JSON
// array like Rust's (usize, usize) tuple.
type IndexRange [2]int

// FileReview is the persisted review state of one file.
type FileReview struct {
	Path          string                `json:"path"`
	Reviewed      bool                  `json:"reviewed"`
	Status        FileStatus            `json:"status"`
	FileComments  []*Comment            `json:"file_comments"`
	LineComments  map[uint32][]*Comment `json:"line_comments"`
	ReviewedHunks StringSet             `json:"reviewed_hunks"`
	ContentHash   *uint64               `json:"content_hash"`
}

// NewFileReview builds an unreviewed FileReview with a known content hash.
func NewFileReview(path string, status FileStatus, contentHash uint64) *FileReview {
	hash := contentHash
	return &FileReview{
		Path:          path,
		Status:        status,
		FileComments:  []*Comment{},
		LineComments:  map[uint32][]*Comment{},
		ReviewedHunks: StringSet{},
		ContentHash:   &hash,
	}
}

// CommentCount counts file plus line comments.
func (f *FileReview) CommentCount() int {
	n := len(f.FileComments)
	for _, comments := range f.LineComments {
		n += len(comments)
	}
	return n
}

// AddFileComment appends a file-level comment.
func (f *FileReview) AddFileComment(c *Comment) {
	f.FileComments = append(f.FileComments, c)
}

// AddLineComment appends a comment under its line key (range comments are
// keyed by the range end).
func (f *FileReview) AddLineComment(line uint32, c *Comment) {
	if f.LineComments == nil {
		f.LineComments = map[uint32][]*Comment{}
	}
	f.LineComments[line] = append(f.LineComments[line], c)
}

// Clone returns a deep copy sharing no pointers with the original.
func (f *FileReview) Clone() *FileReview {
	clone := *f
	if f.ContentHash != nil {
		hash := *f.ContentHash
		clone.ContentHash = &hash
	}
	clone.FileComments = make([]*Comment, len(f.FileComments))
	for i, c := range f.FileComments {
		clone.FileComments[i] = c.Clone()
	}
	clone.LineComments = make(map[uint32][]*Comment, len(f.LineComments))
	for line, comments := range f.LineComments {
		cloned := make([]*Comment, len(comments))
		for i, c := range comments {
			cloned[i] = c.Clone()
		}
		clone.LineComments[line] = cloned
	}
	clone.ReviewedHunks = append(StringSet(nil), f.ReviewedHunks...)
	return &clone
}

// ToggleHunkReviewed flips a hunk key's membership; reports the new state.
func (f *FileReview) ToggleHunkReviewed(key string) bool {
	if f.ReviewedHunks.Contains(key) {
		f.ReviewedHunks.Remove(key)
		return false
	}
	f.ReviewedHunks.Insert(key)
	return true
}

// ReviewSession is the persisted review document, format v1.3.
type ReviewSession struct {
	ID                       string                          `json:"id"`
	Version                  string                          `json:"version"`
	RepoPath                 string                          `json:"repo_path"`
	BranchName               *string                         `json:"branch_name"`
	BaseCommit               string                          `json:"base_commit"`
	DiffSource               SessionDiffSource               `json:"diff_source"`
	CommitRange              []string                        `json:"commit_range"`
	PrSessionKey             *forgetypes.PrSessionKey        `json:"pr_session_key"`
	RemoteCommentsVisibility forgetypes.PrCommentsVisibility `json:"remote_comments_visibility"`
	CommitSelectionRange     *IndexRange                     `json:"commit_selection_range"`
	CreatedAt                time.Time                       `json:"created_at"`
	UpdatedAt                time.Time                       `json:"updated_at"`
	ReviewComments           []*Comment                      `json:"review_comments"`
	Files                    map[string]*FileReview          `json:"files"`
	SessionNotes             *string                         `json:"session_notes"`
}

type reviewSessionAlias ReviewSession

// UnmarshalJSON restores serde defaults for fields older sessions lack:
// diff_source working_tree and remote_comments_visibility unresolved.
func (s *ReviewSession) UnmarshalJSON(data []byte) error {
	alias := reviewSessionAlias{
		DiffSource:               SourceWorkingTree,
		RemoteCommentsVisibility: forgetypes.VisibilityUnresolved,
	}
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	if alias.DiffSource == "" {
		alias.DiffSource = SourceWorkingTree
	}
	if alias.RemoteCommentsVisibility == "" {
		alias.RemoteCommentsVisibility = forgetypes.VisibilityUnresolved
	}
	*s = ReviewSession(alias)
	return nil
}

// NewReviewSession creates an empty session for the given target.
func NewReviewSession(repoPath, baseCommit string, branchName *string, source SessionDiffSource) *ReviewSession {
	now := nowFn()
	return &ReviewSession{
		ID:                       newIDFn(),
		Version:                  SessionVersion,
		RepoPath:                 repoPath,
		BranchName:               branchName,
		BaseCommit:               baseCommit,
		DiffSource:               source,
		RemoteCommentsVisibility: forgetypes.VisibilityUnresolved,
		CreatedAt:                now,
		UpdatedAt:                now,
		ReviewComments:           []*Comment{},
		Files:                    map[string]*FileReview{},
	}
}

// Clone returns a deep copy sharing no pointers with the original.
func (s *ReviewSession) Clone() *ReviewSession {
	clone := *s
	if s.BranchName != nil {
		branch := *s.BranchName
		clone.BranchName = &branch
	}
	clone.CommitRange = append([]string(nil), s.CommitRange...)
	if s.PrSessionKey != nil {
		key := *s.PrSessionKey
		clone.PrSessionKey = &key
	}
	if s.CommitSelectionRange != nil {
		r := *s.CommitSelectionRange
		clone.CommitSelectionRange = &r
	}
	if s.SessionNotes != nil {
		notes := *s.SessionNotes
		clone.SessionNotes = &notes
	}
	clone.ReviewComments = make([]*Comment, len(s.ReviewComments))
	for i, c := range s.ReviewComments {
		clone.ReviewComments[i] = c.Clone()
	}
	clone.Files = make(map[string]*FileReview, len(s.Files))
	for path, review := range s.Files {
		clone.Files[path] = review.Clone()
	}
	return &clone
}

// ReviewedCount counts files marked reviewed.
func (s *ReviewSession) ReviewedCount() int {
	n := 0
	for _, f := range s.Files {
		if f.Reviewed {
			n++
		}
	}
	return n
}

// HasReviewedState reports whether any file or hunk is marked reviewed.
func (s *ReviewSession) HasReviewedState() bool {
	for _, f := range s.Files {
		if f.Reviewed || len(f.ReviewedHunks) > 0 {
			return true
		}
	}
	return false
}

// AddFile registers a file. It returns true when a previously reviewed
// file's content changed, which resets its reviewed flag; a legacy entry
// with no content hash always invalidates.
func (s *ReviewSession) AddFile(path string, status FileStatus, contentHash uint64) bool {
	if review, ok := s.Files[path]; ok {
		oldHash := review.ContentHash
		hash := contentHash
		review.ContentHash = &hash
		if review.Reviewed && (oldHash == nil || *oldHash != contentHash) {
			review.Reviewed = false
			return true
		}
		return false
	}
	s.Files[path] = NewFileReview(path, status, contentHash)
	return false
}

// AddDiffFile registers a parsed diff file and prunes reviewed-hunk keys
// that no longer exist in it.
func (s *ReviewSession) AddDiffFile(file *DiffFile) bool {
	path := file.DisplayPath()
	invalidated := s.AddFile(path, file.Status, file.ContentHash)
	if review, ok := s.Files[path]; ok {
		valid := make(map[string]bool)
		for _, key := range file.HunkReviewKeys() {
			valid[key] = true
		}
		review.ReviewedHunks.Retain(func(key string) bool { return valid[key] })
	}
	return invalidated
}

// AddDiffFilePreservingHunks registers a transient filtered diff without
// dropping hunk keys belonging to the broader persisted scope.
func (s *ReviewSession) AddDiffFilePreservingHunks(file *DiffFile) bool {
	return s.AddFile(file.DisplayPath(), file.Status, file.ContentHash)
}

// File returns the review entry for path, or nil.
func (s *ReviewSession) File(path string) *FileReview {
	return s.Files[path]
}

// HasComments reports whether any comment exists at any scope.
func (s *ReviewSession) HasComments() bool {
	if len(s.ReviewComments) > 0 {
		return true
	}
	for _, f := range s.Files {
		if f.CommentCount() > 0 {
			return true
		}
	}
	return false
}

// ClearComments removes comments (and reviewed state under
// ClearCommentsAndReviewed), returning (cleared, unreviewed) counts.
func (s *ReviewSession) ClearComments(scope ClearScope) (cleared, unreviewed int) {
	cleared = len(s.ReviewComments)
	s.ReviewComments = s.ReviewComments[:0]
	for _, f := range s.Files {
		cleared += f.CommentCount()
		f.FileComments = f.FileComments[:0]
		f.LineComments = map[uint32][]*Comment{}
		if scope == ClearCommentsAndReviewed {
			if f.Reviewed || len(f.ReviewedHunks) > 0 {
				unreviewed++
			}
			f.Reviewed = false
			f.ReviewedHunks = StringSet{}
		}
	}
	return cleared, unreviewed
}

// IsFileReviewed reports the reviewed flag for path.
func (s *ReviewSession) IsFileReviewed(path string) bool {
	f, ok := s.Files[path]
	return ok && f.Reviewed
}

// IsHunkReviewed reports whether the hunk key is marked reviewed for path.
func (s *ReviewSession) IsHunkReviewed(path, key string) bool {
	f, ok := s.Files[path]
	return ok && f.ReviewedHunks.Contains(key)
}
