"""Check an exported OKF bundle with the OKF reference parser (dev only).

khub's own tests pin the bundle bytes; this asks a foreign parser whether it
accepts them. Every concept must parse, validate (a non-empty `type`) and
normalize its `verified`; every index.md must parse, and only the root one may
carry frontmatter, holding `okf_version` alone. CI's okf-reference job runs it,
informationally, through okf_reference.sh against a pinned document.py.

    uv run --with pyyaml==6.0.3 python parity/tools/okf_validate.py <bundle> <dir holding document.py>
"""

import importlib.util
import sys
from pathlib import Path


def main(bundle: Path, ref: Path) -> int:
    spec = importlib.util.spec_from_file_location("okf_document", ref / "document.py")
    doc = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = doc  # dataclasses resolve their module by name
    spec.loader.exec_module(doc)
    failures, concepts, indexes = [], 0, 0
    for path in sorted(bundle.rglob("*.md")):
        rel = path.relative_to(bundle).as_posix()
        try:
            parsed = doc.OKFDocument.parse(path.read_text(encoding="utf-8"))
            if path.name == "index.md":
                indexes += 1
                if rel == "index.md":
                    if set(parsed.frontmatter) - {"okf_version"}:
                        failures.append(f"{rel}: root frontmatter holds more than okf_version")
                elif parsed.frontmatter:
                    failures.append(f"{rel}: an index below the root carries frontmatter")
                continue
            concepts += 1
            parsed.validate()
            doc.normalize_verified(parsed.frontmatter)
        except Exception as e:  # the parser's own errors and anything it trips on
            failures.append(f"{rel}: {e}")
    for f in failures:
        print("FAIL", f)
    print(f"concepts={concepts} indexes={indexes} failures={len(failures)}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main(Path(sys.argv[1]), Path(sys.argv[2])))
