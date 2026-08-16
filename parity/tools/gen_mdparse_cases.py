"""parity/corpus/mdparse-cases.jsonl — python-frontmatter parse() expectations.

The md scan altitude (formats.parse) must never fail on a missing or
unterminated fence: that is what makes a loose .md inside a layout a STRAY
rather than a malformed entity. Recorded from the real library."""
import json
import frontmatter

CASES = [
    "loose notes, not an entity\n",
    "",
    "   \n\n  ",
    "---\nunterminated\n",
    "---\ntype: note\n---\nbody\n",
    "----\ntype: x\n----\nbody\n",
    "--- \ntype: y\n---\nb\n",
    "\n\n---\ntype: z\n---\nb\n",
    "---\n- a\n- b\n---\nbody\n",
    "---\n---\nempty fm\n",
    "---\ntype: note\n---\n",
    "---\ntype: note\n---\n\n\n  body with space  \n\n",
    "no fence but --- inside\ntext\n",
    "---\ntype: note\nnested:\n  a: 1\n---\nbody\n",
    "﻿---\ntype: bom\n---\nb\n",
    "text before\n---\ntype: late\n---\nafter\n",
    "---\ndraft: yes\n---\nb\n",
    "-----\ntype: five\n-----\nb\n",
    # CRLF and other whitespace after the fence: python-frontmatter's boundary
    # is ^-{3,}\s*$, so \r counts. A [ \t]-only class made CRLF entities invisible.
    "---\r\ntype: crlf\r\ntitle: t\r\n---\r\nbody\r\n",
    "--- \ntype: trailing-space\n--- \nbody\n",
    "---\t\ntype: trailing-tab\n---\nbody\n",
]

with open("parity/corpus/mdparse-cases.jsonl", "w") as fh:
    for t in CASES:
        try:
            p = frontmatter.loads(t)
            rec = {"text": t, "error": None,
                   "meta": {k: (v if isinstance(v, (str, int, float, bool, type(None))) else str(v))
                            for k, v in p.metadata.items()},
                   "body": p.content}
        except Exception as e:
            rec = {"text": t, "error": type(e).__name__}
        fh.write(json.dumps(rec, ensure_ascii=False) + "\n")
print("ok", len(CASES))
