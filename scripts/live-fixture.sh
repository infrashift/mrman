#!/usr/bin/env bash
# live-fixture.sh — build (and tear down) the merge request mrman's live tests
# review, on GitHub, GitLab (gitlab.com or self-managed), Azure DevOps or
# Forgejo (Codeberg).
#
# The fixture is built with git in a throwaway clone, so it is the same on
# every forge and never touches your own checkout or the default branch. A
# base branch `mrman-e2e-base` carries the "before" files and a head branch
# `mrman-e2e-<stamp>` the "after", in two commits; the merge request is
# head -> base. After it opens, the base branch moves once more, so the
# merge request's base and the target branch's tip differ.
#
# The head changes one file of every kind TestLiveGitLabReview needs —
# modified (an addition run, a deletion, context), added, deleted, renamed —
# plus files for the parser and driver edge cases:
#
#   edge/config.yaml   deletes a YAML "---" line (the diff row is "----")
#   edge/schema.sql    deletes an SQL "-- " comment (the diff row is "--- ")
#   edge/counter.c     adds "++i;" (the diff row is "+++i;")
#   edge/café.md       a non-ASCII path, which git quotes in diff headers
#   edge/rename-*.go   renamed and edited, for old-side comments on renames
#   edge/long.go       a mid-file change with hidden context on both sides
#   edge/shared.txt    also changed on the base branch after the MR opens
#
#   live-fixture.sh URL up      push the branches and open the merge request
#   live-fixture.sh URL down    close fixture merge requests, delete mrman-e2e-* branches
#
# URL is the repository's https clone URL:
#   https://github.com/OWNER/REPO
#   https://gitlab.com/GROUP[/SUB]/REPO           (or a self-managed host)
#   https://dev.azure.com/ORG/PROJECT/_git/REPO
#   https://codeberg.org/OWNER/REPO               (Forgejo; FORGE=forgejo for other hosts)
#
# Credentials come from the environment and are passed to git through a
# credential helper, never on a command line:
#   GitHub        gh auth (gh must be logged in)
#   GitLab        GITLAB_TOKEN   (api scope)
#   Azure DevOps  AZURE_DEVOPS_EXT_PAT   (Code: Read & Write)
#   Forgejo       FORGEJO_TOKEN or CODEBERG_TOKEN   (repository and issue: read & write)
#
# Commits say [skip ci], so a pipeline that runs on every push builds nothing.
set -euo pipefail

BASE=mrman-e2e-base
TITLE_PREFIX="mrman live fixture"

die() { echo "live-fixture: $*" >&2; exit 1; }

usage() { sed -n '2,41p' "$0" >&2; exit 2; }

[[ $# -eq 2 && $2 =~ ^(up|down)$ ]] || usage
URL="${1%/}"
URL="${URL%.git}"
ACTION=$2

case "${FORGE:-}:$URL" in
:https://github.com/*) FORGE=github ;;
:https://dev.azure.com/*/_git/*) FORGE=ado ;;
:https://codeberg.org/*) FORGE=forgejo ;;
forgejo:https://*) ;;
:https://*) FORGE=gitlab ;;
*) die "unrecognised URL $URL" ;;
esac

# json EXPR — evaluate a Python expression over the JSON on stdin (as `d`).
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

# quote S — percent-encode S for a URL path segment.
quote() { python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$1"; }

# api METHOD URL [JSON] — call a forge REST API; prints the body, fails loudly
# on a non-2xx status. The token reaches curl through a config file on stdin,
# not argv.
api() {
	local method=$1 url=$2 data=${3:-} out code header
	case "$FORGE" in
	gitlab) header="PRIVATE-TOKEN: $GITLAB_TOKEN" ;;
	ado) header="Authorization: Basic $(printf ':%s' "$AZURE_DEVOPS_EXT_PAT" | base64 -w0)" ;;
	forgejo) header="Authorization: token $FJ_TOKEN" ;;
	*) die "api is not used for $FORGE" ;;
	esac
	out="$(mktemp)"
	code="$(printf 'header = "%s"\n' "$header" | curl -sS -K - -o "$out" -w '%{http_code}' \
		-X "$method" -H 'Content-Type: application/json' ${data:+--data "$data"} "$url")"
	if [[ $code != 2* ]]; then
		echo "live-fixture: $method $url -> HTTP $code: $(head -c 400 "$out")" >&2
		rm -f "$out"
		return 1
	fi
	cat "$out"
	rm -f "$out"
}

