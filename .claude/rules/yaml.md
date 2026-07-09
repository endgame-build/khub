# YAML in khub — always ruamel.yaml

Use `ruamel.yaml` for all YAML in this repo. Never `import yaml` (PyYAML) in `src/` or `tests/`.

PyYAML is not a declared dependency; it only reaches the environment transitively (via `python-frontmatter`). Relying on it is a latent break. `ruamel.yaml` is the declared YAML library in `pyproject.toml`.

## Read

```python
from ruamel.yaml import YAML

_yaml = YAML(typ="safe")
with path.open() as fh:
    data = _yaml.load(fh) or {}
```

## Write

ruamel has no dump-to-string; dump into a buffer.

```python
import io
from ruamel.yaml import YAML

_yaml = YAML(typ="safe")
_yaml.default_flow_style = False
_yaml.width = 4096  # keep long scalars on one line

buf = io.StringIO()
_yaml.dump(data, buf)
text = buf.getvalue()
```

## Key ordering

`typ="safe"` sorts mapping keys on output, equivalent to the old `yaml.safe_dump(..., sort_keys=True)`. This is the deterministic, sorted form.

To preserve authored/insertion order (the old `sort_keys=False`), disable sorting:

```python
_yaml.representer.sort_base_mapping_type_on_output = False
```

`core/workspace.py` does this so `schema.yaml` keeps `base` before `entities` and the declared entity order.

## Reference implementations

- `core/resolve.py` — canonical safe loader (`load_yaml`).
- `core/workspace.py` — order-preserving dump.
