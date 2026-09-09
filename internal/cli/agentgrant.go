// agentgrant.go parses `--auto`, the flag by which a human authorizes an
// agent to submit reviews from the CLI during their TUI session.
//
// Two properties keep the flag from being self-granted by the agent that
// would benefit from it, and both are load-bearing rather than defensive
// habit:
//
//   - It is refused alongside --json. The headless path is the one an agent
//     can invoke; if it could also issue a grant, the agent would grant
//     itself and the interlock would mean nothing.
//   - It requires a terminal on stdin. A shelled-out invocation has no TTY,
//     so an agent cannot quietly take the interactive path instead.
//
// Neither is a security boundary — an agent with a shell can allocate a pty
// — but together they mean a grant cannot appear by accident or by an agent
// deciding it would be convenient.

package cli

import (
	"fmt"
	"sort"
	"strings"
)

// grantableEvents are the submit events --auto can authorize, mapped to
// whether they are in the default set.
//
// comment and draft are the default because neither changes a pull
// request's fate: a comment review is commentary, and a draft is invisible
// to the author until a human releases it. approve and request-changes
// alter whether code merges, so they must be named explicitly.
var grantableEvents = map[string]bool{
	"comment":         true,
	"draft":           true,
	"approve":         false,
	"request-changes": false,
}

// DefaultGrantedEvents is what a bare --auto authorizes.
func DefaultGrantedEvents() []string {
	var out []string
	for event, isDefault := range grantableEvents {
		if isDefault {
			out = append(out, event)
		}
	}
	sort.Strings(out)
	return out
}

// ParseAutoGrant turns the --auto value into the set of authorized events.
//
// A bare --auto (empty value) yields the default set. An explicit list
// replaces it entirely. An unrecognized event is a hard error rather than a
// silent narrowing: a user who mistypes "aprove" must not end up believing
// they granted approval.
func ParseAutoGrant(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return DefaultGrantedEvents(), nil
	}
	seen := map[string]bool{}
	var out []string
	for raw := range strings.SplitSeq(value, ",") {
		event := strings.ToLower(strings.TrimSpace(raw))
		if event == "" {
			continue
		}
		if _, ok := grantableEvents[event]; !ok {
			return nil, fmt.Errorf(
				"--auto: unknown submit event %q (valid: %s)", event, strings.Join(knownEvents(), ", "))
		}
		if !seen[event] {
			seen[event] = true
			out = append(out, event)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--auto: no submit events named; omit the flag to grant nothing")
	}
	sort.Strings(out)
	return out, nil
}

func knownEvents() []string {
	out := make([]string, 0, len(grantableEvents))
	for event := range grantableEvents {
		out = append(out, event)
	}
	sort.Strings(out)
	return out
}
