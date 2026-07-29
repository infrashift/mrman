package cli

import (
	"strings"
	"testing"
)

func TestBareAutoGrantsOnlyTheSafeEvents(t *testing.T) {
	events, err := ParseAutoGrant("")
	if err != nil {
		t.Fatal(err)
	}
	want := "comment,draft"
	if strings.Join(events, ",") != want {
		t.Errorf("bare --auto granted %v, want %s", events, want)
	}
	// Approving is what gets code merged; it must never be implied.
	for _, e := range events {
		if e == "approve" || e == "request-changes" {
			t.Errorf("bare --auto must not grant %q", e)
		}
	}
}

func TestAutoGrantAcceptsAnExplicitList(t *testing.T) {
	events, err := ParseAutoGrant("approve, comment ,approve")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "approve,comment" {
		t.Errorf("got %v, want a sorted deduplicated approve,comment", events)
	}
}

func TestAutoGrantRejectsAnUnknownEvent(t *testing.T) {
	// A typo must fail loudly. Silently narrowing would leave the user
	// believing they granted something they did not.
	_, err := ParseAutoGrant("comment,aprove")
	if err == nil {
		t.Fatal("a misspelled event must be rejected")
	}
	if !strings.Contains(err.Error(), "aprove") || !strings.Contains(err.Error(), "approve") {
		t.Errorf("the error must name both the typo and the valid set, got %v", err)
	}
}

func TestAutoGrantRejectsAnEmptyList(t *testing.T) {
	if _, err := ParseAutoGrant(" , "); err == nil {
		t.Error("--auto with no events must be an error, not a silent no-op")
	}
}

// TestAutoRequiresATerminal pins the second half of the interlock: an agent
// shelling out has no TTY, so it cannot reach the interactive grant path.
func TestAutoRequiresATerminal(t *testing.T) {
	restore := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = restore })

	stdinIsTerminal = func() bool { return false }
	_, err := Parse([]string{"pr", "1", "--auto"})
	if err == nil {
		t.Fatal("--auto without a terminal must be refused")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("the refusal must say why, got %v", err)
	}

	stdinIsTerminal = func() bool { return true }
	args, err := Parse([]string{"pr", "1", "--auto"})
	if err != nil {
		t.Fatalf("--auto at a terminal must be accepted: %v", err)
	}
	if !args.Tui.AutoSet || len(args.Tui.GrantedEvents) == 0 {
		t.Errorf("a terminal --auto must record a grant, got %+v", args.Tui)
	}
}

// TestAutoIsRefusedWithJSON is the load-bearing exclusion: --json is the
// path an agent can invoke, so it must never be able to carry a grant.
func TestAutoIsRefusedWithJSON(t *testing.T) {
	restore := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = restore })
	stdinIsTerminal = func() bool { return true }

	_, err := Parse([]string{"pr", "1", "--json", "--auto"})
	if err == nil {
		t.Fatal("--json with --auto must be refused even at a terminal")
	}
	if !strings.Contains(err.Error(), "json") || !strings.Contains(err.Error(), "auto") {
		t.Errorf("the refusal must name both flags, got %v", err)
	}
}

func TestNoAutoMeansNoGrant(t *testing.T) {
	args, err := Parse([]string{"pr", "1"})
	if err != nil {
		t.Fatal(err)
	}
	if args.Tui.AutoSet || len(args.Tui.GrantedEvents) != 0 {
		t.Errorf("omitting --auto must grant nothing, got %+v", args.Tui)
	}
}

func TestJSONAloneIsFine(t *testing.T) {
	args, err := Parse([]string{"pr", "1", "--json"})
	if err != nil {
		t.Fatalf("--json alone must be accepted: %v", err)
	}
	if !args.Tui.JSON {
		t.Error("--json must be recorded")
	}
	if len(args.Tui.GrantedEvents) != 0 {
		t.Error("a headless open must never carry a grant")
	}
}
