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
