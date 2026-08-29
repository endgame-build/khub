#!/usr/bin/env node
"use strict";
// Launcher for the khub static binary.
//
// One package carries every binary, under binaries/<platform>-<arch>/khub.
// package.json's `bin` is a single path, so this shim exists to pick the right
// one at run time and hand over: stdio is inherited (khub's own TTY detection
// and EPIPE handling see the real file descriptors), the exit code is forwarded
// verbatim (0/1/2 is khub's pinned contract), and a terminating signal is
// re-raised rather than translated into a code.
//
// The supported set is whatever binaries/ actually holds — there is no list in
// here to drift out of step with the goreleaser build matrix.
//
// Deliberately dependency-free and lifecycle-script-free: installs must work
// under --ignore-scripts, and there is nothing here to supply-chain audit
// beyond what you are reading.

const { spawnSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const root = path.join(__dirname, "..", "binaries");
const target = `${process.platform}-${process.arch}`;
const bin = path.join(root, target, "khub");

if (!fs.existsSync(bin)) {
  // No fallback if binaries/ is unreadable: node's own ENOENT names the path,
  // which beats anything this could invent about why the package is malformed.
  const shipped = fs.readdirSync(root).sort();
  process.stderr.write(
    `khub: no binary for ${target}. This build ships: ${shipped.join(", ")}.\n` +
      `If ${target} should be supported, the package is incomplete — reinstall ` +
      "@endgame-build/khub.\n"
  );
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  process.stderr.write(`khub: cannot execute ${bin}: ${result.error.message}\n`);
  process.exit(1);
}
if (result.signal) {
  // Re-raise so the parent observes the same termination the binary did.
  process.kill(process.pid, result.signal);
}
process.exit(result.status === null ? 1 : result.status);
