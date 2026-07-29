package ui

import (
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/detect"
)

// Run opens the read-only TUI for the given CLI options (M3 scope: working
// tree and commit ranges; target selector, sessions-on-disk wiring and PR
// mode land in later milestones).
func Run(opts cli.TuiOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	resolved, warnings, err := resolveTheme(opts)
	if err != nil {
		return err
	}

	backend, err := detect.Detect(cwd, vcs.WhitespaceNormal, vcs.SystemRunner{})
	if err != nil {
		if errors.Is(err, errs.ErrNotARepository) {
			return fmt.Errorf("not inside a supported repository (git or jj): %w", err)
		}
		return err
	}
	info := backend.Info()

	highlighter := resolved.Highlighter()
	var files []model.DiffFile
	source := app.DiffSource{Kind: app.DiffSourceWorkingTree}
	switch {
	case opts.Revisions != "":
		rng, resolveErr := backend.ResolveRevisionRange(opts.Revisions)
		if resolveErr != nil {
			return resolveErr
		}
		files, err = backend.CommitRangeDiff(rng, highlighter)
		source = app.DiffSource{Kind: app.DiffSourceCommitRange, Commits: reversed(rng.CommitIDs)}
	default:
		files, err = backend.WorkingTreeDiff(highlighter)
	}
	if err != nil {
		if errors.Is(err, errs.ErrNoChanges) {
			return fmt.Errorf("no changes to review")
		}
		return err
	}

	filter := ignore.Load(info.RootPath)
	files = filter.FilterDiffFiles(files)
	if opts.Path != "" {
		files = filterByPath(files, opts.Path)
	}
	if len(files) == 0 {
		return fmt.Errorf("no changes to review")
	}

	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, sessionSource(source))
	a := app.NewApp(backend, info, files, session, source)

	m := NewModel(a, resolved)
	for _, w := range warnings {
		a.SetWarning(w)
	}

	prog := tea.NewProgram(m)
	_, err = prog.Run()
	return err
}

func resolveTheme(opts cli.TuiOptions) (*theme.Theme, []string, error) {
	flagAppearance := theme.AppearanceUnset
	if opts.Appearance != "" {
		parsed, err := theme.ParseAppearance(opts.Appearance)
		if err != nil {
			return nil, nil, err
		}
		flagAppearance = parsed
	}
	return theme.Resolve(opts.Theme, "", "", "", flagAppearance, theme.AppearanceUnset, systemIsDark)
}

// systemIsDark defaults to dark; the OSC-11/OS detection chain lands with
// the config milestone.
func systemIsDark() bool { return true }

func sessionSource(src app.DiffSource) model.SessionDiffSource {
	switch src.Kind {
	case app.DiffSourceCommitRange:
		return model.SourceCommitRange
	case app.DiffSourceStaged:
		return model.SourceStaged
	case app.DiffSourceUnstaged:
		return model.SourceUnstaged
	case app.DiffSourceStagedAndUnstaged:
		return model.SourceStagedAndUnstaged
	}
	return model.SourceWorkingTree
}

func reversed(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[len(ids)-1-i] = id
	}
	return out
}

func filterByPath(files []model.DiffFile, prefix string) []model.DiffFile {
	kept := files[:0]
	for i := range files {
		path := files[i].DisplayPath()
		if path == prefix || hasDirPrefix(path, prefix) {
			kept = append(kept, files[i])
		}
	}
	return kept
}

func hasDirPrefix(path, prefix string) bool {
	if len(prefix) > 0 && prefix[len(prefix)-1] == '/' {
		return len(path) >= len(prefix) && path[:len(prefix)] == prefix
	}
	return len(path) > len(prefix) && path[:len(prefix)] == prefix && path[len(prefix)] == '/'
}
