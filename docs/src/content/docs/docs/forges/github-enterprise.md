---
title: GitHub Enterprise Server
description: Point mrman at an on-premise GitHub Enterprise Server — api_base, GH_ENTERPRISE_TOKEN, private certificate authorities and token commands.
sidebar:
  order: 2
---

GitHub Enterprise Server has every capability [github.com](../github/) has. The
only difference is that mrman cannot guess your hostname, so you tell it once.

## Configure the host

```toml
# ~/.config/mrman/config.toml
[[forge.hosts]]
host = "ghe.mycorp.com"
forge = "github"
api_base = "https://ghe.mycorp.com/api/v3"
token = "$GHE_TOKEN"                  # $VAR is expanded
ca_file = "/etc/ssl/corp-root.pem"    # if your CA is not in the system store
```

`api_base` is the one field people forget. GitHub Enterprise Server serves its
REST API under `/api/v3`, not at the host root, and the GraphQL endpoint is
derived from it. Without `api_base`, mrman would be calling the web UI.

Once the host entry exists, everything else works the way it does on
github.com:

```sh
mrman pr 125                                       # from a GHE checkout
mrman pr ghe.mycorp.com/owner/repo#125
mrman pr https://ghe.mycorp.com/owner/repo/pull/125
```

Configuration wins over URL shape, so the `[[forge.hosts]]` entry is what
decides that `ghe.mycorp.com` is a GitHub host — mrman does not have to
recognize the name.

## Tokens

For GitHub hosts that are **not** github.com, the environment variable mrman
reads is `GH_ENTERPRISE_TOKEN` — the GitHub CLI convention:

```sh
export GH_ENTERPRISE_TOKEN=...
```

This scoping is deliberate. `GITHUB_TOKEN` is read **only** for github.com, so
a token in your shell for public work never leaks to your employer's
on-premise instance, and vice versa.

The `[[forge.hosts]]` entry above is required for *any* credential, not just
for `api_base`: `GH_ENTERPRISE_TOKEN` is read only for hosts you have listed.
Without an entry mrman still recognises the host as GitHub — from the URL,
or from a hostname containing "github" — but connects unauthenticated and
warns, so a pasted link to a look-alike domain can never be handed your
enterprise token.

Resolution order for a GHE host, first hit wins:

1. `GH_ENTERPRISE_TOKEN`
2. The host's `token` (with `$VAR` expansion)
3. The host's `token_cmd`
4. `gh auth token --hostname ghe.mycorp.com`, if
   `[forge] cli_token_fallback` is left on

### Keeping the token out of your config file

`token_cmd` runs through `sh -c` (`cmd /C` on Windows) and its result is cached for the process, so a
password manager works cleanly:

```toml
[[forge.hosts]]
host = "ghe.mycorp.com"
forge = "github"
api_base = "https://ghe.mycorp.com/api/v3"
token_cmd = "pass show work/ghe-token"
```

A host entry may set `token` **or** `token_cmd`, never both — the schema
rejects that rather than leaving you guessing which one won. And unlike the
environment lookup, a failing `token_cmd` is fatal rather than skipped: you
configured it on purpose, so silently falling through to anonymous access would
be the wrong kindness.

## TLS

`ca_file` adds your organization's certificate authority for that host only.
Point it at a PEM bundle.

```toml
ca_file = "/etc/ssl/corp-root.pem"
```

There is also `insecure_skip_verify = true` per host. It exists because
sometimes you need to get work done, and it should stay a last resort — it
disables certificate verification for that host entirely. mrman reminds you
it is on with a warning at every start, so it does not outlive the outage
that justified it.

## SSH remotes

If your remote is `git@ghe.mycorp.com:owner/repo.git`, mrman resolves the host
from it normally. Two extra conveniences:

- **`~/.ssh/config` Host aliases** are resolved to their real `HostName`, so a
  remote using an alias still finds the right forge host. Only exact `Host`
  patterns match — wildcards, negation, `Match` and `Include` are unsupported
  and fall back to the alias unchanged.
- **SSH-over-443 transport hosts** (`ssh.github.com`) are mapped back to their
  API host, because the transport name is right for git and wrong for API
  calls.

## Verify it works

```sh
mrman pr <some open MR number>
```

If you get a 404 on a repository you can see in the browser, mrman is
unauthenticated for that host — walk the resolution order above. The
[Troubleshooting](../../project/troubleshooting/) page has the full checklist,
including how to prove which link in the chain fired.
