"""khub core library — resolver and compiler.

Public verbs:
- ``resolve(schema_files) -> ResolvedSchema``  (WPK-000-1, Stage C1)
- ``compile_schema(schema, out) -> CompileResult``  (WPK-000-2, Stage C2)
"""

from khub.core.compile import compile_schema
from khub.core.resolve import resolve

__all__ = ["resolve", "compile_schema"]
