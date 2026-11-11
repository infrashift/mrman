package app

import "github.com/infrashift/mrman/internal/risk"

// RiskState is the opt-in Vizzini risk score of the diff on screen. App.Risk
// is nil unless [risk] enabled is set, and every reader checks for that, so
// a reviewer who never turns risk on sees mrman exactly as it is without it.
type RiskState struct {
	// Policy is the policy in force and where it came from.
	Policy *risk.Policy
	// Assessment is the deterministic score of DiffFiles, its Files
	// index-aligned with them. Recomputed whenever the diff changes.
	Assessment risk.Assessment
}

// EnableRisk turns risk scoring on under p and scores the current diff.
func (a *App) EnableRisk(p *risk.Policy) {
	a.Risk = &RiskState{Policy: p}
	a.rescoreRisk()
}

// rescoreRisk recomputes the score after DiffFiles changed. It must run
// after SortFilesByDirectory, since the per-file results are aligned by
// index with the files as displayed.
func (a *App) rescoreRisk() {
	if a.Risk == nil {
		return
	}
	a.Risk.Assessment = risk.Assess(a.DiffFiles, a.Risk.Policy)
}

// FileRisk returns the score of DiffFiles[idx], or false when risk is off,
// idx is out of range, or the entry does not count toward the score.
func (a *App) FileRisk(idx int) (risk.FileScore, bool) {
	if a.Risk == nil || idx < 0 || idx >= len(a.Risk.Assessment.Files) {
		return risk.FileScore{}, false
	}
	s := a.Risk.Assessment.Files[idx]
	return s, s.Counted
}
