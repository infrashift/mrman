package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	_ "github.com/infrashift/mrman/internal/forge/drivers" // register drivers
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/prload"
	"github.com/infrashift/mrman/internal/slug"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
)

// prOpen is everything both PR entrypoints need before they diverge into a
// TUI or a JSON dump.
type prOpen struct {
	cfg           config.Config
	warnings      []string
	resolved      *theme.Theme
	repo          *forgetypes.Repository
	backend       forge.Forge
	load          app.PullRequestLoad
	localCheckout string
}

// openPullRequest resolves the target, authenticates, and fetches. Shared so
// `mrman pr X` and `mrman pr X --json` cannot drift into disagreeing about
// which pull request they mean.
func openPullRequest(target string, opts cli.TuiOptions) (*prOpen, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, cfgWarnings := config.Load()
	resolved, warnings, err := resolveTheme(opts, cfg)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	repo := parsed.Repository
	if repo == nil {
		return nil, fmt.Errorf("cannot determine the repository for '%s': run inside a checkout or pass owner/repo#%d", target, parsed.Number)
	}

	if !forge.HostTrusted(repo.Host, cfg.Forge) {
		warnings = append(warnings, forge.UntrustedHostWarning(repo.Host))
	}
	backend, err := forge.ForRepository(*repo, cfg.Forge)
	if err != nil {
		return nil, err
	}

	load, err := fetchPullRequest(context.Background(), backend, repo, *parsed,
		resolved.Highlighter(), localCheckout)
	if err != nil {
		return nil, err
	}
	return &prOpen{
		cfg: cfg, warnings: warnings, resolved: resolved,
		repo: repo, backend: backend, load: load, localCheckout: localCheckout,
	}, nil
}

// RunPr opens the TUI on a forge pull request target.
func RunPr(target string, opts cli.TuiOptions) error {
	opened, err := openPullRequest(target, opts)
	if err != nil {
		return err
	}
	cfg, warnings, resolved := opened.cfg, opened.warnings, opened.resolved
	repo, backend, load, localCheckout := opened.repo, opened.backend, opened.load, opened.localCheckout

	details := load.Details

	fresh := app.NewPrSession(details)
	store, storeErr := persistence.NewDefaultStore()
	if storeErr != nil {
		store = nil
	}
	lifecycle, session := openPrSession(store, fresh, opts.GrantedEvents)

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
	m.grantedEvents = opts.GrantedEvents
	applyConfig(cfg, a, m)
	for _, w := range warnings {
		a.SetWarning(w)
	}
	a.SetMessage(fmt.Sprintf("Reviewing %s#%d · %s", repo.Slug(), details.Number, details.Title))
	if w := lifecycle.activationWarning(len(opts.GrantedEvents) > 0); w != "" {
		a.SetStickyWarning(w)
	}
	// After the greeting, so a stale anchor is not buried under it.
	reportAnchorValidation(a)

	prog := tea.NewProgram(m)
	_, err = prog.Run()
	if m.session != nil {
		m.shutdown(a)
	}
	if m.PendingStdout != "" {
		fmt.Print(m.PendingStdout)
	}
	return err
}

// fetchPullRequest forwards to prload, kept so the UI's call sites read
// the same as before the extraction.
func fetchPullRequest(
	ctx context.Context,
	backend forge.Forge,
	repo *forgetypes.Repository,
	target forge.Target,
	highlighter *syntax.Highlighter,
	localCheckout string,
) (app.PullRequestLoad, error) {
	return prload.Fetch(ctx, backend, repo, target, highlighter, localCheckout)
}

// openPrSession resumes or persists a PR session, announcing the slug.
//
// grantedEvents is the agent-submit authorization from an interactive
// --auto; it is recorded against this process so it dies when the TUI does.
func openPrSession(
	store *persistence.Store, fresh *model.ReviewSession, grantedEvents []string,
) (*sessionLifecycle, *model.ReviewSession) {
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
			lc.lastHeartbeatAt = time.Now()
			lc.activateErr = store.MarkSessionActiveWithGrant(session, path, grantedEvents)
		}
	}
	announceSession(session)
	return lc, session
}

