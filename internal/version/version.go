package version

import "fmt"

// Version is the canonical version string for Squid-OS.
const Version = "v0.7.0"

// GitCommit is injected at build time via -ldflags (optional).
var GitCommit string

// String returns the canonical version identifier.
func String() string {
	return Version
}

// Full returns version with build metadata if available.
func Full() string {
	if GitCommit != "" {
		return fmt.Sprintf("%s-%s", Version, GitCommit)
	}
	return Version
}
