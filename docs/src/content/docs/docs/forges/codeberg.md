---
title: Codeberg
description: Review Codeberg pull requests from the terminal. Codeberg is claimed by default, so a token is the only setup.
sidebar:
  order: 6
---

[Codeberg](https://codeberg.org) is a public Forgejo instance, and mrman claims
`codeberg.org` automatically. There is **nothing to configure** — set a token
and go.

## Authenticate

Generate a token under *Settings → Applications → Access Tokens* with
**repository** read and write permission, then:

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

## Open a pull request

```sh
mrman pr 12                                    # from inside the checkout
mrman pr owner/repo#12
mrman pr https://codeberg.org/owner/repo/pulls/12
mrman                                          # then Tab to Pull Requests
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
