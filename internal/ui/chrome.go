package ui

import (
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/render"
	"github.com/infrashift/mrman/internal/theme"
)

// Header renders the 1-row top bar: brand left, context chunks right.
func Header(a *app.App, t *theme.Theme, width int) string {
	emitter := &render.Emitter{}
	brand := render.Span{Text: " mrman ", Style: render.Style{Fg: t.FgPrimary, Bg: t.StatusBarBg, Bold: true}}

	var chunks []string
	if a.VcsInfo != nil {
		branch := "detached"
		if a.VcsInfo.BranchName != nil {
			branch = *a.VcsInfo.BranchName
		}
		chunks = append(chunks, fmt.Sprintf("%s:%s", a.VcsInfo.Type, branch))
	}
	if src := headerSourceChunk(a); src != "" {
		chunks = append(chunks, src)
	}
	if a.IsSingleFileView {
		chunks = append(chunks, "FOCUS")
	}
	if a.IsPristineMode {
		chunks = append(chunks, fmt.Sprintf("PRISTINE · %d files", len(a.DiffFiles)))
	}

	right := " " + strings.Join(chunks, " · ") + " "
	pad := width - render.StringWidth(brand.Text) - render.StringWidth(right)
	if pad < 0 {
		pad = 0
	}
	return emitter.Line([]render.Span{
		brand,
		{Text: strings.Repeat(" ", pad), Style: render.Style{Bg: t.StatusBarBg}},
		{Text: right, Style: render.Style{Fg: t.FgSecondary, Bg: t.StatusBarBg}},
	})
}

func headerSourceChunk(a *app.App) string {
	switch a.DiffSource.Kind {
	case app.DiffSourceStaged:
		return "staged"
	case app.DiffSourceUnstaged:
		return "unstaged"
	case app.DiffSourceStagedAndUnstaged:
		return "staged + unstaged"
	case app.DiffSourceCommitRange:
		if n := len(a.DiffSource.Commits); n == 1 {
			return "commit " + shortSHA(a.DiffSource.Commits[0])
		} else if n > 1 {
			return fmt.Sprintf("%d commits", n)
		}
	case app.DiffSourceStagedUnstagedAndCommits:
		if n := len(a.DiffSource.Commits); n == 1 {
			return "staged + unstaged + commit " + shortSHA(a.DiffSource.Commits[0])
		} else if n > 1 {
			return fmt.Sprintf("staged + unstaged + %d commits", n)
		}
	}
	return ""
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// modeChipText returns the status-bar mode chip for a mode.
func modeChipText(a *app.App) string {
	switch a.InputMode {
	case input.ModeNormal:
		if a.PendingCount != nil && *a.PendingCount > 0 {
			return fmt.Sprintf(" NORMAL %d ", *a.PendingCount)
		}
		return " NORMAL "
	case input.ModeCommand:
		return " COMMAND "
	case input.ModeSearch:
		return " SEARCH "
	case input.ModeComment:
		return " COMMENT "
	case input.ModeHelp:
		return " HELP "
	case input.ModeConfirm:
		return " CONFIRM "
	case input.ModeCommitSelect:
		return " SELECT "
	case input.ModeVisualSelect:
		return " VISUAL "
	case input.ModeSubmitResolver:
		return " RESOLVE "
	case input.ModeSubmitConfirm, input.ModeSubmitActionPicker:
		return " SUBMIT "
	}
	return " ? "
}

func modeHint(a *app.App) string {
	switch a.InputMode {
	case input.ModeNormal:
		return "   j/k scroll · {/} file · m/M comment · r file · R hunk · c comment · ? help"
	case input.ModeCommand:
		return "   tab complete · ↵ execute · esc cancel"
	case input.ModeSearch:
		return "   ↵ search · esc cancel"
	case input.ModeComment:
		return "   ctrl-s save · esc cancel"
	case input.ModeHelp:
		return "   / search · n/N match · q/?/esc close"
	case input.ModeConfirm:
		return "   y yes · n no"
	case input.ModeCommitSelect:
		return "   j/k navigate · space select · ↵ confirm · esc back"
	case input.ModeVisualSelect:
		return "   j/k extend · c/↵ comment · y yank · esc/V cancel"
	}
	return ""
}

// StatusBar renders the 1-row bottom bar: prompt or mode chip + hint left,
// message right.
func StatusBar(a *app.App, t *theme.Theme, width int) string {
	emitter := &render.Emitter{}
	var left []render.Span
	switch a.InputMode {
	case input.ModeCommand:
		left = []render.Span{{Text: ":" + a.CommandBuffer, Style: render.Style{Fg: t.FgPrimary, Bg: t.StatusBarBg}}}
	case input.ModeSearch:
		left = []render.Span{{Text: "/" + a.SearchBuffer, Style: render.Style{Fg: t.FgPrimary, Bg: t.StatusBarBg}}}
	default:
		left = []render.Span{
			{Text: modeChipText(a), Style: render.Style{Fg: t.ModeFg, Bg: t.ModeBg, Bold: true}},
		}
		if a.Message == nil {
			left = append(left, render.Span{Text: modeHint(a), Style: render.Style{Fg: t.FgSecondary, Bg: t.StatusBarBg}})
		}
	}

	var right render.Span
	if a.Message != nil {
		fg, bg := t.MessageInfoFg, t.MessageInfoBg
		switch a.Message.Type {
		case app.MessageWarning:
			fg, bg = t.MessageWarningFg, t.MessageWarningBg
		case app.MessageError:
			fg, bg = t.MessageErrorFg, t.MessageErrorBg
		}
		right = render.Span{Text: " " + a.Message.Content + " ", Style: render.Style{Fg: fg, Bg: bg, Bold: true}}
	}

	used := render.SpanWidth(left) + render.StringWidth(right.Text)
	pad := width - used
	if pad < 0 {
		pad = 0
	}
	spans := append(left, render.Span{Text: strings.Repeat(" ", pad), Style: render.Style{Bg: t.StatusBarBg}})
	if right.Text != "" {
		spans = append(spans, right)
	}
	return emitter.Line(spans)
}
