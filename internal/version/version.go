// Package version holds release metadata injected at link time.
package version

var (
	// Version is the semver release (VERSION file / tag).
	Version = "dev"
	// Commit is the short git SHA at build time.
	Commit = "unknown"
	// Branch is the git branch at build time.
	Branch = "unknown"
	// BuildDate is the UTC RFC3339 build timestamp.
	BuildDate = "unknown"
)
