// Package version holds the build version shared by all OBIE binaries.
package version

// Version is the build version. It is injected at build time via
//
//	-ldflags "-X github.com/MNCloudwerksTechnology/obie/internal/version.Version=<version>"
//
// and defaults to "dev" for untagged local builds.
var Version = "dev"

// String returns the version line printed by a binary's --version flag.
func String(program string) string {
	return program + " " + Version
}
