package ui

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	_ "github.com/infrashift/mrman/internal/forge/drivers" // register drivers
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
	"github.com/infrashift/mrman/internal/vcs/prnoop"
)

// RunPr opens the TUI on a forge pull request target.
func RunPr(target string, opts cli.TuiOptions) error {
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

	// Resolve the checkout repository (best-effort) for bare-number and
	// owner/repo#N targets and for local fast paths.
	var checkoutRepo *forgetypes.Repository
	localCheckout := ""
	if opts.RepoURL != "" {
		if repo, repoErr := forge.ResolveRepository([]string{opts.RepoURL}, cfg.Forge); repoErr == nil {
			checkoutRepo = repo
		}
	} else if urls := forge.RemoteURLs(cwd, vcs.SystemRunner{}); len(urls) > 0 {
		if repo, repoErr := forge.ResolveRepository(urls, cfg.Forge); repoErr == nil {
			checkoutRepo = repo
			localCheckout = cwd
		}
	}

	parsed, err := forge.ParseTarget(target, checkoutRepo, cfg.Forge)
	if err != nil {
		return err
	}
	repo := parsed.Repository
	if repo == nil {
		return fmt.Errorf("cannot determine the repository for '%s': run inside a checkout or pass owner/repo#%d", target, parsed.Number)
	}

	backend, err := forge.ForRepository(*repo, cfg.Forge)
	if err != nil {
		return err
	}

	load, err := fetchPullRequest(context.Background(), backend, repo, *parsed,
		resolved.Highlighter(), localCheckout)
	if err != nil {
		return err
	}
	details := load.Details

	fresh := app.NewPrSession(details)
	store, storeErr := persistence.NewDefaultStore()
	if storeErr != nil {
		store = nil
	}
	lifecycle, session := openPrSession(store, fresh)

	info := &vcs.Info{
		RootPath:   localCheckout,
		HeadCommit: details.HeadSHA,
		Type:       vcs.TypeGit,
	}
	a := app.NewApp(load.VCS, info, load.Files, session,
		app.DiffSource{Kind: app.DiffSourcePullRequest})
	a.Pr = &app.PrState{
		Backend:    load.Backend,
		Repository: load.Repository,
		Details:    details,
		Commits:    load.Commits,
	}
	a.CommentTypePrefix = cfg.Forge.CommentTypePrefix
	a.SetupPrCommitSelector(load.Commits)

	m := NewModel(a, resolved)
	m.session = lifecycle
	m.store = store
	m.export = exportOptions{ShowLegend: cfg.ExportLegend, ToStdout: opts.Stdout}
	m.forge = staticForgeResolver(backend, *repo)
	m.localCheckout = localCheckout
	applyConfig(cfg, a, m)
	for _, w := range warnings {
		a.SetWarning(w)
	}
	a.SetMessage(fmt.Sprintf("Reviewing %s#%d · %s", repo.Slug(), details.Number, details.Title))

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

// fetchPullRequest performs the network round of opening a pull request —
// details, cumulative diff, commits — and parses the patch into diff files.
// Shared by the `mrman pr` entrypoint and the selector's Pull Requests tab
// so both produce identical review state.
//
// ListCommits is best-effort: a PR is perfectly reviewable without its
// commit list, and some forges rate-limit it separately.
func fetchPullRequest(
	ctx context.Context,
	backend forge.Forge,
	repo *forgetypes.Repository,
	target forge.Target,
	highlighter *syntax.Highlighter,
	localCheckout string,
) (app.PullRequestLoad, error) {
	details, err := backend.GetPullRequest(ctx, target)
	if err != nil {
		return app.PullRequestLoad{}, err
	}
	patch, err := backend.GetDiff(ctx, details)
	if err != nil {
		return app.PullRequestLoad{}, err
	}
	commits, _ := backend.ListCommits(ctx, details)

	files, err := diffparser.Parse(patch, diffparser.GitStyle, highlighter)
	if err != nil {
		return app.PullRequestLoad{}, err
	}
	if localCheckout != "" {
		files = ignore.Load(localCheckout).FilterDiffFiles(files)
	}
	if len(files) == 0 {
		return app.PullRequestLoad{}, fmt.Errorf("pull request #%d has no file changes", details.Number)
	}

	return app.PullRequestLoad{
		Backend:    backend,
		Repository: repo,
		Details:    details,
		Commits:    commits,
		Files:      files,
		VCS:        prnoop.New(localCheckout),
	}, nil
}

// openPrSession resumes or persists a PR session, announcing the slug.
func openPrSession(store *persistence.Store, fresh *model.ReviewSession) (*sessionLifecycle, *model.ReviewSession) {
	lc := &sessionLifecycle{store: store, watchEvery: msDuration(1000)}
	session := fresh
	lc.wasCreated = true
	if store != nil && fresh.PrSessionKey != nil {
		if path, existing, found, err := store.LoadPrSession(fresh.PrSessionKey); err == nil && found {
			session = existing
			lc.path = path
			lc.wasCreated = false
		}
		if path, err := store.SaveSession(session); err == nil {
			lc.path = path
			lc.snapshot = session.Clone()
			lc.fileState = statFile(path)
			_ = store.MarkSessionActive(session, path)
		}
	}
	announceSession(session)
	return lc, session
}
