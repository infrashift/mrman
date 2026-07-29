package theme

import (
	"image/color"

	lipgloss "charm.land/lipgloss/v2"
)

// Styles is the materialized lipgloss style vocabulary for the TUI chrome,
// a port of tuicr's src/ui/styles.rs. Slots whose theme color is nil
// (terminal default / transparent background) leave the corresponding
// foreground or background unset.
type Styles struct {
	Selected             lipgloss.Style
	Dim                  lipgloss.Style
	DiffAdd              lipgloss.Style
	DiffDel              lipgloss.Style
	DiffContext          lipgloss.Style
	ExpandedContext      lipgloss.Style
	HunkHeader           lipgloss.Style
	FileHeader           lipgloss.Style
	Reviewed             lipgloss.Style
	Pending              lipgloss.Style
	Panel                lipgloss.Style
	Popup                lipgloss.Style
	StatusBar            lipgloss.Style
	Mode                 lipgloss.Style
	CurrentLineIndicator lipgloss.Style
	Hash                 lipgloss.Style
	Branch               lipgloss.Style
	DirIcon              lipgloss.Style
	VisualSelection      lipgloss.Style
	HelpIndicator        lipgloss.Style
	RangeBar             lipgloss.Style
	ErrorInline          lipgloss.Style
	PseudoCommitTag      lipgloss.Style

	BorderFocused   lipgloss.Style
	BorderUnfocused lipgloss.Style
}

// fg applies a foreground color to a style, skipping nil (terminal
// default).
func fg(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Foreground(c)
}

// bg applies a background color to a style, skipping nil (transparent).
func bg(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Background(c)
}

// NewStyles materializes the style vocabulary from a theme.
func NewStyles(t *Theme) Styles {
	base := lipgloss.NewStyle()
	panel := bg(fg(base, t.FgPrimary), t.PanelBg)
	return Styles{
		Selected:             bg(fg(base, t.FgPrimary), t.BgHighlight),
		Dim:                  fg(base, t.FgDim),
		DiffAdd:              bg(fg(base, t.DiffAdd), t.DiffAddBg),
		DiffDel:              bg(fg(base, t.DiffDel), t.DiffDelBg),
		DiffContext:          fg(base, t.DiffContext),
		ExpandedContext:      fg(base, t.ExpandedContextFg),
		HunkHeader:           bg(fg(base, t.FgDim), t.SectionHighlightBg()),
		FileHeader:           fg(base, t.FgPrimary).Bold(true),
		Reviewed:             fg(base, t.Reviewed),
		Pending:              fg(base, t.Pending),
		Panel:                panel,
		Popup:                panel,
		StatusBar:            bg(fg(base, t.FgPrimary), t.StatusBarBg),
		Mode:                 bg(fg(base, t.ModeFg), t.ModeBg).Bold(true),
		CurrentLineIndicator: fg(base, t.BorderFocused),
		Hash:                 fg(base, t.CursorColor),
		Branch:               fg(base, t.BranchName),
		DirIcon:              fg(base, t.DiffHunkHeader),
		VisualSelection:      bg(base, t.BgHighlight),
		HelpIndicator:        bg(fg(base, t.HelpIndicator), t.PanelBg),
		RangeBar:             fg(base, t.BorderFocused),
		ErrorInline:          fg(base, t.MessageErrorFg).Bold(true),
		PseudoCommitTag:      fg(base, t.FileModified),

		BorderFocused:   fg(base, t.BorderFocused),
		BorderUnfocused: fg(base, t.BorderUnfocused),
	}
}

// Border returns the focused or unfocused border style.
func (s *Styles) Border(focused bool) lipgloss.Style {
	if focused {
		return s.BorderFocused
	}
	return s.BorderUnfocused
}

// FileStatusColor maps a git status character to its theme color:
// 'A' added, 'M' modified, 'D' deleted, 'R' renamed; anything else falls
// back to the secondary foreground.
func FileStatusColor(t *Theme, statusChar byte) color.Color {
	switch statusChar {
	case 'A':
		return t.FileAdded
	case 'M':
		return t.FileModified
	case 'D':
		return t.FileDeleted
	case 'R':
		return t.FileRenamed
	default:
		return t.FgSecondary
	}
}

// authorPalette is the fixed palette used to tint comment chrome by author.
// It excludes red/green so the color never collides with diff add/del
// semantics; the ANSI indices match tuicr's ratatui palette
// [Cyan, Yellow, Magenta, Blue, LightMagenta, LightCyan] = 6,3,5,4,13,14.
var authorPalette = []color.Color{
	lipgloss.Cyan,          // ANSI 6
	lipgloss.Yellow,        // ANSI 3
	lipgloss.Magenta,       // ANSI 5
	lipgloss.Blue,          // ANSI 4
	lipgloss.BrightMagenta, // ANSI 13
	lipgloss.BrightCyan,    // ANSI 14
}

// AuthorColorFor returns the deterministic palette color for an author
// name, hashed with FNV-1a so the mapping is platform-independent and
// stable across runs. Callers gate visibility separately via AuthorAccent.
func AuthorColorFor(author string) color.Color {
	hash := uint64(0xcbf29ce484222325)
	for i := 0; i < len(author); i++ {
		hash ^= uint64(author[i])
		hash *= 0x100000001b3
	}
	return authorPalette[hash%uint64(len(authorPalette))]
}

// AuthorAccent returns the accent color for a comment author when the
// comment chrome should advertise authorship. ok is false when the author
// is the viewer or empty (the comment is the viewer's own / unattributed).
func AuthorAccent(viewer, author string) (color.Color, bool) {
	if author == "" || author == viewer {
		return nil, false
	}
	return AuthorColorFor(author), true
}
