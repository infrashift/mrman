// mrman's unattended-submit policy for garmr (Vizzini ADR-0016).
//
// `mrman review submit --grant-policy DIR` posts a review without a human at
// a terminal only when garmr allows it. mrman builds one AgentSubmit input
// per submit and evaluates DIR in-process:
//
//   garmr eval --policy-dir DIR -n mrman -o json --input -
//
// A human grant (`mrman pr <target> --auto`) is the interactive way; this
// file is the unattended one. Commit it, review changes to it like code: the
// review of this file is the grant. Copy the pack into your repository and
// set the values below. DENY when any rule fails.
package mrman

// Events the bot may submit. approve and request-changes decide whether code
// merges; list them only deliberately.
_allowedEvents: ["comment"]

// Forge identities (the login the token authenticates as, as the forge
// reports it; on Azure DevOps the display name) allowed to submit.
_allowedIdentities: ["vizzini-bot"]

// Most comments one submit may post: a runaway review should fail, not flood.
_maxComments: 50

_target: resources: [{apiGroup: "mrman.infrashift.io", kind: "AgentSubmit"}]

agentSubmit: {
	apiVersion: "policy.garmr.io/v1"
	kind:       "Policy"
	metadata: {
		name:      "agent-submit"
		namespace: "mrman"
	}
	spec: {
		description: "Lets a named bot identity post comment reviews unattended; nothing that approves or blocks a merge."
		target:      _target
		rules: [
			{
				id:          "MRM-001"
				description: "Only allowed submit events"
				severity:    "critical"
				priority:    10
				expr: match: {path: "event", in: _allowedEvents}
				message:     "event {{event}} is not allowed unattended"
				remediation: "Submit as comment, or have a human submit with mrman pr --auto."
				category:    "authorization"
			},
			{
				id:          "MRM-002"
				description: "Only allowed bot identities"
				severity:    "critical"
				priority:    20
				expr: match: {path: "identity", in: _allowedIdentities}
				message:     "identity {{identity}} may not submit unattended"
				remediation: "Run the bot with its own token, or add its identity to the policy in a reviewed change."
				category:    "authorization"
			},
			{
				id:          "MRM-003"
				description: "At most _maxComments comments per submit"
				severity:    "high"
				priority:    30
				expr: match: {path: "comments", lessThanOrEqual: _maxComments}
				message:     "{{comments}} comments exceed the unattended limit"
				remediation: "Review the run: a flood of comments usually means a runaway review."
				category:    "authorization"
			},
			{
				id:          "MRM-004"
				description: "Every unattended submit names its run"
				severity:    "high"
				priority:    40
				expr: match: {path: "run_id", exists: true}
				message:     "no run_id: the submit could not be traced to the run that produced it"
				remediation: "Pass --run-id (vizzini-bot does)."
				category:    "audit"
			},
		]
		enforcement: action: "deny"
	}
}
