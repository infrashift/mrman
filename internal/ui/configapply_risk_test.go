package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/risk"
	"github.com/infrashift/mrman/internal/vcs"
)

func TestApplyRiskOffLeavesAppUntouched(t *testing.T) {
	m := testModel(t)
	if w := applyRisk(config.Default(), m.App); w != nil || m.App.Risk != nil {
		t.Errorf("risk off: warnings %v, state %+v", w, m.App.Risk)
	}
}

func TestApplyRiskUsesTheRepositoryPolicy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".vizzini"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vizzini", "risk.json"), []byte(`{"high_threshold": 90}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.App.VcsInfo = &vcs.Info{RootPath: root}
	cfg := config.Default()
	cfg.Risk = config.RiskConfig{Enabled: true, Policy: filepath.Join(root, "missing.json")}

	w := applyRisk(cfg, m.App)
	if m.App.Risk == nil || m.App.Risk.Policy.Source != risk.SourceRepo {
		t.Fatalf("risk state %+v", m.App.Risk)
	}
	// The repo policy won, so the missing config file was never read.
	if len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	if len(m.App.Risk.Assessment.Files) != len(m.App.DiffFiles) {
		t.Error("the open diff was not scored")
	}

	m.App.VcsInfo = nil
	if w := applyRisk(cfg, m.App); len(w) != 1 || m.App.Risk.Policy.Source != risk.SourceDefault {
		t.Errorf("no checkout, missing config: warnings %v, source %s", w, m.App.Risk.Policy.Source)
	}
}
