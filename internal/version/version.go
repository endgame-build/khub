// Package version carries khub's single version constant. It replaced the
// pyproject/__init__ pair, which drifted four minors apart unnoticed because
// nothing asserted they agreed; the release workflow now fails if this constant
// and the git tag disagree. Release builds override it via -ldflags.
package version

var Version = "0.23.0"
