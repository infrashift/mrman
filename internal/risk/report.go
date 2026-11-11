package risk

import (
	"fmt"
	"io"
	"os"

	vrisk "github.com/infrashift/vizzini-pr-reviewer/pkg/risk"
)

// maxReportBytes bounds a report read from disk. A review's JSON is a few
// kilobytes per finding; anything near this is not one.
const maxReportBytes = 8 << 20

// ReadReport reads a Vizzini review report (pr-review --format json, or a
// marshaled ReviewOutput) from path.
func ReadReport(path string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("risk report: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxReportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("risk report %s: %w", path, err)
	}
	if len(data) > maxReportBytes {
		return nil, fmt.Errorf("risk report %s: larger than %d bytes, too large to be a review", path, maxReportBytes)
	}
	r, err := vrisk.ParseReport(data)
	if err != nil {
		return nil, fmt.Errorf("risk report %s: %w", path, err)
	}
	return r, nil
}

// Stale reports whether r was made for a different diff or policy than the
// one a scored. The report's base score is the same deterministic number
// Assess computes, so a mismatch means the model's half describes something
// else.
func Stale(r *Report, a *Assessment) bool {
	return r.BaseRiskScore != a.Score
}
