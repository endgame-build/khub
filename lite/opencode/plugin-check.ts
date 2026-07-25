/**
 * build-lite ambient gate — copy to .opencode/plugins/build-lite-check.ts
 *
 * opencode has no PostToolUse shell hook; `tool.execute.after` is the analogue.
 * After any write/edit that touched the corpus, run the sweep and append the
 * errors to the tool's own output, so the model sees the breakage it just
 * caused in the same turn instead of at the end of the session.
 *
 * Gaps are deliberately not surfaced here — they are normal mid-change state,
 * and a gate that cries every edit gets ignored.
 */
import type { Plugin } from "@opencode-ai/plugin"

const WRITERS = ["write", "edit", "patch"]
const CORPUS = /(knowledge|specs)\//

export const BuildLiteCheck: Plugin = async ({ $, worktree }) => ({
  "tool.execute.after": async (input, output) => {
    if (!WRITERS.includes(input.tool)) return
    const touched = JSON.stringify(input ?? {}) + JSON.stringify(output?.metadata ?? {})
    if (!CORPUS.test(touched)) return

    const bl = `${worktree}/.opencode/skills/build-lite/bl.py`
    const raw = await $`python3 ${bl} check --json`.cwd(worktree).nothrow().quiet().text()
    let errors: { code: string; where: string; message: string }[] = []
    try {
      errors = JSON.parse(raw).errors ?? []
    } catch {
      return // bl unavailable or output unparseable — never block the edit
    }
    if (!errors.length) return

    const lines = errors.map((e) => `  ${e.code}  ${e.where}  ${e.message}`).join("\n")
    output.output += `\n\nbuild-lite check — ${errors.length} error(s), fix before finishing:\n${lines}`
  },
})
