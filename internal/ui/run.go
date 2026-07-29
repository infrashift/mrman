package ui

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/detect"
	"github.com/infrashift/mrman/internal/vcs/filebackend"
)

// Run opens the read-only TUI for the given CLI options (M3 scope: working
// tree and commit ranges; target selector, sessions-on-disk wiring and PR
// mode land in later milestones).
func Run(opts cli.TuiOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, cfgWarnings := config.Load()
	resolved, warnings, err := resolveTheme(opts, cfg)
	if err != nil {
		return err
	}
	warnings = append(cfgWarnings, warnings...)
	if cfg.TransparentBackground {
		resolved.ApplyTransparentBackground()
	}

	var backend vcs.Backend
	switch {
	case opts.File != "":
		fb, fbErr := filebackend.New(opts.File)
		if fbErr != nil {
			return fbErr
		}
		backend = fb
	case opts.AllFiles:
		paths, pErr := filebackend.CollectTrackedPaths(cwd, vcs.SystemRunner{})
		if pErr != nil {
			return pErr
		}
		fb, fbErr := filebackend.NewPristine(paths, cwd)
		if fbErr != nil {
			return fbErr
		}
		backend = fb
	default:
		detected, dErr := detect.Detect(cwd, vcs.WhitespaceNormal, vcs.SystemRunner{})
		if dErr != nil {
			if errors.Is(dErr, errs.ErrNotARepository) {
				return fmt.Errorf("not inside a supported repository (git or jj): %w", dErr)
			}
			return dErr
		}
		backend = detected
	}
	info := backend.Info()

	// No explicit target and a real VCS → open the target selector instead
	// of loading a diff (tuicr's default entry).
	selectorStart := opts.Revisions == "" && !opts.WorkingTree &&
		opts.File == "" && !opts.AllFiles

	highlighter := resolved.Highlighter()
	var files []model.DiffFile
	source := app.DiffSource{Kind: app.DiffSourceWorkingTree}
	switch {
	case selectorStart && opts.File == "" && !opts.AllFiles:
		// Files load after the user confirms a target.
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

	if !selectorStart {
		filter := ignore.Load(info.RootPath)
		files = filter.FilterDiffFiles(files)
		if opts.Path != "" {
			files = filterByPath(files, opts.Path)
		}
		if len(files) == 0 {
			return fmt.Errorf("no changes to review")
		}
	}

	baseCommit := info.HeadCommit
	sessionSrc := sessionSource(source)
	if opts.AllFiles {
		// Pristine identity is stable across pulls: prefixed head + path hash.
		baseCommit = fmt.Sprintf("pristine:%s:%016x",
			filebackend.HeadShortSHA(cwd, vcs.SystemRunner{}), pathSetHash(files))
		sessionSrc = model.SourcePristine
	}
	fresh := model.NewReviewSession(info.RootPath, baseCommit, info.BranchName, sessionSrc)
	fresh.CommitRange = source.Commits

	store, storeErr := persistence.NewDefaultStore()
	if storeErr != nil {
		store = nil // reviews dir unavailable: run without persistence
	}
	var (
		lifecycle *sessionLifecycle
		session   = fresh
	)
	if !selectorStart {
		lifecycle, session = openSession(store, fresh)
	}

	a := app.NewApp(backend, info, files, session, source)
	if selectorStart {
		if selErr := a.EnterTargetSelector(app.TargetTabLocal); selErr != nil {
			return selErr
		}
	}
	if opts.AllFiles {
		// Pristine mode forces unified rendering and single-file focus.
		a.IsPristineMode = true
		a.DiffViewMode = app.ViewUnified
		a.IsSingleFileView = true
	}

	m := NewModel(a, resolved)
	m.session = lifecycle
	m.store = store
	m.export = exportOptions{ShowLegend: true, ToStdout: opts.Stdout}
	applyConfig(cfg, a, m)
	for _, w := range warnings {
		a.SetWarning(w)
	}
	if storeErr != nil {
		a.SetStickyWarning("Sessions are not persisted: " + storeErr.Error())
	}

	prog := tea.NewProgram(m)
	_, err = prog.Run()
	if m.session != nil {
		m.session.finish(a)
	}
	if m.PendingStdout != "" {
		fmt.Print(m.PendingStdout)
	}
	return err
}

func resolveTheme(opts cli.TuiOptions, cfg config.Config) (*theme.Theme, []string, error) {
	flagAppearance := theme.AppearanceUnset
	if opts.Appearance != "" {
		parsed, err := theme.ParseAppearance(opts.Appearance)
		if err != nil {
			return nil, nil, err
		}
		flagAppearance = parsed
	}
	cfgAppearance := theme.AppearanceUnset
	if cfg.Appearance != "" && cfg.Appearance != "system" {
		if parsed, err := theme.ParseAppearance(cfg.Appearance); err == nil {
			cfgAppearance = parsed
		}
	}
	return theme.Resolve(opts.Theme, cfg.Theme, cfg.ThemeDark, cfg.ThemeLight,
		flagAppearance, cfgAppearance, systemIsDark)
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

// pathSetHash hashes the sorted file path set for pristine session identity.
func pathSetHash(files []model.DiffFile) uint64 {
	hasher := fnv.New64a()
	for i := range files {
		_, _ = hasher.Write([]byte(files[i].DisplayPath()))
		_, _ = hasher.Write([]byte("\n"))
	}
	return hasher.Sum64()
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
