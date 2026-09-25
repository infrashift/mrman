#!/usr/bin/env bash
# gitlab-live-fixture.sh — build (and tear down) the merge request that
# TestLiveGitLabReview reviews, on any self-hosted GitLab.
#
# The live test needs a merge request that changes one file of every kind —
# modified (with an addition run, a deletion and context), added, deleted,
# renamed — and whose reviewer is the user mrman runs as. This makes one in an
# existing project without touching its default branch: a scratch base branch
# `mrman-e2e-base` carries the "before" files and a head branch
# `mrman-e2e-<stamp>` the "after", so the merge request is head -> base.
#
# Every commit is created with the branch in one call (start_branch) and says
# [skip ci], so a project whose pipeline runs on every push builds nothing.
#
#   GITLAB_URL     http(s)://host[:port]           (required)
#   ADMIN_TOKEN    admin token with api + admin_mode: authors the fixture and
#                  mints the reviewer's impersonation tokens   (required)
#   PROJECT        group/project the fixture lives in          (required)
#   REVIEWER       username mrman reviews as                   (required)
#
#   gitlab-live-fixture.sh up               base branch (once), head branch, merge request
#   gitlab-live-fixture.sh token SCOPE FILE mint a 1-day impersonation token for REVIEWER
#                                           (SCOPE api|read_api) into FILE, mode 0600
#   gitlab-live-fixture.sh down             close fixture MRs, delete mrman-e2e-* branches,
#                                           revoke REVIEWER's mrman-live tokens
#
# Tokens are read from the environment and written only to FILE; nothing here
# prints one.
set -euo pipefail

BASE=mrman-e2e-base
TITLE_PREFIX="mrman live fixture"

# gl METHOD PATH [JSON] — prints the body; fails loudly on a non-2xx status.
gl() {
	local method=$1 path=$2 data=${3:-} out code
	out="$(mktemp)"
	code="$(curl -sS -o "$out" -w '%{http_code}' -X "$method" \
		-H "PRIVATE-TOKEN: $ADMIN_TOKEN" -H 'Content-Type: application/json' \
		${data:+--data "$data"} "$API$path")"
	if [[ $code != 2* ]]; then
		echo "gitlab-live-fixture: $method $path -> HTTP $code: $(head -c 400 "$out")" >&2
		rm -f "$out"
		return 1
	fi
	cat "$out"
	rm -f "$out"
}

# json EXPR — evaluate a Python expression over the JSON on stdin (as `d`).
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

reviewer_id() {
	gl GET "/users?username=$REVIEWER" | json 'd[0]["id"] if d else sys.exit("no such user: '"$REVIEWER"'")'
}

# commit BRANCH START_BRANCH MESSAGE ACTIONS_JSON
commit() {
	gl POST "/projects/$PID/repository/commits" "$(python3 -c '
import json,sys
print(json.dumps({"branch": sys.argv[1], "start_branch": sys.argv[2],
                  "commit_message": sys.argv[3], "actions": json.loads(sys.argv[4])}))' "$@")" \
		| json 'd["id"]'
}

MODIFIED_BEFORE='package fixture

// Cache holds values.
type Cache struct {
	items map[string]string
}

// Get returns a value.
func (c *Cache) Get(k string) string {
	return c.items[k]
}

// Len reports the size.
func (c *Cache) Len() int {
	return len(c.items)
}
'
MODIFIED_AFTER='package fixture

// Cache holds values.
type Cache struct {
	items map[string]string
}

// Get returns the value stored under k.
func (c *Cache) Get(k string) string {
	return c.items[k]
}

// Len reports the size.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	return len(c.items)
}
'
RENAMED_BEFORE='# Notes

line one
line two
line three
line four
'
RENAMED_AFTER='# Notes

line one
line 2
line three
line four
'

actions() {
	python3 -c '
import json,sys
acts=[]
for i in range(1,len(sys.argv),4):
    a={"action":sys.argv[i],"file_path":sys.argv[i+1]}
    if sys.argv[i+2]: a["previous_path"]=sys.argv[i+2]
    if sys.argv[i+3] or sys.argv[i] in ("create","update","move"): a["content"]=sys.argv[i+3]
    acts.append(a)
print(json.dumps(acts))' "$@"
}