// reportAgentGrant answers `:agent` with what an agent may currently submit.
func (m *Model) reportAgentGrant() {
	if len(m.grantedEvents) == 0 {
		m.App.SetMessage("Agent submit: off — relaunch with --auto to allow it")
		return
	}
	m.App.SetStickyWarning("Agent submit: " + strings.Join(m.grantedEvents, ", ") +
		" — :agent off to revoke")
}

// revokeAgentGrant answers `:agent off`.
//
// Dropping privilege needs no confirmation and cannot fail in a way worth
// reporting: if the registry write fails the grant still goes away in this
// process, and the entry expires with the process regardless.
func (m *Model) revokeAgentGrant() {
	if len(m.grantedEvents) == 0 {
		m.App.SetMessage("Agent submit is already off")
		return
	}
	m.grantedEvents = nil
	if m.store != nil {
		_ = m.store.RevokeGrantForPid()
	}
	m.App.SetMessage("Agent submit revoked")
}

// PrSessionOutput is what `mrman pr <target> --json` prints: enough for an
// agent to attach with `review comments`/`watch` and to know what it is
// looking at, without opening a terminal.
type PrSessionOutput struct {
	Slug      string `json:"slug"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Repo      string `json:"repo"`
	Number    uint64 `json:"number"`
	Title     string `json:"title"`
	HeadSHA   string `json:"head_sha"`
	BaseSHA   string `json:"base_sha"`
	FileCount int    `json:"file_count"`
	ReadOnly  bool   `json:"read_only"`
	// GrantedEvents is always empty here: a headless open cannot issue a
	// grant, because this is the path an agent can invoke. See
	// internal/cli/agentgrant.go.
	GrantedEvents []string `json:"granted_events"`
}

// RunPrHeadless opens a pull-request session without a TUI and prints it as
// JSON, so an agent can start a review without a terminal or a multiplexer.
//
// It performs the same fetch and session open as RunPr — the same session
// file, resumable by the same slug — and then stops. Nothing here writes to
// the forge.
func RunPrHeadless(target string, opts cli.TuiOptions, out io.Writer) error {
	opened, err := openPullRequest(target, opts)
	if err != nil {
		return err
	}
	for _, w := range opened.warnings {
		fmt.Fprintf(os.Stderr, "mrman: %s\n", w)
	}
	details := opened.load.Details

	store, storeErr := persistence.NewDefaultStore()
	if storeErr != nil {
		return fmt.Errorf("sessions are unavailable, so there is nothing to attach to: %w", storeErr)
	}

	fresh := app.NewPrSession(details)
	path, session, err := saveHeadlessPrSession(store, fresh, opened.load.Files)
	if err != nil {
		return err
	}

	sessionSlug := ""
	if s, slugErr := slug.ForSession(session); slugErr == nil {
		sessionSlug = s.String()
	}

	return json.NewEncoder(out).Encode(PrSessionOutput{
		Slug:          sessionSlug,
		Kind:          "pr",
		Path:          path,
		Repo:          details.Repository.Slug(),
		Number:        details.Number,
		Title:         details.Title,
		HeadSHA:       details.HeadSHA,
		BaseSHA:       details.BaseSHA,
		FileCount:     len(opened.load.Files),
		ReadOnly:      details.IsReadOnly(),
		GrantedEvents: []string{},
	})
}

// saveHeadlessPrSession resumes or creates the PR session on disk without
// marking it active: no TUI is holding it, and claiming otherwise would make
// `review list` lie about who is present.
//
// The diff files must be registered exactly as the TUI path registers them,
// or the session has no idea which paths belong to it and `review add`
// rejects every one of them.
func saveHeadlessPrSession(
	store *persistence.Store, fresh *model.ReviewSession, files []model.DiffFile,
) (string, *model.ReviewSession, error) {
	session := fresh
	if fresh.PrSessionKey != nil {
		if existing, found, err := loadExistingPrSession(store, fresh); err == nil && found {
			session = existing
		}
	}
	app.RegisterDiffFiles(session, files)
	path, err := store.SaveSession(session)
	if err != nil {
		return "", nil, err
	}
	return path, session, nil
}

func loadExistingPrSession(
	store *persistence.Store, fresh *model.ReviewSession,
) (*model.ReviewSession, bool, error) {
	_, existing, found, err := store.LoadPrSession(fresh.PrSessionKey)
	return existing, found, err
}