# Per-forge coordinates.
case "$FORGE" in
github)
	command -v gh >/dev/null || die "gh is required for GitHub"
	REPO_SLUG="${URL#https://github.com/}"
	;;
gitlab)
	: "${GITLAB_TOKEN:?set GITLAB_TOKEN to a token with the api scope}"
	HOST="${URL#https://}"
	HOST="${HOST%%/*}"
	PROJECT="${URL#https://"$HOST"/}"
	GL_API="https://$HOST/api/v4"
	PID="$(quote "$PROJECT")"
	;;
ado)
	: "${AZURE_DEVOPS_EXT_PAT:?set AZURE_DEVOPS_EXT_PAT to a PAT with Code (Read & Write)}"
	rest="${URL#https://dev.azure.com/}"
	ORG="${rest%%/*}"
	rest="${rest#*/}"
	PROJECT="${rest%%/_git/*}"
	REPO="${rest#*/_git/}"
	ADO_API="https://dev.azure.com/$ORG/$(quote "$PROJECT")/_apis/git/repositories/$(quote "$REPO")"
	;;
forgejo)
	FJ_TOKEN="${FORGEJO_TOKEN:-${CODEBERG_TOKEN:-}}"
	export FJ_TOKEN
	[[ -n $FJ_TOKEN ]] || die "set FORGEJO_TOKEN or CODEBERG_TOKEN"
	HOST="${URL#https://}"
	HOST="${HOST%%/*}"
	REPO_SLUG="${URL#https://"$HOST"/}"
	FJ_API="https://$HOST/api/v1/repos/$REPO_SLUG"
	;;
esac

# Git credentials, read from the environment by a helper at run time: the
# single quotes are deliberate, so the token never appears in git's argv.
GIT_CRED=()
# shellcheck disable=SC2016
case "$FORGE" in
github) GIT_CRED=(-c credential.helper= -c 'credential.helper=!gh auth git-credential') ;;
gitlab) GIT_CRED=(-c credential.helper= -c 'credential.helper=!f() { echo username=oauth2; echo "password=$GITLAB_TOKEN"; }; f') ;;
ado) GIT_CRED=(-c credential.helper= -c 'credential.helper=!f() { echo username=pat; echo "password=$AZURE_DEVOPS_EXT_PAT"; }; f') ;;
forgejo) GIT_CRED=(-c credential.helper= -c 'credential.helper=!f() { echo username=token; echo "password=$FJ_TOKEN"; }; f') ;;
esac

WORK=""
cleanup() { [[ -n $WORK ]] && rm -rf "$WORK"; }
trap cleanup EXIT

g() { git -C "$WORK" "${GIT_CRED[@]}" -c user.name="mrman live fixture" -c user.email=mrman-live@example.invalid "$@"; }

# clone_work — a throwaway repository wired to the remote.
clone_work() {
	WORK="$(mktemp -d)"
	git -C "$WORK" init -q
	g remote add origin "$URL"
	g fetch -q origin 2>/dev/null || true
}

# default_branch — the remote's default branch, seeding "main" with one
# commit when the repository is empty.
default_branch() {
	local head
	head="$(g ls-remote --symref origin HEAD 2>/dev/null | sed -n 's|^ref: refs/heads/\([^\t]*\)\tHEAD$|\1|p')"
	if [[ -n $head ]]; then
		echo "$head"
		return
	fi
	if [[ -n "$(g ls-remote --heads origin main)" ]]; then
		echo main
		return
	fi
	echo "live-fixture: $URL is empty; seeding main" >&2
	g checkout -q --orphan main
	printf '# scratch\n\nA throwaway repository for mrman live tests.\n' >"$WORK/README.md"
	g add README.md
	g commit -q -m "[skip ci] Seed the repository"
	g push -q origin main
	echo main
}

write() { mkdir -p "$(dirname "$WORK/$1")"; printf '%s' "$2" >"$WORK/$1"; }