cmd_up() {
	local default_branch stamp head rid
	default_branch="$(gl GET "/projects/$PID" | json 'd["default_branch"]')"
	if ! gl GET "/projects/$PID/repository/branches/$BASE" >/dev/null 2>&1; then
		commit "$BASE" "$default_branch" "[skip ci] $TITLE_PREFIX: base" "$(actions \
			create mrman-e2e/cache.go '' "$MODIFIED_BEFORE" \
			create mrman-e2e/obsolete.txt '' $'to be deleted\n' \
			create mrman-e2e/notes-old.md '' "$RENAMED_BEFORE")" >/dev/null
		echo "created $BASE from $default_branch" >&2
	fi
	stamp="$(date -u +%Y%m%dT%H%M%SZ)"
	head="mrman-e2e-$stamp"
	commit "$head" "$BASE" "[skip ci] $TITLE_PREFIX: head $stamp" "$(actions \
		update mrman-e2e/cache.go '' "$MODIFIED_AFTER" \
		delete mrman-e2e/obsolete.txt '' '' \
		move mrman-e2e/notes-new.md mrman-e2e/notes-old.md "$RENAMED_AFTER" \
		create mrman-e2e/added.txt '' $'brand new\nsecond line\n')" >/dev/null
	rid="$(reviewer_id)"
	gl POST "/projects/$PID/merge_requests" "$(python3 -c '
import json,sys
print(json.dumps({"source_branch": sys.argv[1], "target_branch": sys.argv[2],
                  "title": sys.argv[3], "reviewer_ids": [int(sys.argv[4])],
                  "remove_source_branch": False}))' "$head" "$BASE" "$TITLE_PREFIX $stamp" "$rid")" \
		| json 'd["web_url"]'
}

cmd_token() {
	local scope=${1:?scope: api or read_api} file=${2:?output file} rid expires
	rid="$(reviewer_id)"
	expires="$(date -u -d tomorrow +%Y-%m-%d 2>/dev/null || date -u -v+1d +%Y-%m-%d)"
	umask 077
	gl POST "/users/$rid/impersonation_tokens" "$(python3 -c '
import json,sys
print(json.dumps({"name": "mrman-live-"+sys.argv[1], "scopes": [sys.argv[1]], "expires_at": sys.argv[2]}))' "$scope" "$expires")" \
		| json 'd["token"]' >"$file"
	chmod 600 "$file"
	echo "wrote a $scope token for $REVIEWER to $file (expires $expires)" >&2
}

cmd_down() {
	local rid
	for iid in $(gl GET "/projects/$PID/merge_requests?state=opened&per_page=100" |
		json '" ".join(str(m["iid"]) for m in d if m["title"].startswith("'"$TITLE_PREFIX"'"))'); do
		gl PUT "/projects/$PID/merge_requests/$iid" '{"state_event":"close"}' >/dev/null
		echo "closed !$iid" >&2
	done
	for b in $(gl GET "/projects/$PID/repository/branches?search=%5Emrman-e2e-&per_page=100" |
		json '" ".join(b["name"] for b in d)'); do
		gl DELETE "/projects/$PID/repository/branches/$b" >/dev/null
		echo "deleted branch $b" >&2
	done
	rid="$(reviewer_id)"
	for id in $(gl GET "/users/$rid/impersonation_tokens?state=active&per_page=100" |
		json '" ".join(str(t["id"]) for t in d if t["name"].startswith("mrman-live-"))'); do
		gl DELETE "/users/$rid/impersonation_tokens/$id" >/dev/null
		echo "revoked token $id" >&2
	done
}

usage() { sed -n '2,28p' "$0" >&2; exit 2; }
[[ ${1:-} =~ ^(up|token|down)$ ]] || usage
: "${GITLAB_URL:?}" "${ADMIN_TOKEN:?}" "${PROJECT:?}" "${REVIEWER:?}"
API="${GITLAB_URL%/}/api/v4"
PID="$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$PROJECT")"

case "$1" in
up) cmd_up ;;
token) shift && cmd_token "$@" ;;
down) cmd_down ;;
*) usage ;;
esac
