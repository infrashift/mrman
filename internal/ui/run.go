package ui

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/output"
	"github.com/infrashift/mrman/internal/patch"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/detect"
	"github.com/infrashift/mrman/internal/vcs/filebackend"
	"github.com/infrashift/mrman/internal/vcs/patchbackend"
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
	// patchBackend is non-nil only for a --patch review; it carries the
	// parsed series for the commit strip and the reply exporter.
	var patchBackend *patchbackend.Backend
	switch {
	case opts.Patch != "":
		pb, pbErr := patchbackend.New(opts.Patch, patch.Options{StripLevel: opts.PatchStrip})
		if pbErr != nil {
			return pbErr
		}
		backend, patchBackend = pb, pb
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
		detected, dErr := detect.Detect(cwd, whitespaceMode(cfg), vcs.SystemRunner{})
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
		opts.File == "" && !opts.AllFiles && opts.Patch == ""

	highlighter := resolved.Highlighter()
	var files []model.DiffFile
	// rangeCommits are the rows behind the inline commit strip for a -r
	// review; empty for every other start.
	var rangeCommits []vcs.CommitInfo
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
		// The strip's rows are a display concern: a backend that cannot
		// describe the commits still reviews the range fine, so failing
		// here would trade a working review for a missing panel.
		rangeCommits, _ = backend.CommitsInfo(reversed(rng.CommitIDs))
	case patchBackend != nil:
		files, err = backend.WorkingTreeDiff(highlighter)
		source = app.DiffSource{Kind: app.DiffSourcePatch}
		// Each patch is a row in the strip, so a series is walked with ( and )
		// exactly like a multi-commit review.
		rangeCommits, _ = backend.RecentCommits(0, 0)
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
	if patchBackend != nil {
		m.export.ReplyHeaders = replyHeadersFor(patchBackend.Series())
	}
	// The Pull Requests tab resolves its forge lazily on first use, so a
	// local review never pays for token resolution it will not need.
	m.forge = checkoutForgeResolver(cwd, cfg.Forge)
	m.localCheckout = info.RootPath
	a.CommentTypePrefix = cfg.Forge.CommentTypePrefix
	applyConfig(cfg, a, m)
	// After applyConfig, which would otherwise reset the strip's visibility
	// to the configured default and hide a range the user explicitly asked
	// for — the selector path installs commits well after config too.
	if len(rangeCommits) > 0 {
		a.InstallReviewCommits(rangeCommits)
	}
	for _, w := range warnings {
		a.SetWarning(w)
	}
	reportSessionResume(a, lifecycle)
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

// reportSessionResume tells the reviewer where the comments on screen came
// from. A session carried forward from a HEAD they have since rewritten is
// worth saying out loud — combined with the anchor report it is the whole
// story: these comments are older than this diff, and here is what moved.
func reportSessionResume(a *app.App, lc *sessionLifecycle) {
	if lc == nil || lc.adoptedFrom == "" {
		reportAnchorValidation(a)
		return
	}
	msg := fmt.Sprintf("Resumed this review from a previous HEAD (%.7s)", lc.adoptedFrom)
	if anchors := a.AnchorStats.Message(); anchors != "" {
		msg += " · " + anchors
	}
	if a.AnchorStats.Outdated > 0 {
		a.SetWarning(msg)
		return
	}
	a.SetMessage(msg)
}

// reportAnchorValidation surfaces what opening the session did to its comment
// anchors. Re-anchoring is informational; an anchor that could not be placed
// at all is a warning, because the reviewer is about to read a comment box
// whose line number is no longer evidence of anything.
func reportAnchorValidation(a *app.App) {
	msg := a.AnchorStats.Message()
	if msg == "" {
		return
	}
	if a.AnchorStats.Outdated > 0 {
		a.SetWarning(msg)
		return
	}
	a.SetMessage(msg)
}

// whitespaceMode maps the ignore_whitespace config onto the VCS diff mode.
// It applies to local diffs only — a pull request's diff comes from the
// forge already rendered, so there is nothing to re-run with a flag.
func whitespaceMode(cfg config.Config) vcs.WhitespaceMode {
	if cfg.IgnoreWhitespace {
		return vcs.WhitespaceIgnoreAll
	}
	return vcs.WhitespaceNormal
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
		flagAppearance, cfgAppearance, systemIsDark,
		filepath.Join(config.Dir(), "themes"))
}

// systemIsDark queries the terminal/OS appearance chain; --stdout mode is
// handled by the caller passing a pre-bound closure.
func systemIsDark() bool { return detectSystemDark(false) }

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
	case app.DiffSourcePatch:
		return model.SourcePatch
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

// replyHeadersFor derives the mail headers that thread a review reply into the
// conversation a patch was posted in.
//
// It threads against the first patch that carries a Message-Id, which for a
// series is the cover letter or patch 1 — the message the whole thread hangs
// off. When nothing carries one it returns the zero value and the exporter
// emits no header block at all: plain `git format-patch` writes no Message-Id,
// and a reply threaded to an invented id lands in the wrong conversation,
// which is worse than one that does not thread.
func replyHeadersFor(series *patch.Series) output.ReplyHeaders {
	if series == nil {
		return output.ReplyHeaders{}
	}
	for i := range series.Patches {
		p := &series.Patches[i]
		if p.MessageID == "" {
			continue
		}
		h := output.ReplyHeaders{
			InReplyTo:   p.MessageID,
			Attribution: attributionFor(p),
		}
		if p.RawSubject != "" {
			h.Subject = "Re: " + p.RawSubject
		}
		// Keep the thread's existing References and add the message being
		// replied to, which is what a mail client would do.
		h.References = append(append([]string(nil), p.References...), p.MessageID)
		return h
	}
	return output.ReplyHeaders{}
}

// attributionFor renders the "On <date>, <author> wrote:" line, omitting
// whichever half the artifact did not carry.
func attributionFor(p *patch.Patch) string {
	switch {
	case p.Author == "":
		return ""
	case p.Date.IsZero():
		return fmt.Sprintf("%s wrote:", p.Author)
	}
	return fmt.Sprintf("On %s, %s wrote:", p.Date.Format("Mon, 02 Jan 2006"), p.Author)
}
