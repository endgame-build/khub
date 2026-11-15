// warnings.go: the session-scoped integrity warnings a write verb prints to
// stderr. The logic is integrity.SessionWarnings; this file only names the
// session and prints the lines.

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/clock"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
)

// writeSession names the session a write belongs to. KHUB_SESSION wins, and
// set but empty turns the warnings off. Without it, Claude Code's
// CLAUDE_CODE_SESSION_ID, which it exports to every shell it runs, so an agent
// gets the warnings without having to ask. Neither set: no session, no
// warnings, and a write prints exactly what it always has.
func writeSession() (string, bool) {
	if s, set := os.LookupEnv("KHUB_SESSION"); set {
		return s, s != ""
	}
	s := os.Getenv("CLAUDE_CODE_SESSION_ID")
	return s, s != ""
}

// warnAfterWrite prints `warning: <id> › <rule>: <message>` for each gating
// `check` finding this session has not been shown. A write verb calls it after
// the write landed and before Emit, so the lines precede the result as a
// search note does. The check runs here, outside the workspace lock, with the
// flag-shadow finding the CLI owns filled in as `check` fills it. Advisory: a
// failure here prints nothing and never changes the write's exit code.
func warnAfterWrite(cmd *cobra.Command, ws string) {
	session, ok := writeSession()
	if !ok {
		return
	}
	report, err := integrity.Check(ws, false, clock.Now())
	if err != nil {
		return
	}
	resolved, err := introspect.LoadSchema(ws)
	if err != nil {
		return
	}
	report.FlagShadowed = shadowedFields(cmd.Root(), resolved)
	findings, err := integrity.SessionWarnings(ws, session, report.Findings())
	if err != nil {
		return
	}
	for _, line := range warningLines(findings) {
		Warn(line)
	}
}

// warningCap bounds the lines one write prints. A session's first write in a
// workspace with a long backlog would otherwise print every finding, burying
// the write's own result; the rest are recorded as shown all the same, and
// `check` lists them.
const warningCap = 20

// warningLines is the stderr text for a write's findings, before Warn's
// prefix: the first
// warningCap of them, then one line counting the rest.
func warningLines(findings []integrity.Finding) []string {
	lines := []string{}
	for i, f := range findings {
		if i == warningCap {
			lines = append(lines, fmt.Sprintf("… and %d more; run khub check", len(findings)-warningCap))
			break
		}
		lines = append(lines, f.Line())
	}
	return lines
}
