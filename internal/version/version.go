// Package version carries khub's single version constant. It replaced the
// pyproject/__init__ pair, which drifted four minors apart unnoticed because
// nothing asserted they agreed; the release workflow now fails if this constant
// and the git tag disagree. Release builds override it via -ldflags.
package version

// Version is the release the binary reports. goreleaser overrides it with
// -ldflags at build time; the value here is the version of the source tree.
var Version = "0.25.0"
