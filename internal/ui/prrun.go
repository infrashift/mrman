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

	ctx := context.Background()
	details, err := backend.GetPullRequest(ctx, *parsed)
	if err != nil {
		return err
	}
	patch, err := backend.GetDiff(ctx, details)
	if err != nil {
		return err
	}
	commits, _ := backend.ListCommits(ctx, details) // best-effort

	files, err := diffparser.Parse(patch, diffparser.GitStyle, resolved.Highlighter())
	if err != nil {
		return err
	}
	if localCheckout != "" {
		files = ignore.Load(localCheckout).FilterDiffFiles(files)
	}
	if len(files) == 0 {
		return fmt.Errorf("pull request #%d has no file changes", details.Number)
	}

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
	a := app.NewApp(prnoop.New(localCheckout), info, files, session,
		app.DiffSource{Kind: app.DiffSourcePullRequest})
	a.Pr = &app.PrState{
		Backend:    backend,
		Repository: repo,
		Details:    details,
		Commits:    commits,
	}
	a.CommentTypePrefix = cfg.Forge.CommentTypePrefix

	m := NewModel(a, resolved)
	m.session = lifecycle
	m.store = store
	m.export = exportOptions{ShowLegend: cfg.ExportLegend, ToStdout: opts.Stdout}
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