numbered() { # numbered PREFIX FROM TO — one "PREFIX N" line per number
	local i
	for ((i = $2; i <= $3; i++)); do printf '%s %d\n' "$1" "$i"; done
}

base_files() {
	write mrman-e2e/cache.go 'package fixture

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
	write mrman-e2e/obsolete.txt $'to be deleted\n'
	write mrman-e2e/notes-old.md $'# Notes\n\nline one\nline two\nline three\nline four\n'
	write mrman-e2e/edge/config.yaml $'# two documents\n---\nname: alpha\nsize: 1\n---\nname: beta\nsize: 2\nlimit: 10\n'
	write mrman-e2e/edge/schema.sql $'CREATE TABLE t (\n  id INT,\n  -- legacy column\n  legacy TEXT,\n  name TEXT\n);\n'
	write mrman-e2e/edge/counter.c $'int count(int n) {\n  int i = 0;\n  while (i < n) {\n  }\n  return i;\n}\n'
	write "mrman-e2e/edge/café.md" $'# Café\n\nespresso\nlatte\n'
	write mrman-e2e/edge/rename-src.go "$(printf 'package fixture\n\n'; numbered '// rename line' 1 20)"$'\n'
	write mrman-e2e/edge/long.go "$(printf 'package fixture\n\n'; numbered '// filler' 1 50; printf 'func middle() int { return 1 }\n'; numbered '// filler' 51 100)"$'\n'
	write mrman-e2e/edge/shared.txt "$(numbered 'shared line' 1 40)"$'\n'
}

head_commit_1() {
	write mrman-e2e/cache.go 'package fixture

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
	rm "$WORK/mrman-e2e/obsolete.txt"
	g mv mrman-e2e/notes-old.md mrman-e2e/notes-new.md
	write mrman-e2e/notes-new.md $'# Notes\n\nline one\nline 2\nline three\nline four\n'
	write mrman-e2e/added.txt $'brand new\nsecond line\n'
}

head_commit_2() {
	write mrman-e2e/edge/config.yaml $'# two documents\n---\nname: alpha\nsize: 1\nname: beta\nsize: 3\nlimit: 10\n'
	write mrman-e2e/edge/schema.sql $'CREATE TABLE t (\n  id INT,\n  legacy TEXT,\n  name TEXT\n);\n'
	write mrman-e2e/edge/counter.c $'int count(int n) {\n  int i = 0;\n  while (i < n) {\n++i;\n  }\n  return i;\n}\n'
	write "mrman-e2e/edge/café.md" $'# Café\n\nespresso\ncortado\nlatte\n'
	g mv mrman-e2e/edge/rename-src.go mrman-e2e/edge/rename-dst.go
	write mrman-e2e/edge/rename-dst.go "$(printf 'package fixture\n\n'; numbered '// rename line' 1 9; numbered '// renamed and edited line' 10 10; numbered '// rename line' 12 20)"$'\n'
	write mrman-e2e/edge/long.go "$(printf 'package fixture\n\n'; numbered '// filler' 1 50; printf 'func middle() int { return 2 }\n'; numbered '// filler' 51 100)"$'\n'
	write mrman-e2e/edge/shared.txt "$(numbered 'shared line' 1 29; echo 'shared line 30, changed on the head'; numbered 'shared line' 31 40)"$'\n'
}

base_moves_on() {
	write mrman-e2e/edge/shared.txt "$(numbered 'shared line' 1 4; echo 'shared line 5, changed on the base after the MR opened'; numbered 'shared line' 6 40)"$'\n'
}

open_mr() { # open_mr HEAD TITLE — prints the merge request's web URL
	local head=$1 title=$2 body="Created by scripts/live-fixture.sh for mrman's live tests. Safe to close."
	case "$FORGE" in
	github)
		gh pr create -R "$REPO_SLUG" --base "$BASE" --head "$head" --title "$title" --body "$body"
		;;
	gitlab)
		local uid
		uid="$(api GET "$GL_API/user" | json 'd["id"]')"
		api POST "$GL_API/projects/$PID/merge_requests" "$(python3 -c '
