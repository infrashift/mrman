// Tests for mrman/agent-submit. Run with:
//
//	garmr test policies/garmr/mrman/agent-submit.cue policies/garmr/mrman/agent-submit_test.cue

_input: {
	apiVersion: "mrman.infrashift.io/v1"
	kind:       "AgentSubmit"
	event:      string | *"comment"
	identity:   string | *"vizzini-bot"
	forge:      "azuredevops"
	pr:         "ado:dev.azure.com/o/p/r#12"
	comments:   int | *10
	run_id:     "01a1cbce-d804-7e69-a0cb-9c0c7861e829"
	agent:      "vizzini-bot/v0.1.0"
}

policy: "mrman/agent-submit"

tests: [
	{
		name:  "the bot may post a comment review"
		input: _input
		expect: {decision: "allow", noViolations: true}
	},
	{
		name:  "the bot may resolve the threads it opened"
		input: _input & {event: "resolve", comments: 3}
		expect: {decision: "allow", noViolations: true}
	},
	{
		name:  "the bot may reply in a thread"
		input: _input & {event: "reply", comments: 1}
		expect: {decision: "allow", noViolations: true}
	},
	{
		name:  "draft is not allowed unattended by default"
		input: _input & {event: "draft"}
		expect: {decision: "deny", violations: [{id: "MRM-001"}]}
	},
	{
		name:  "approve is never unattended by default"
		input: _input & {event: "approve"}
		expect: {decision: "deny", violations: [{id: "MRM-001"}]}
	},
	{
		name:  "request-changes is never unattended by default"
		input: _input & {event: "request-changes"}
		expect: {decision: "deny", violations: [{id: "MRM-001"}]}
	},
	{
		name:  "another identity may not post"
		input: _input & {identity: "alice"}
		expect: {decision: "deny", violations: [{id: "MRM-002"}]}
	},
	{
		name:  "another identity may not resolve"
		input: _input & {event: "resolve", identity: "alice"}
		expect: {decision: "deny", violations: [{id: "MRM-002"}]}
	},
	{
		name:  "a flood of comments is refused"
		input: _input & {comments: 51}
		expect: {decision: "deny", violations: [{id: "MRM-003"}]}
	},
	{
		name: "a submit without a run id is refused"
		input: {
			apiVersion: "mrman.infrashift.io/v1"
			kind:       "AgentSubmit"
			event:      "comment"
			identity:   "vizzini-bot"
			forge:      "azuredevops"
			pr:         "ado:dev.azure.com/o/p/r#12"
			comments:   1
		}
		expect: {decision: "deny", violations: [{id: "MRM-004"}]}
	},
]
