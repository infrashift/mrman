// Package risk adapts Vizzini's deterministic, LLM-free risk score
// (github.com/infrashift/vizzini-pr-reviewer/pkg/risk) to mrman's diff model.
//
// It is the only mrman package allowed to import Vizzini, and it imports only
// pkg/risk, which links no model client, tokenizer or network code. Risk is
// opt-in ([risk] enabled in config.toml): when it is off, nothing calls this
// package and mrman behaves exactly as it does without it.
//
// mrman never runs a model. A score computed here is the review's
// base_risk_score; the model's half comes only from a report file someone
// else produced (LoadReport), and its auto_approve is shown as a suggestion,
// never acted on.
package risk

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	vrisk "github.com/infrashift/vizzini-pr-reviewer/pkg/risk"

	"github.com/infrashift/mrman/internal/model"
)

// Re-exported Vizzini types, so callers outside this package never import
// Vizzini themselves.
type (
	// Band is a coarse reading of a score: low, medium or high.
	Band = vrisk.Band
	// FileRisk is one file's contribution to the score.
	FileRisk = vrisk.FileRisk
	// Report is the risk part of a review someone else ran.
	Report = vrisk.Report
	// Finding is one finding of that review, as Vizzini wrote it.
	Finding = vrisk.Finding
)

// Bands, lowest first.
const (
	BandLow    = vrisk.BandLow
	BandMedium = vrisk.BandMedium
	BandHigh   = vrisk.BandHigh
)

// PolicySource says where the policy in force came from.
type PolicySource string

// Policy sources, in resolution order.
const (
	// SourceRepo is the repository's own .vizzini/risk.json.
	SourceRepo PolicySource = "repo"
	// SourceConfig is the file named by [risk] policy in config.toml.
	SourceConfig PolicySource = "config"
	// SourceDefault is Vizzini's built-in policy.
	SourceDefault PolicySource = "default"
)

// Policy is a validated Vizzini risk policy plus where it came from and the
// path excludes a review applies.
type Policy struct {
	policy  *vrisk.Policy
	exclude *vrisk.Excluder
	// Source is where the policy came from.
	Source PolicySource
	// Path is the file it was read from, empty for SourceDefault.
	Path string
}

// DefaultPolicy is Vizzini's built-in policy with the default excludes.
func DefaultPolicy() *Policy {
	p, err := newPolicy(vrisk.DefaultPolicy(), SourceDefault, "")
	if err != nil {
		// The default exclude list is a constant Vizzini tests; reaching
		// this means the dependency itself is broken.
		panic("risk: default excludes: " + err.Error())
	}
	return p
}

func newPolicy(p *vrisk.Policy, source PolicySource, path string) (*Policy, error) {
	ex, err := vrisk.NewExcluder(nil)
	if err != nil {
		return nil, err
	}
	return &Policy{policy: p, exclude: ex, Source: source, Path: path}, nil
}

type candidate struct {
	path   string
	source PolicySource
}

// ResolvePolicy picks the policy in force: the repository's
// .vizzini/risk.json under repoRoot, then the file at configPath, then
// Vizzini's defaults. An empty repoRoot or configPath skips that step. A
// candidate that exists but does not validate is reported as a warning and
// skipped, so a broken policy never hides the score: the next one applies.
func ResolvePolicy(repoRoot, configPath string) (*Policy, []string) {
	var warnings []string
	var candidates []candidate
	if repoRoot != "" {
		candidates = append(candidates, candidate{filepath.Join(repoRoot, filepath.FromSlash(vrisk.RepoPolicyPath)), SourceRepo})
	}
	if configPath != "" {
		candidates = append(candidates, candidate{configPath, SourceConfig})
	}
	for _, c := range candidates {
		data, err := os.ReadFile(c.path)
		if err != nil {
			// A repository without its own policy is the common case and
			// not worth a warning; a configured file that is missing is.
			if c.source == SourceRepo && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("risk policy %s: %v — skipped", c.path, err))
			continue
		}
		vp, err := vrisk.LoadPolicy(data)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("risk policy %s: %v — skipped", c.path, err))
			continue
		}
		p, err := newPolicy(vp, c.source, c.path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("risk policy %s: %v — skipped", c.path, err))
			continue
		}
		return p, warnings
	}
	return DefaultPolicy(), warnings
}

// Band reads a score against the policy's thresholds.
func (p *Policy) Band(score int) Band {
	return p.policy.Band(score)
}

// IsSensitive reports whether a path weight marks a sensitive file, one a
// review never auto-approves.
func (p *Policy) IsSensitive(weight float64) bool {
	return p.policy.IsSensitive(weight)
}

// Assessment is the deterministic score of a diff in mrman's file order.
type Assessment struct {
	// Score is 0..100 and equals the base_risk_score a Vizzini review of
	// the same diff under the same policy reports.
	Score int
	// Band is Score read against the policy.
	Band Band
	// Files is index-aligned with the scored []model.DiffFile. An entry
	// that does not count (the commit-message pseudo-file, or a path a
	// review excludes such as go.sum) has Counted false and zero points.
	Files []FileScore
}

// FileScore is one diff entry's part of an Assessment.
type FileScore struct {
	FileRisk
	// Counted is false for entries left out of the score.
	Counted bool
	// Sensitive marks a counted file whose path weight reaches the
	// policy's sensitive_weight.
	Sensitive bool
}

// Assess scores files under p. Results stay aligned with files by index, so
// a patch series that lists one path twice gets one score per entry.
func Assess(files []model.DiffFile, p *Policy) Assessment {
	a := Assessment{Files: make([]FileScore, len(files))}
	counted := make([]FileRisk, 0, len(files))
	for i := range files {
		f := &files[i]
		if f.IsCommitMessage {
			continue
		}
		path := f.DisplayPath()
		if path == "" || p.exclude.Excluded(path) {
			continue
		}
		change := toChange(f, path)
		fr := vrisk.ScoreFile(&change, p.policy)
		a.Files[i] = FileScore{FileRisk: fr, Counted: true, Sensitive: p.IsSensitive(fr.Weight)}
		counted = append(counted, fr)
	}
	a.Score = vrisk.Combine(counted, p.policy)
	a.Band = p.Band(a.Score)
	return a
}

// toChange reduces a diff entry to what the score reads. Line text comes
// from Raw (the source line, prefix stripped) so it matches what Vizzini
// reads from the same patch; Content, which has tabs expanded, stands in for
// lines mrman synthesized without a Raw.
func toChange(f *model.DiffFile, path string) vrisk.FileChange {
	c := vrisk.FileChange{Path: path, Binary: f.IsBinary}
	for h := range f.Hunks {
		for _, l := range f.Hunks[h].Lines {
			text := l.Content
			if l.Raw != "" {
				text = l.Raw[1:]
			}
			switch l.Origin {
			case model.OriginAddition:
				c.Added = append(c.Added, text)
			case model.OriginDeletion:
				c.Deleted = append(c.Deleted, text)
			case model.OriginContext:
			}
		}
	}
	return c
}