import json,sys
print(json.dumps({"source_branch": sys.argv[1], "target_branch": sys.argv[2], "title": sys.argv[3],
                  "description": sys.argv[4], "reviewer_ids": [int(sys.argv[5])], "remove_source_branch": False}))' \
			"$head" "$BASE" "$title" "$body" "$uid")" | json 'd["web_url"]'
		;;
	ado)
		local id
		id="$(api POST "$ADO_API/pullrequests?api-version=7.1" "$(python3 -c '
import json,sys
print(json.dumps({"sourceRefName": "refs/heads/"+sys.argv[1], "targetRefName": "refs/heads/"+sys.argv[2],
                  "title": sys.argv[3], "description": sys.argv[4]}))' "$head" "$BASE" "$title" "$body")" | json 'd["pullRequestId"]')"
		echo "$URL/pullrequest/$id"
		;;
	forgejo)
		api POST "$FJ_API/pulls" "$(python3 -c '
import json,sys
print(json.dumps({"head": sys.argv[1], "base": sys.argv[2], "title": sys.argv[3], "body": sys.argv[4]}))' \
			"$head" "$BASE" "$title" "$body")" | json 'd["html_url"]'
		;;
	esac
}

cmd_up() {
	clone_work
	local default stamp head
	default="$(default_branch)"
	stamp="$(date -u +%Y%m%dT%H%M%SZ)"
	head="mrman-e2e-$stamp"

	g fetch -q origin "$default"
	g checkout -q -B "$BASE" FETCH_HEAD
	base_files
	g add -A
	g commit -q -m "[skip ci] $TITLE_PREFIX: base $stamp"
	g push -q -f origin "$BASE"

	g checkout -q -b "$head"
	head_commit_1
	g add -A
	g commit -q -m "[skip ci] $TITLE_PREFIX: modify, add, delete, rename"
	head_commit_2
	g add -A
	g commit -q -m "[skip ci] $TITLE_PREFIX: parser and driver edge cases"
	g push -q origin "$head"

	open_mr "$head" "$TITLE_PREFIX $stamp"

	g checkout -q "$BASE"
	base_moves_on
	g commit -q -am "[skip ci] $TITLE_PREFIX: move the base on"
	g push -q origin "$BASE"
}

cmd_down() {
	case "$FORGE" in
	github)
		for n in $(gh pr list -R "$REPO_SLUG" --state open --json number,title \
			--jq ".[] | select(.title | startswith(\"$TITLE_PREFIX\")) | .number"); do
			gh pr close -R "$REPO_SLUG" "$n" >/dev/null && echo "closed #$n" >&2
		done
		;;
	gitlab)
		for iid in $(api GET "$GL_API/projects/$PID/merge_requests?state=opened&per_page=100" |
			json '" ".join(str(m["iid"]) for m in d if m["title"].startswith("'"$TITLE_PREFIX"'"))'); do
			api PUT "$GL_API/projects/$PID/merge_requests/$iid" '{"state_event":"close"}' >/dev/null
			echo "closed !$iid" >&2
		done
		;;
	ado)
		for id in $(api GET "$ADO_API/pullrequests?searchCriteria.status=active&api-version=7.1" |
			json '" ".join(str(p["pullRequestId"]) for p in d["value"] if p["title"].startswith("'"$TITLE_PREFIX"'"))'); do
			api PATCH "$ADO_API/pullrequests/$id?api-version=7.1" '{"status":"abandoned"}' >/dev/null
			echo "abandoned !$id" >&2
		done
		;;
	forgejo)
		for n in $(api GET "$FJ_API/pulls?state=open&limit=50" |
			json '" ".join(str(p["number"]) for p in d if p["title"].startswith("'"$TITLE_PREFIX"'"))'); do
			api PATCH "$FJ_API/pulls/$n" '{"state":"closed"}' >/dev/null
			echo "closed #$n" >&2
		done
		;;
	esac
	clone_work
	for b in $(g ls-remote --heads origin 'mrman-e2e-*' | sed 's|.*refs/heads/||'); do
		g push -q origin --delete "$b"
		echo "deleted branch $b" >&2
	done
}

case "$ACTION" in
up) cmd_up ;;
down) cmd_down ;;
esac
