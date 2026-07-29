// appinit.go ports the read-only slice of tuicr's src/app/init.rs
// App::build: session registration of the parsed diff, default state, and
// the initial sort/expand/annotate pass.
package app

import (
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// RegisterDiffFiles registers every parsed diff file in the session so
// reviewed-state checks and content-hash invalidation work.
func RegisterDiffFiles(session *model.ReviewSession, files []model.DiffFile) {
	for i := range files {
		session.AddDiffFile(&files[i])
	}
}

// NewApp is the shared constructor all open paths converge on: it registers
// the diff files in the session, applies tuicr's defaults (diff panel
// focused, unified view, file list shown, wrapping on), sorts files by
// directory, expands the tree, populates the line-count cache, and builds
// the initial annotations.
func NewApp(backend vcs.Backend, info *vcs.Info, files []model.DiffFile, session *model.ReviewSession, source DiffSource) *App {
	RegisterDiffFiles(session, files)

	a := &App{
		VCS:        backend,
		VcsInfo:    info,
		Session:    session,
		DiffFiles:  files,
		DiffSource: source,

		InputMode:    input.ModeNormal,
		FocusedPanel: PanelDiff,
		DiffViewMode: ViewUnified,

		DiffState: NewDiffState(),

		ShowFileList:        true,
		CursorLineHighlight: true,

		ExpandedDirs:       map[string]bool{},
		ExpandedTop:        map[GapID][]model.DiffLine{},
		ExpandedBottom:     map[GapID][]model.DiffLine{},
		FileLineCountCache: map[int]uint32{},

		// The default cycle is just the typeless None entry; callers with a
		// comment_types config overwrite via SetCommentTypes.
		CommentTypes: ResolveCommentTypes(nil),
	}
	a.CommentType = a.DefaultCommentType()

	a.SortFilesByDirectory(true)
	a.ExpandAllDirs()
	a.populateFileLineCountCache()
	a.RebuildAnnotations()
	return a
}

// SetCommentTypes installs the configured comment-type cycle (None is
// always kept cyclable) and resets the current type to the new default.
func (a *App) SetCommentTypes(configs []CommentTypeDef) {
	a.CommentTypes = ResolveCommentTypes(configs)
	a.CommentType = a.DefaultCommentType()
}
