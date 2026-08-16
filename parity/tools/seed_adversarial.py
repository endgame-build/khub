"""Seed a workspace with the file shapes that actually broke this port.

    python3 parity/tools/seed_adversarial.py <workspace>

The differential fuzzer exercises SEQUENCES; this exercises the CORPUS. Every
entry here corresponds to a real bug or a near miss found during the Go port,
so a regression in any of them shows up as a read-path divergence on the very
first `query`/`check`/`search` the fuzzer runs.
"""

from __future__ import annotations

import sys
from pathlib import Path

# (relative path, raw bytes, what it is guarding against)
CASES: list[tuple[str, bytes, str]] = [
    ("fragments/crlf.md",
     b"---\r\ntype: fragment\r\ncreated: 2026-01-15\r\nname: CRLF entity\r\n---\r\nbody\r\n",
     "CRLF entity was INVISIBLE to Go: the frontmatter boundary used [ \\t]* where "
     "python-frontmatter uses \\s*, and \\s matches \\r"),

    ("fragments/bom.md",
     "﻿---\ntype: fragment\ncreated: 2026-01-15\nname: BOM\n---\nbody\n".encode(),
     "python-frontmatter does NOT tolerate a BOM (so this is a stray) while khub's "
     "own edit-altitude splitter does — the two md read altitudes differ here"),

    ("fragments/no-fence.md",
     b"loose notes, not an entity\n",
     "a fenceless .md inside a layout is a STRAY, not malformed"),

    ("fragments/unterminated.md",
     b"---\ntype: fragment\nunclosed: [\n",
     "an UNTERMINATED fence is also a stray; only a CLOSED fence over invalid YAML "
     "is malformed"),

    ("fragments/malformed.md",
     b"---\ntype: fragment\nunclosed: [\n---\nbody\n",
     "the genuinely malformed case: fence closes, YAML does not parse"),

    ("fragments/comments.md",
     b"---\n# header comment\ntype: fragment\ncreated: 2026-01-15\n"
     b"name: Commented   # inline comment\n# between keys\nnote: keep\n---\nbody\n",
     "comment preservation on edit (ruamel keeps them; the splice must too)"),

    ("fragments/ctrl-char.md",
     ("---\ntype: fragment\ncreated: 2026-01-15\nname: "
      + "x" * 100 + "\x01" + "y" * 40 + "\n---\nbody\n").encode(),
     "a control character past the fold column PANICKED the emitter (Python slices "
     "clamp, Go's do not)"),

    ("fragments/yaml-scalars.md",
     b"---\ntype: fragment\ncreated: 2026-01-15\nname: scalars\n"
     b"a: yes\nb: no\nc: on\nd: 017\ne: 1:30\nf: 2026-01-15\ng: .inf\n---\nbody\n",
     "YAML 1.1 vs 1.2 resolution: md frontmatter goes through PyYAML (1.1), yaml "
     "entities through ruamel (1.2), and they genuinely disagree"),

    ("fragments/unicode-slug.md",
     "---\ntype: fragment\ncreated: 2026-01-15\nname: ß İstanbul Café\n---\nbody\n".encode(),
     "str.lower() on 'İ' yields TWO code points in Python and one in strings.ToLower, "
     "which changes the minted slug"),

    ("fragments/big-int.md",
     b"---\ntype: fragment\ncreated: 2026-01-15\nname: big\ncount: 100000000000000000000000000000\n---\nbody\n",
     "Python ints are unbounded; the value must survive a round trip"),

    ("fragments/empty-body.md",
     b"---\ntype: fragment\ncreated: 2026-01-15\nname: empty body\n---\n",
     "empty body, and the trailing-newline handling around the closing fence"),

    ("fragments/trailing-ws.md",
     b"---\ntype: fragment\ncreated: 2026-01-15\nname: trailing   \n---\nbody\n",
     "trailing whitespace after a scalar (neither implementation preserves it, and "
     "they must agree on that)"),
]


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__)
        return 2
    ws = Path(sys.argv[1])
    if not (ws / ".khub").is_dir():
        print(f"{ws} is not a khub workspace", file=sys.stderr)
        return 2
    for rel, payload, why in CASES:
        p = ws / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(payload)
        print(f"  {rel:<34} {why[:70]}")
    print(f"\nseeded {len(CASES)} adversarial files into {ws}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
