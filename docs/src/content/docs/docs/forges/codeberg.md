---
title: Codeberg
description: Review Codeberg merge requests from the terminal. Codeberg is claimed by default, so a token is the only setup.
sidebar:
  order: 6
---

:::note[Verified against Codeberg]
The Forgejo driver was exercised end to end against Codeberg (Forgejo
16.0.0-dev) on 2026-10-08. The run covered browsing a merge request,
expanding context, posting inline comments and a review body, a draft
(pending) review, existing threads and review bodies rendering, and an agent
submitting through the grant. Every outcome was read back from Codeberg. On
your own merge request, Forgejo refuses an approval or a request for changes
("approve your own pull is not allowed"), and mrman shows that refusal;
voting on someone else's merge request was not exercised. See [support
levels](../../reference/forge-capabilities/#support-levels).
:::

[Codeberg](https://codeberg.org) is a public Forgejo instance, and mrman claims
`codeberg.org` automatically. There is **nothing to configure** — set a token
and go.

## Authenticate

Generate a token under *Settings → Applications → Access Tokens* with
**repository** read and write permission, then:

- add **issue** read for the review-requested filter (`r` in the merge-request
  list);
- add **user** read for "commits since your last review", which needs to know
  who you are. Without it the review still works, only that shortcut is off.

```sh
export FORGEJO_TOKEN=...
# CODEBERG_TOKEN works too, and is checked second
```

Both variables are read for `codeberg.org` only, so they never travel to a
self-hosted instance. If you prefer config:

```toml
[[forge.hosts]]
host = "codeberg.org"
forge = "forgejo"
token_cmd = "pass show codeberg-token"
```

A public repository needs no token at all — you can browse and review one
unauthenticated, and only need credentials to submit.

## Open a merge request

```sh
mrman pr 12                                    # from inside the checkout
mrman pr owner/repo#12
mrman pr https://codeberg.org/owner/repo/pulls/12
mrman                                          # then Tab to Merge Requests
```

No `[[forge.hosts]]` entry is needed for any of these: `codeberg.org` is one of
the well-known SaaS hosts mrman recognizes, alongside `github.com`,
`gitlab.com` and `dev.azure.com`. It is also the default host when
`[forge] default = "forgejo"` and you pass a bare `owner/repo#N` from outside a
checkout.

## Capabilities

Codeberg runs Forgejo, so it behaves exactly as described on the [Forgejo &
Gitea](../forgejo/) page — including the two things worth knowing before you
review:

- **Thread resolution and outdated state are approximated**, because Forgejo's
  API returns comments rather than threads. If a resolved state looks wrong,
  believe the web UI.
- **Multi-line comments downgrade** to their end line with a `Lines X–Y:` prefix
  in the body, since Forgejo accepts one position rather than a span.

On the plus side, Forgejo gives mrman **atomic submit** and real draft reviews —
your review either posts entirely or not at all, and `:submit draft` creates a
`PENDING` review you publish later.

Read [Forgejo & Gitea](../forgejo/) for the detail; everything there applies
here, minus the host configuration.
