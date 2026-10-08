package forge

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/infrashift/mrman/internal/vcs"
)

// sshConfigPathFn locates the ssh client config consulted for Host alias
// resolution. A seam so tests can point it at a fixture file.
var sshConfigPathFn = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// SplitRemoteURL normalizes a git remote URL into its forge host and path
// segments. It accepts https/http/ssh URL forms and the scp-like
// "git@host:path" form, strips the user@ prefix, any :port, the ".git"
// suffix, and query/fragment noise, resolves ~/.ssh/config Host aliases for
// scp-form hosts (exact patterns only), and maps documented SSH transport
// hosts back to their API hosts (ssh.github.com, altssh.gitlab.com,
// ssh.dev.azure.com). ok is false when the URL has no recognizable
// host/path shape.
func SplitRemoteURL(raw string) (host string, segments []string, ok bool) {
	trimmed := trimURLSuffix(strings.TrimSpace(raw))
	if trimmed == "" {
		return "", nil, false
	}

	if scpHost, scpPath, isSCP := splitSCPLike(trimmed); isSCP {
		return hostAndSegments(resolveSSHHostname(scpHost), scpPath)
	}

	rest, hadScheme := stripScheme(trimmed)
	if !hadScheme {
		if strings.Contains(trimmed, "://") {
			return "", nil, false // unrecognized scheme
		}
		rest = trimmed
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	urlHost, path, found := strings.Cut(rest, "/")
	if !found || urlHost == "" {
		return "", nil, false
	}
	return hostAndSegments(urlHost, path)
}

// hostAndSegments normalizes the host (lowercase, port stripped, transport
// aliases mapped) and splits the path into non-empty, percent-decoded
// segments with the trailing ".git" removed.
//
// Decoding matters for Azure DevOps, whose clone URLs encode a space in a
// project or repository name as %20: kept encoded, the SDK escapes it again
// and every request 404s on "My%2520Project".
func hostAndSegments(host, path string) (string, []string, bool) {
	host = normalizeTransportHost(strings.ToLower(stripPort(host)))
	if host == "" {
		return "", nil, false
	}
	segments := nonEmptySegments(path)
	if len(segments) == 0 {
		return "", nil, false
	}
	for i, seg := range segments {
		if decoded, err := url.PathUnescape(seg); err == nil {
			segments[i] = decoded
		}
	}
	last := strings.TrimSuffix(segments[len(segments)-1], ".git")
	if last == "" {
		return "", nil, false
	}
	segments[len(segments)-1] = last
	return host, segments, true
}

// splitSCPLike recognizes the scp-like "user@host:path" remote form (no
// scheme). ok is false for URL forms and malformed values.
func splitSCPLike(remote string) (host, path string, ok bool) {
	if strings.Contains(remote, "://") {
		return "", "", false
	}
	hostPart, pathPart, found := strings.Cut(remote, ":")
	if !found || strings.Contains(hostPart, "/") || pathPart == "" {
		return "", "", false
	}
	if at := strings.LastIndex(hostPart, "@"); at >= 0 {
		hostPart = hostPart[at+1:]
	}
	return hostPart, pathPart, true
}

// stripScheme removes a recognized URL scheme prefix.
func stripScheme(value string) (rest string, ok bool) {
	for _, scheme := range []string{"https://", "http://", "ssh://", "git://"} {
		if strings.HasPrefix(value, scheme) {
			return value[len(scheme):], true
		}
	}
	return value, false
}

// stripPort removes a trailing ":<digits>" port from a host.
func stripPort(host string) string {
	i := strings.LastIndex(host, ":")
	if i < 0 || i == len(host)-1 {
		return host
	}
	for _, r := range host[i+1:] {
		if r < '0' || r > '9' {
			return host
		}
	}
	return host[:i]
}

// trimURLSuffix cuts query/fragment noise and trailing slashes.
func trimURLSuffix(value string) string {
	if i := strings.IndexAny(value, "?#"); i >= 0 {
		value = value[:i]
	}
	return strings.TrimRight(value, "/")
}

func stripGitSuffix(value string) string {
	return strings.TrimSuffix(value, ".git")
}

func nonEmptySegments(path string) []string {
	var segments []string
	for seg := range strings.SplitSeq(path, "/") {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	return segments
}

// normalizeTransportHost maps documented SSH transport hosts back to the
// hosts the forge APIs live on. Users behind port-22 blocks follow each
// forge's SSH-over-443 workaround; the transport hostname is correct for
// git but not for API calls or host identity.
func normalizeTransportHost(host string) string {
	switch host {
	case "ssh.github.com":
		return "github.com"
	case "altssh.gitlab.com":
		return "gitlab.com"
	case "ssh.dev.azure.com":
		return "dev.azure.com"
	}
	return host
}

// resolveSSHHostname resolves an SSH host alias to its real HostName via
// the ssh client config, returning the alias unchanged when the config is
// missing, unreadable, or has no matching block.
//
// Limitations: only exact Host patterns match. Wildcards, negation, Match,
// and Include directives are unsupported; aliases depending on them fall
// back unchanged.
func resolveSSHHostname(alias string) string {
	path := sshConfigPathFn()
	if path == "" {
		return alias
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return alias
	}
	return resolveSSHHostnameFromConfig(alias, string(content))
}

// resolveSSHHostnameFromConfig scans ssh config text for a Host block with
// an exact pattern match and returns its HostName, or the alias unchanged.
func resolveSSHHostnameFromConfig(alias, config string) string {
	inBlock := false
	for raw := range strings.SplitSeq(config, "\n") {
		line := raw
		// Strip inline comments and surrounding whitespace.
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value := splitSSHKeyValue(line)
		switch {
		case strings.EqualFold(key, "Host"):
			// Exact match only; wildcards and negation are
			// intentionally unsupported.
			inBlock = slices.Contains(strings.Fields(value), alias)
		case strings.EqualFold(key, "Match"):
			// Match blocks aren't supported; exit any Host block.
			inBlock = false
		case inBlock && strings.EqualFold(key, "HostName"):
			return value
		}
	}
	return alias
}

// splitSSHKeyValue splits an ssh config line into keyword and value; the
// separator is whitespace or '='.
func splitSSHKeyValue(line string) (key, value string) {
	i := strings.IndexFunc(line, func(r rune) bool {
		return r == '=' || unicode.IsSpace(r)
	})
	if i < 0 {
		return line, ""
	}
	rest := strings.TrimLeft(line[i:], "= \t")
	return line[:i], strings.TrimSpace(rest)
}

// RemoteURLs lists the repository's remote URLs, origin first, then every
// other remote in `git remote -v` order, deduplicated. Errors are treated
// as "no remotes" — detection is best-effort.
func RemoteURLs(repoRoot string, run vcs.Runner) []string {
	var urls []string
	seen := make(map[string]bool)
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}

	if stdout, _, err := run.Run(repoRoot, "git", "remote", "get-url", "origin"); err == nil {
		add(firstLine(string(stdout)))
	}
	if stdout, _, err := run.Run(repoRoot, "git", "remote", "-v"); err == nil {
		for line := range strings.SplitSeq(string(stdout), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				add(fields[1])
			}
		}
	}
	return urls
}

// firstLine returns the first line of s, trimmed.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
