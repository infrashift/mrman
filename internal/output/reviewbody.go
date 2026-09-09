package output

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"
)

//go:embed templates/review_body.md.tmpl
var defaultReviewBodyTemplate string

// ReviewBodyComment is one comment rendered into the submitted review body.
type ReviewBodyComment struct {
	Type    string // "" for untyped (marker suppressed)
	Path    string // moved-to-summary items only
	Content string
	Author  string // "" for comments written before mrman stamped authors
	// ShowAuthor is whether Author should be badged, resolved from the same
	// rule the TUI badges by (AuthorVisibility.Shows).
	ShowAuthor bool
}

// AuthorTag returns the comment's "@name" badge, or "" when the author is
// unknown or is not badged for this reader.
func (c ReviewBodyComment) AuthorTag() string {
	return authorTag(c.Author, c.ShowAuthor)
}

// Tag returns the bracket contents for a summary item: the type, the author
// badge, or both joined by a space. Review comments render no type marker, so
// they use AuthorTag directly instead.
func (c ReviewBodyComment) Tag() string {
	tag := c.AuthorTag()
	switch {
	case c.Type == "":
		return tag
	case tag == "":
		return strings.ToUpper(c.Type)
	}
	return strings.ToUpper(c.Type) + " " + tag
}

// ReviewBodyData feeds the review-body template: the summary text posted
// with a forge review submission.
type ReviewBodyData struct {
	ReviewComments []ReviewBodyComment
	MovedToSummary []ReviewBodyComment
}

// LoadReviewBodyTemplate loads the review-body template; an empty
// overridePath returns the embedded default, and unreadable/unparsable
// overrides warn and fall back.
func LoadReviewBodyTemplate(overridePath string) (*template.Template, []string) {
	var warnings []string
	if overridePath != "" {
		raw, err := os.ReadFile(overridePath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"review body template override %s: %v; using embedded default", overridePath, err))
		} else {
			tmpl, parseErr := template.New("review_body").Funcs(funcMap()).Parse(string(raw))
			if parseErr != nil {
				warnings = append(warnings, fmt.Sprintf(
					"review body template override %s: %v; using embedded default", overridePath, parseErr))
			} else {
				return tmpl, warnings
			}
		}
	}
	return template.Must(template.New("review_body").Funcs(funcMap()).Parse(defaultReviewBodyTemplate)), warnings
}

// RenderReviewBody executes the template; an empty result (no review
// comments, nothing moved to summary) renders as "".
func RenderReviewBody(tmpl *template.Template, data *ReviewBodyData) (string, error) {
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
