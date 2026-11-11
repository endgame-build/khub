// Package install holds the commands that install khub, its agent skills and
// its Claude Code plugin, each written once.
//
// khub prints them (`init`'s follow-up hint and help text) and writes them into
// every wired workspace's AGENTS.md and CLAUDE.md, and the docs show them to
// people and agents. install_test.go checks the names against the marketplace
// manifest, the plugin manifest and the npm package template, and reads the
// docs it lists: it fails on any install command that is not one of these, and
// on any `npx` run of a package other than Package. A rename of the package,
// the repo, the marketplace or the plugin lands here, and the test names every
// copy in those docs to fix. CHANGELOG.md and the literal strings wire_test.go
// pins are not checked.
package install

const (
	// Repo is the GitHub repository: the skills source and the plugin marketplace.
	Repo = "endgame-build/khub"
	// Package is the npm package carrying the binary.
	Package = "@endgame-build/khub"
	// Marketplace is the name the repo's marketplace manifest gives itself.
	Marketplace = "khub"
	// Plugin is the plugin's id within that marketplace.
	Plugin = "khub@" + Marketplace
)

const (
	// NPMInstall pins khub as a dev dependency of the repo, the primary install.
	NPMInstall = "npm install -D " + Package
	// NPMInstallGlobal installs khub for the machine.
	NPMInstallGlobal = "npm install -g " + Package

	// SkillsAdd installs the agent skills into the agents `npx skills` finds.
	SkillsAdd = "npx skills add " + Repo

	// MarketplaceAdd and PluginInstall install the plugin, typed at the Claude
	// Code prompt. From a shell they are spelled `claude plugin …`.
	MarketplaceAdd = "/plugin marketplace add " + Repo
	PluginInstall  = "/plugin install " + Plugin

	// MarketplaceUpdate and PluginUpdate update the plugin, run from a shell.
	MarketplaceUpdate = "claude plugin marketplace update " + Marketplace
	PluginUpdate      = "claude plugin update " + Plugin
)
