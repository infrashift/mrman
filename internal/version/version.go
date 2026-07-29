// Package version holds build metadata stamped at link time via -ldflags.
package version

var (
	// Version is the semantic version or git describe output.
	Version = "dev"
	// Commit is the short git commit hash of the build.
	Commit = "none"
	// Date is the UTC build timestamp in RFC 3339 format.
	Date = "unknown"
)

// String returns the full human-readable version line.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
